# Research: sandbox-security

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code's sandbox-security subsystem (v2.1.x, 2025-2026) is a defense-in-depth layering of three mechanisms: (1) an in-process permission rule engine (deny→ask→allow, with gitignore-style path and Bash-wildcard specifiers), (2) a 4-stage Bash-command static-analysis wrapper that classifies command text as read-only / dangerous / too-complex before it is matched against rules or executed, and (3) an OS-level Bash sandbox (macOS Seatbelt via sandbox-exec; Linux/WSL2 bubblewrap+bwrap+socat+seccomp) that confines filesystem writes to cwd+$TMPDIR and forces all network egress through a host-side allowlist proxy over a Unix socket. The sandbox was introduced Oct 20 2025 (Anthropic engineering blog) and open-sourced as @anthropic-ai/sandbox-runtime. Two sandbox modes exist: "auto-allow" (sandboxed Bash runs unprompted; the sandbox boundary replaces the prompt) and "regular permissions" (sandboxed commands still prompt). Even in auto-allow, explicit deny rules, content-scoped ask rules (e.g. Bash(git push *)), and rm/rmdir targeting /, $HOME, or critical paths still force prompts. Secrets/PII are handled by subprocess-env scrubbing (CLAUDE_CODE_SUBPROCESS_ENV_SCRUB), a 40+-rule gitleaks-based client-side secret scanner that redacts tool output before team-memory sync, OAuth-param redaction, and API-key truncation in the UI. The bypassPermissions mode (--dangerously-skip-permissions) is gated by a remote GrowthBook killswitch (tengu_disable_bypass_permissions_mode) and blocked when running as root/sudo.

## Components
### Permission rule engine (deny→ask→allow)
**Purpose:** Decides whether a tool call (Bash, Read, Edit, WebFetch, MCP, Agent, Cd) is allowed, denied, or must prompt — before the tool runs.

**Mechanism:** Each Bash command is parsed (Stage 1, see Bash wrapper) and split on separators && || ; | |& & and newlines into independent subcommands; each must independently match an allow rule for a compound command to be allowed. Before matching, a fixed built-in set of process wrappers is stripped: timeout, time, nice, nohup, stdbuf, and bare xargs (only when flag-less). Dev runners like npx/docker exec/devbox run/mise exec are NOT stripped. Read-only command set (ls, cat, echo, pwd, head, tail, grep, find, wc, which, diff, stat, du, cd, read-only git) is auto-allowed in every mode. Known issue (Adversa AI, v2.1.88): deny checks silently stop after 50 subcommands in one pipeline. Symlink-aware: allow requires BOTH symlink path and target to match; deny triggers if EITHER matches.

**Data model:** Rule = {tool: string, behavior: 'allow'|'deny'|'ask', specifier: string|undefined}. Settings shape: {permissions:{allow:[...],deny:[...],ask:[...],defaultMode:'default'|'acceptEdits'|'plan'|'auto'|'dontAsk'|'bypassPermissions'}}. Known source files: utils/permissions/PermissionMode.ts, PermissionRule.ts, permissionRuleParser.ts, bashPermissions.ts, permissionSetup.ts.

**Config:** settings.json `permissions.allow/ask/deny` arrays; `permissions.defaultMode`; `permissions.disableBypassPermissionsMode`; `permissions.disableAutoMode`. CLI flags `--allowedTools`, `--disallowedTools`. Managed-only: `allowManagedPermissionRulesOnly`.

### Bash sandbox — OS-level isolation
**Purpose:** Wraps each Bash subprocess (and all its children) in an OS-enforced filesystem + network boundary so commands can be auto-allowed without per-command prompts.

**Mechanism:** When enabled, every Bash invocation is wrapped by the sandbox-runtime (standalone `@anthropic-ai/sandbox-runtime`, CLI `srt`, Rust crate `sandbox-runtime-rs`) before spawn. (1) Filesystem: default write = cwd subtree + session $TMPDIR; default read = whole machine except certain denied dirs (note: ~/.aws/credentials and ~/.ssh/ are readable by default — admins must add denyRead). Writable region extended via allowWrite. git worktree shared .git is writable for refs/index but .git/hooks and .git/config remain denied. settings.json files at every scope and the managed-settings dir are always write-denied inside the sandbox so a command can't edit its own policy. (2) Network: all outbound traffic is forced through a host-side proxy (loopback). The sandbox grants socket access only to the proxy; the proxy consults allowedDomains/deniedDomains by requested hostname (no TLS termination, no inspection — documented domain-fronting limitation). On Linux the inner net namespace is unshared (bubblewrap --unshare-net) and socat relays localhost to the host proxy via a mounted Unix socket; on macOS Seatbelt blocks non-loopback traffic at the socket layer as a backstop for tools ignoring proxy env vars. First request to a new domain prompts the user (auto-allow mode) or is blocked (allowManagedDomainsOnly). (3) Escape hatch: if a sandboxed command fails due to restrictions, Claude may re-invoke the Bash tool with dangerouslyDisableSandbox=true; that retry runs UNSANDBOXED and goes through the regular permission flow. Setting allowUnsandboxedCommands:false ('Strict sandbox mode') ignores dangerouslyDisableSandbox entirely.

**Data model:** {sandbox:{enabled:bool, autoAllowBashIfSandboxed:bool, allowUnsandboxedCommands:bool, failIfUnavailable:bool, excludedCommands:[...], filesystem:{allowRead:[...], allowWrite:[...], denyRead:[...], denyWrite:[...], allowManagedReadPathsOnly:bool}, network:{allowedDomains:[...], deniedDomains:[...], httpProxyPort:int, socksProxyPort:int, allowUnixSockets:[...], allowAllUnixSockets:bool, allowLocalBinding:bool, allowMachLookup:[...]}}}. Filesystem arrays MERGE across scopes (managed+user+project+local). enableWeakerNestedSandbox and enableWeakerNetworkIsolation are top-level booleans.

**Config:** sandbox.enabled (bool); sandbox.autoAllowBashIfSandboxed (default true); sandbox.allowUnsandboxedCommands (bool/array); sandbox.failIfUnavailable (bool); sandbox.excludedCommands (array, e.g. ['docker *']); sandbox.network.httpProxyPort / socksProxyPort; sandbox.network.allowUnixSockets / allowAllUnixSockets / allowLocalBinding / allowMachLookup (macOS XPC); sandbox.network.allowManagedDomainsOnly (managed-only).

### Platform backends (Seatbelt / bubblewrap)
**Purpose:** Provide the actual OS primitives that enforce fs+net restrictions per platform.

**Mechanism:** At startup Claude Code probes for the platform backend. macOS: /usr/bin/sandbox-exec present → Seatbelt. Linux/WSL2: bubblewrap (bwrap) + socat + (optional) the seccomp filter from @anthropic-ai/sandbox-runtime which blocks Unix domain sockets. If the backend is missing or platform unsupported (native Windows, WSL1), Claude warns and runs unsandboxed unless sandbox.failIfUnavailable=true. WSL1 unsupported (bubblewrap needs WSL2 kernel features). Ubuntu 24.04+ needs an AppArmor profile granting bwrap userns.

**Data model:** macOS Seatbelt profile is SBPL text emitted with separate rules: `(allow file-write* (subpath ...))`, `(deny file-read* (subpath ...))` + re-allow `(allow file-read* (subpath ...))`. BUG (issue #39635, v2.1.85): the profile historically used `require-not` inside a deny clause, which is invalid SBPL and makes sandbox-exec abort → all bash silently fails exit 1. Valid generation requires separate deny then allow rules.

**Config:** Drives sandbox selection via runtime probe. failIfUnavailable converts the silent unsandboxed fallback into a hard startup failure (for managed deployments).

### Filesystem & network boundary config
**Purpose:** Define exactly which paths and domains the sandbox permits/blocks.

**Mechanism:** Default read = entire machine minus denied set; default write = cwd + $TMPDIR. Path-prefix resolution table: '/x' absolute (stays /x), '~/x' -> $HOME/x, './x' or bare 'x' -> relative to project root for project settings OR relative to ~/.claude for user settings (so '.' in user settings resolves to ~/.claude, not the project — a known footgun). allowRead re-allows inside a denyRead region. Filesystem arrays from multiple scopes MERGE (combined, not replaced). Permission rules (Read/Edit allow and deny) and sandbox.filesystem paths are MERGED into the final sandbox boundary. Network merges WebFetch allow rules + sandbox.allowedDomains; deniedDomains blocks even when a wildcard would otherwise allow. Managed-only lockdowns: allowManagedReadPathsOnly and allowManagedDomainsOnly ignore user/project/local entries.

**Data model:** denyWrite/allowWrite/allowRead/denyRead are string arrays. Path-prefix table: '/' absolute; '~/' home; './' or bare project-root-relative. Distinct from Read/Edit permission rule path syntax (which uses '//abs', '/proj', '~/home'). Network: allowedDomains/deniedDomains are hostname strings with '*' wildcards.

**Config:** sandbox.filesystem.allowWrite / denyWrite / allowRead / denyRead; sandbox.network.allowedDomains / deniedDomains.

### Bash wrapper multi-stage validation
**Purpose:** Parse, classify, and gate Bash command text before execution / permission matching; defends against parser-differential and shell-quoting attacks.

**Mechanism:** Stage 1 AST parse (tree-sitter-bash; fallback shell-quote+regex in external builds) with allowlist of safe node types — anything unhandled -> 'too-complex' requiring approval (fail-closed; PARSE_ABORTED distinguishes timeout/panic). Stage 2 (bashSecurity.ts): 23+ checks for command substitution $(...) and backticks, process substitution <(...) >(..), IFS injection, control chars, Unicode whitespace (U+00A0, U+2000-200B), brace expansion with quotes, heredoc extraction; plus zsh-specific bypass detection (=cmd expansion, =(cmd) process sub, zmodload/zpty/ztcp, PowerShell <# comments). Stage 3 semantic: only static >/dev/null and 2>&1 redirections are stripped; dynamic targets (vars, command subst, globs, tilde) reject and prompt. Stage 4 permission match against argv[0]+subcommands. In auto mode, dangerous-pattern rules are auto-stripped so Bash(python:*) etc. can't auto-approve code execution.

**Data model:** BASH_SECURITY_CHECK_IDS enum (23+ ids, bashSecurity.ts lines 76-101). DANGEROUS_BASH_PATTERNS list (all-users) + ANT-only extension list (dangerousPatterns.ts lines 58-79). Unknown AST nodes become `too-complex` sentinel. Failed parse -> PARSE_ABORTED sentinel.

**Config:** Gated by build-time `USER_TYPE === 'ant'` for the extended list (curl/wget/git/gh/kubectl/aws/gcloud/gsutil/sudo/zsh/fish/eval/exec/env/xargs). TRANSCRIPT_CLASSIFIER build flag gates the auto-mode ML classifier.

### Shell quoting & provider security
**Purpose:** Prevent injection when assembling the command line passed to the shell.

**Mechanism:** spawn() with a separate args array, never shell:true with raw input. The shell provider wraps the command: bash disables extglob and wraps the payload in eval for alias expansion; PowerShell uses -EncodedCommand base64 UTF-16LE (not -Command). pwd captured via `pwd -P >| quoted_path`. O_NOFOLLOW on file opens prevents symlink attacks. Heredocs are extracted before parsing and restored after to work around shell-quote limitations. Command separators recognized for splitting: && || ; | |& & and newlines. 'Yes dont ask again' on a compound command saves up to 5 separate per-subcommand rules.

**Data model:** Token normalization uses a cryptographic placeholder salt (8 random bytes hex) so injected placeholder tokens can't collide. Quoted patterns preserved; unquoted globs allowed only when every flag is read-only.

**Config:** Process wrapper stripping list is hardcoded and NOT configurable. Exec wrappers (watch, setsid, ionice, flock) and find -exec/-delete always prompt.

### Sandbox↔permission interaction & circuit breakers
**Purpose:** Define how the OS sandbox boundary composes with the in-process permission system and which prompts can never be suppressed.

**Mechanism:** Auto-allow mode (default when sandbox enabled) runs sandboxed commands without prompts; the sandbox boundary substitutes for the prompt. Even so, these always still apply: explicit deny rules; rm/rmdir targeting /, home, or critical system paths; content-scoped ask rules like Bash(git push *); a bare Bash ask rule is skipped for sandboxed commands but still applies to commands that fall back to unsandboxed. bypassPermissions mode (--dangerously-skip-permissions) skips prompts but STILL prompts for explicit ask rules and for rm -rf /, rm -rf ~, and writes to protected dirs (.git, .claude, .vscode, .idea, .husky, .cargo, .devcontainer, .yarn, .mvn, .config/git); blocked entirely when running as root/sudo on Linux/macOS unless inside a recognized sandbox.

**Data model:** PermissionMode enum: default, plan, acceptEdits, bypassPermissions, dontAsk, auto. Modes default to prompting; deny rules from ANY scope (managed/user/project/local) always win and cannot be overridden at any other scope.

**Config:** sandbox.autoAllowBashIfSandboxed (default true). bypassPermissions gated by remote killswitch gate `tengu_disable_bypass_permissions_mode` (GrowthBook/Statsig, fail-open). permissions.disableBypassPermissionsMode and permissions.disableAutoMode = 'disable' to forbid.

### Secret/PII handling in tool results & subprocess env
**Purpose:** Prevent credential leakage via subprocess env, tool output, logs, team-memory sync, and error messages.

**Mechanism:** Credentials: macOS Keychain (hex-encoded so invisible in process monitors) with plaintext fallback to ~/.claude/.credentials.json at 0o600 with explicit user warning. API keys never logged; auth status logged only as booleans; keys truncated in UI (sk-ant-...{last}). When CLAUDE_CODE_SUBPROCESS_ENV_SCRUB is set (auto in GitHub Actions with untrusted content), subprocessEnv.ts strips Anthropic/cloud/GitHub-Actions secrets from child envs before spawning Bash. Client-side secretScanner (40+ gitleaks rules) replaces detected secrets with [REDACTED] before uploading to team memory. OAuth params (state/nonce/code_challenge/code_verifier/code) redacted from logs via redactSensitiveUrlParams. Undercover mode (ant-only) strips internal codenames/versions from commits and PRs.

**Data model:** Scrubbed env var categories: Anthropic (ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN, ANTHROPIC_AUTH_TOKEN, ANTHROPIC_FOUNDRY_API_KEY, ANTHROPIC_CUSTOM_HEADERS), OTEL (*_HEADERS for LOGS/METRICS/TRACES), cloud (AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_BEARER_TOKEN_BEDROCK, GOOGLE_APPLICATION_CREDENTIALS, AZURE_CLIENT_SECRET, AZURE_CLIENT_CERTIFICATE_PATH), GitHub Actions (ACTIONS_ID_TOKEN_REQUEST_TOKEN/URL, ACTIONS_RUNTIME_TOKEN/URL, ALL_INPUTS, OVERRIDE_GITHUB_TOKEN, DEFAULT_WORKFLOW_TOKEN, SSH_SIGNING_KEY) plus INPUT_<NAME> duplicates. GITHUB_TOKEN/GH_TOKEN intentionally NOT scrubbed. secretScanner.ts: 40+ gitleaks rules -> [REDACTED].

**Config:** CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1. plainTextStorage path ~/.claude/.credentials.json (0o600). Keychain uses hex encoding. redactSensitiveUrlParams strips state/nonce/code_challenge/code_verifier/code.

### WebFetch security (preapproved domains, SSRF)
**Purpose:** Constrain Claude's own web fetches against SSRF, malicious domains, and redirect loops.

**Mechanism:** Max URL length 2000 chars, max HTTP content 10MB, fetch timeout 60s, max 10 redirects, markdown truncation 100K chars. Blocks embedded user:password URLs, single-label hostnames (<2 domain parts), HTTP->HTTPS auto-upgrade. Only same-origin redirects allowed (www. variants OK); cross-domain needs approval. Preflight domain_info query to api.anthropic.com (10s timeout, 5-min LRU TTL; URL content cached 15 min). 130+ preapproved doc/registry domains for GET-only WebFetch (curated; not inherited by sandbox; some allow uploads so unsafe for unrestricted net). file:// implicitly blocked via empty-hostname parts<2 check.

**Data model:** Preapproved list is WebFetch-GET-only and explicitly NOT inherited by the sandbox fs/net boundary. Path-prefix match uses segment boundary: pathname===p || pathname.startsWith(p+'/').

**Config:** permissions.deny WebFetch(domain:...) and sandbox.network.deniedDomains combine. WebFetch allow/deny rules and sandbox allowedDomains merge for the sandbox network boundary.

## Key behaviors
- Default read policy is the WHOLE machine (including ~/.ssh and ~/.aws/credentials) — only writes are confined to cwd+$TMPDIR. Add denyRead for credential dirs. This is a frequent footgun for re-implementors who assume read is also confined.
- Permission precedence is deny>ask>allow with NO specificity override: a matching ask rule prompts even when a more specific allow also matches. Deny from ANY settings scope (managed>CLI>local project>shared project>user) cannot be overridden by allow at any other scope.
- Bash compound commands are split on && || ; | |& & and newlines; EACH subcommand must independently pass. Approving a compound with 'Yes, dont ask again' saves up to 5 separate per-subcommand rules (not one rule for the whole string).
- Process wrappers stripped before matching: timeout, time, nice, nohup, stdbuf, and bare (flag-less) xargs only. npx/docker exec/devbox run/mise exec are NOT stripped — Bash(devbox run *) matches everything after 'run' including 'devbox run rm -rf .'. Exec wrappers watch/setsid/ionice/flock always prompt.
- Space before '*' matters: Bash(ls *) matches 'ls -la' (word boundary) but not 'lsof'; Bash(ls*) matches both. Trailing ':*' is equivalent to trailing ' *' and is only recognized at the very end of a pattern.
- A bare tool-name deny (e.g. 'Bash' or 'mcp__*') REMOVES the tool from Claude's context entirely (Claude never sees it). A scoped deny ('Bash(rm *)') leaves the tool visible and blocks matching calls at runtime.
- Sandbox fs path-prefix syntax differs from Read/Edit permission syntax: sandbox uses '/abs', '~/', './proj' (standard); Read/Edit use '//abs', '/proj', '~/home'. Do NOT reuse one parser for the other.
- Filesystem arrays MERGE across scopes (managed+user+project+local) — they are combined, not replaced. But boolean keys (enabled, failIfUnavailable) take the managed value and ignore local. excludedCommands always merges and has no managed-only lockdown, so a developer can always append escape-hatch commands.
- '.' in sandbox fs config resolves to the project root only inside project settings; in user settings (~/.claude/settings.json) it resolves to ~/.claude — placing the denyRead ~/ + allowRead . example in user settings would NOT protect the project.
- Two sandbox modes: auto-allow (sandboxed commands run unprompted) and regular permissions (sandboxed commands still prompt). Auto-allow works independently of permission mode — even outside acceptEdits, sandboxed Bash modifying files runs without prompt.
- autoAllowBashIfSandboxed (default true) means a bare Bash ask rule is SKIPPED for sandboxed commands (sandbox substitutes for the prompt), but content-scoped ask rules like Bash(git push *) STILL force a prompt, deny rules still apply, and rm/rmdir of /, home, or critical paths still prompts.
- Sandbox does NOT cover built-in file tools (Read/Edit/Write — those use the permission system), computer use (runs on real desktop), or environment inheritance (sandboxed Bash inherits parent env incl. credentials unless CLAUDE_CODE_SUBPROCESS_ENV_SCRUB is set). Subagents share the parent sandbox config.
- bypassPermissions skips prompts but still prompts for: explicit ask rules, rm -rf / and rm -rf ~ (circuit breaker), and writes to protected dirs (.git/.claude/.vscode/.idea/.husky/.cargo/.devcontainer/.yarn/.mvn/.config/git). --dangerously-skip-permissions is BLOCKED when running as root/sudo on Linux/macOS unless inside a recognized sandbox.
- seatbelt SBPL generation must NOT use require-not inside a deny clause (aborts sandbox-exec, silent exit 1 — issue #39635). Emit separate (deny file-read* (subpath ...)) then (allow file-read* (subpath ...)) rules.
- Known parser-differential risk: tree-sitter-bash is the primary parser; external builds fall back to shell-quote+regex which is less robust. Fail-closed: unknown AST node -> 'too-complex' -> approval required.
- dangerousPatterns auto-mode stripping is split: python/node/ruby/perl/php/lua/deno/tsx/npx/npm|yarn|pnpm|bun run/bash/sh/ssh are stripped for ALL users; curl/wget/git/gh/kubectl/aws/gcloud/gsutil/sudo/zsh/fish/eval/exec/env/xargs are ant-internal only (USER_TYPE==='ant'). External users get weaker protection for those.
- Adversa AI disclosed deny-rule bypass: deny checks silently stop after 50 subcommands in a single pipeline (v2.1.88). A reimplementation must cap/iterate all subcommands, not just the first 50.
- bypassPermissions killswitch via GrowthBook gate `tengu_disable_bypass_permissions_mode` is one-way (Anthropic can revoke, not grant) and FAIL-OPEN (defaults to not-disable if GrowthBook unreachable). Checked once before first query per session; reset on /login.
- Domain safety preflight is cached 5 min (LRU), so a newly-compromised/-blocklisted domain stays reachable up to 5 min. URL content cached 15 min.
- Preapproved WebFetch domains (130+) are GET-only and explicitly NOT shared with the sandbox network boundary — some (huggingface.co, kaggle.com, nuget.org) allow uploads and would be unsafe as general sandbox egress.
- macOS Seatbelt + Go caveat: a faithful Go replica cannot use sandbox-exec's require-not-in-deny and must generate valid SBPL; also note enableWeakerNetworkIsolation (allow system TLS trust service) and enableWeakerNestedSandbox (bind-mount container /proc) deliberately weaken isolation and should only be opt-in.

## External interfaces
- settings.json keys: sandbox.{enabled,autoAllowBashIfSandboxed,allowUnsandboxedCommands,failIfUnavailable,excludedCommands}, sandbox.filesystem.{allowRead,allowWrite,denyRead,denyWrite,allowManagedReadPathsOnly}, sandbox.network.{allowedDomains,deniedDomains,httpProxyPort,socksProxyPort,allowUnixSockets,allowAllUnixSockets,allowLocalBinding,allowMachLookup,allowManagedDomainsOnly}, enableWeakerNestedSandbox, enableWeakerNetworkIsolation
- settings.json keys: permissions.{allow,deny,ask,defaultMode,disableBypassPermissionsMode,disableAutoMode,additionalDirectories}, and bare allow/deny/ask/defaultMode shorthands
- Permission rule syntax: Tool / Tool(specifier); Bash(npm run *) / Bash(ls:*) (= Bash(ls *)); WebFetch(domain:example.com); Read(//abs|~/home|/proj|./cwd); mcp__server__tool and mcp__server__*; Agent(Name); Cd(path)
- Env vars: CLAUDE_CODE_SUBPROCESS_ENV_SCRUB (strip secrets from child envs), CLAUDE_CODE_UNDERCOVER=1 (force undercover), USER_TYPE=ant (build-time internal gating)
- CLI flags: --dangerously-skip-permissions (bypass mode), --allowedTools / --disallowedTools, --add-dir <path>
- Bash tool parameter: dangerouslyDisableSandbox (bool) — retry outside sandbox; ignored under allowUnsandboxedCommands:false
- /sandbox slash command (panel: Mode/Overrides/Config/Dependencies); /permissions; /add-dir; /cd (v2.1.169+)
- Remote gates (GrowthBook/Statsig): tengu_disable_bypass_permissions_mode (bypass killswitch), TRANSCRIPT_CLASSIFIER (auto-mode gate)
- External tool: `srt` / `@anthropic-ai/sandbox-runtime` (npm) / sandbox-runtime-rs (Rust crate) — sandbox-exec (macOS) + bubblewrap + socat + seccomp filter (Linux/WSL2)
- WebFetch domain preflight: POST api.anthropic.com/api/web/domain_info (10s timeout, 5-min cache TTL)

## Open questions
- Exact shape of the dynamically generated SBPL profile emitted for arbitrary allowWrite/denyRead combinations post-fix for issue #39635 (need to read sandbox-runtime source for the canonical generator).
- Whether the `allowUnsandboxedCommands` setting is a boolean (Strict mode toggle) or an array of commands permitted unsandboxed — the gist lists it as an array while docs describe it as bool false=Strict; likely both forms exist (bool false disables the escape hatch, array lists allowed unsandboxed commands).
- The full current DANGEROUS_BASH_PATTERNS + ant-only list as of the latest 2026 build (the v2.1.88 reconstruction may be slightly stale).
- Whether the 50-subcommand deny bypass is fixed in current 2026 builds and what the new cap is.

## Sources
- [Configure the sandboxed Bash tool — Claude Code Docs](https://code.claude.com/docs/en/sandboxing) — Official, authoritative reference for sandbox modes, fs/network config, allowedDomains/deniedDomains, excludedCommands, dangerouslyDisableSandbox escape hatch, Seatbelt/bubblewrap platform mapping, WSL2 details, security limitations.
- [Configure permissions — Claude Code Docs](https://code.claude.com/docs/en/permissions) — Authoritative permission rule syntax: deny→ask→allow order, Bash wildcard/compound/wrapper rules, read-only command set, Read/Edit path anchors, WebFetch domain rules, MCP/Agent/Cd rules, managed-only keys, settings precedence.
- [Beyond permission prompts: making Claude Code more secure and autonomous with sandboxing — Anthropic Engineering](https://www.anthropic.com/engineering/claude-code-sandboxing) — Anthropic engineering post confirming fs+network isolation built on macOS Seatbelt and Linux bubblewrap, the Unix-socket→host-proxy network architecture, 84% prompt reduction, and the open-sourced sandbox-runtime.
- [Security — Claude Code Docs](https://code.claude.com/docs/en/security) — Official statement of read-only-by-default, built-in read-only Bash command set, write confined to launch dir, command-injection detection, fail-closed matching, network command approval, WebDAV/UNC warnings, macOS Keychain credential storage.
- [Security Analysis of Claude Code v2.1.88 — Source Reconstructed from Source Maps](https://b.zzn.im/blog/claude-code-v2.1.88-security-analysis/) — Source-map reconstruction giving internal file paths and mechanisms: 4-stage Bash validation, bashSecurity 23+ checks, dangerousPatterns ant-only split, subprocessEnv scrub var list, secretScanner, bypassPermissions killswitch gate name tengu_disable_bypass_permissions_mode, WebFetch limits, preapproved domains.
- [Seatbelt sandbox silently blocks all bash commands when denyRead is configured — anthropics/claude-code#39635](https://github.com/anthropics/claude-code/issues/39635) — Primary evidence for the exact SBPL generation bug (require-not in deny aborts sandbox-exec) and that valid generation uses separate (deny file-read* (subpath ...)) + (allow ...) rules.
- [anthropic-experimental/sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime) — The open-sourced runtime Claude Code wraps: confirms sandbox-exec (macOS Seatbelt) + bubblewrap (Linux) + proxy-based network filtering; CLI srt / npm @anthropic-ai/sandbox-runtime.
- [Claude Code — Complete settings.json Reference (v2.1.104) — gist](https://gist.github.com/mculp/c082bd1e5a439410158974de90c89db7) — Compiled settings key catalog (~125 keys) including the full sandbox.* and permissions.* schema, enableWeakerNestedSandbox/enableWeakerNetworkIsolation, network sub-keys (allowUnixSockets, allowMachLookup, allowLocalBinding).
- [Critical Claude Code vulnerability: Deny rules silently bypassed after 50 subcommands — Adversa AI](https://adversa.ai/blog/claude-code-security-bypass-deny-rules-disabled/) — Documents the 50-subcommand deny-rule bypass disclosed by Adversa AI Red Team (v2.1.88) — load-bearing for the reimplementation to cap iteration correctly.
- [How /sandbox Works — Claude Code Camp](https://www.claudecodecamp.com/p/claude-code-sandboxing-how-sandbox-works-and-what-it-doesn-t-protect) — Confirms Seatbelt backstop blocking non-loopback traffic at the socket layer for tools that ignore proxy env vars, and the .git/hooks deny that breaks git init under sandbox.
- [Claude Code's Deny Rules Don't Protect You — adamkinney (AI All The Things)](https://adamkinney.com/aatt/claude-code/deny-rules-dont-protect-you-sandbox-does/) — Clarifies that permission deny rules are in-process (not OS-level), why Read deny doesn't stop `python -c 'open(...)'`, and that sandbox.filesystem.denyRead is the OS-enforced layer.
