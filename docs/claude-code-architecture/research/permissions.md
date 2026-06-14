# Research: permissions

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code's permission system layers three independent mechanisms: (1) six session-level permission MODES (default, acceptEdits, plan, auto, dontAsk, bypassPermissions) that set the auto-approval baseline; (2) pattern-based RULE LISTS (allow/ask/deny) in settings.json (and via --allowedTools/--disallowedTools) that are evaluated in fixed order deny->ask->allow with first-match-wins regardless of specificity; and (3) a runtime INTERACTIVE callback (`canUseTool` in SDK; `control_request`/`control_response` NDJSON over stdin/stdout in headless CLI). Rules are enforced by the harness, never the model — CLAUDE.md/prompt text only shapes what Claude attempts, not what is allowed. Deny rules at ANY settings scope cannot be overridden (managed > CLI args > local project > shared project > user). The system is heavily version-evolved (2025-2026): `auto` mode (v2.1.83+, research preview, server-side classifier, fallback at 3-consecutive/20-total blocks), `dontAsk` (locked-down CI), `acceptEdits`/`auto`/`plan` aliases, protected-path write guards (bypass no longer prompts as of v2.1.126), and `additionalDirectories` for multi-root file access. The Go replica must implement the exact 6-step SDK evaluation order, the exact rule syntax (gitignore-style path anchors for Read/Edit, glob for Bash with process-wrapper stripping and compound-command splitting, domain: prefix for WebFetch), and the exact NDJSON control protocol for tool approvals.

## Components
### Permission Modes
**Purpose:** Global session-level policy controlling how often tools pause for approval.

**Mechanism:** Shift+Tab cycles default->acceptEdits->plan. Enabled optional modes slot in after plan in order: bypassPermissions first, auto last. auto appears only via opt-in; dontAsk never appears in cycle (set via flag). bypassPermissions requires startup with --permission-mode bypassPermissions / --dangerously-skip-permissions / --allow-dangerously-skip-permissions (the --allow- variant adds to cycle without activating). On Linux/macOS bypassPermissions refuses to run as root/sudo (check auto-skipped inside recognized sandbox). Modes set the baseline; deny+explicit-ask rules apply in EVERY mode including bypassPermissions.

**Data model:** PermissionMode = "default" | "acceptEdits" | "plan" | "auto" | "dontAsk" | "bypassPermissions". (Python SDK Literal only declares 4: default/acceptEdits/plan/bypassPermissions; CLI also supports auto and dontAsk.)

**Config:** settings.json under `permissions.defaultMode`. CLI flag `--permission-mode <m>` overrides for one session. Valid values: default, acceptEdits, plan, auto, dontAsk, bypassPermissions.

### Permission Rules (allow/ask/deny)
**Purpose:** Per-tool, pattern-based pre-approval / forced-prompt / block lists in settings.json.

**Mechanism:** Evaluation order: DENY -> ASK -> ALLOW; first match wins regardless of specificity. A matching ASK prompts even when a more specific ALLOW also matches. Bare-name deny (e.g. `Bash`) removes the tool from Claude's context before evaluation; only scoped deny (e.g. `Bash(rm *)`) is matched at the per-call step. Enforced by Claude Code, NOT by the model (CLAUDE.md only shapes behavior, doesn't grant access).

**Data model:** Rule = `Tool` | `Tool(specifier)`. `Bash`/`Bash(*)` = all uses (as deny, removes tool from model context entirely). Scoped deny like `Bash(rm *)` leaves tool available, blocks matching calls.

**Config:** Keys live under top-level `permissions` object. Precedence (high->low): Managed > CLI args > local project (.claude/settings.local.json) > shared project (.claude/settings.json) > user (~/.claude/settings.json). Deny at ANY level cannot be overridden. Settings files are hot-reloaded (permissions/hooks/ConfigChange hook fire).

### Bash Pattern Matching
**Purpose:** Match shell commands against allow/deny rules with prefix/suffix/wildcard globs.

**Mechanism:** Glob `*` matches any chars including spaces (one wildcard spans multiple args). Space before `*` enforces word boundary: `Bash(ls *)` matches `ls -la` not `lsof`; `Bash(ls*)` matches both. Trailing `:*` is equivalent to trailing ` *` but ONLY at end of pattern. Claude Code is shell-operator-aware: command separators (&& || ; | |& & newline) split compound commands and EACH subcommand must match independently. Approving compound `git status && npm test` saves up to 5 separate rules (e.g. just `npm test`). Built-in read-only commands run without prompt in every mode: ls, cat, echo, pwd, head, tail, grep, find, wc, which, diff, stat, du, cd, and read-only git forms. Read-only forms allow unquoted globs; write/exec-capable flags (find -delete, sort, sed, git) still prompt.

**Data model:** Separators: && || ; | |& & <newline>. Stripped wrappers: timeout, time, nice, nohup, stdbuf, bare xargs (no flags). NOT stripped: direnv exec, devbox run, mise exec, npx, docker exec (so `Bash(devbox run *)` matches anything after run). Exec wrappers (watch, setsid, ionice, flock) and find -exec/-delete always prompt.

**Config:** Read-only set is built-in and NOT configurable (override via ask/deny rule).

### Read/Edit Path Rules
**Purpose:** File-path-scoped allow/deny using gitignore-style patterns with 4 anchor types.

**Mechanism:** Read rules apply to Read + Grep + Glob + @file mentions + IDE-open-file context. Edit rules apply to all built-in editing tools AND file commands recognized in Bash (cat, head, tail, sed) — but NOT arbitrary subprocesses. Four anchor types: `//abs/path` (filesystem root), `~/path` (home), `/path` (PROJECT ROOT, not absolute!), `path`/`./path` (cwd). A pattern like `/Users/alice/file` is relative to project root, NOT absolute. Windows paths normalized to POSIX (C:\Users\alice -> /c/Users/alice).

**Data model:** Symlink rule: Allow requires BOTH symlink path AND target to match; Deny fires if EITHER matches. `*` = within one segment, `**` = across directories. Bare filename = gitignore semantics (any depth): `Read(.env)` == `Read(**/.env)`.

**Config:** cd into working/additional dir is read-only; cd + git in one compound always prompts.

### WebFetch + Sandbox Interaction
**Purpose:** Network/domain gating, complementary to OS sandbox.

**Mechanism:** WebFetch rules use `domain:` prefix matching hostname (case-insensitive, trailing `.` stripped). `*` matches across `.` ONLY as leading `*.` or whole pattern; elsewhere within one label. Exact rule beats wildcard when both match. Sandbox (Bash-only, OS-level) merges with permissions: filesystem boundary = sandbox.filesystem + Read/Edit deny; network boundary = WebFetch rules + allowedDomains/deniedDomains.

**Data model:** Network deny: WebFetch rules + sandbox deniedDomains both apply (deny-first).

**Config:** autoAllowBashIfSandboxed: true (default) lets sandboxed Bash skip bare-Bash ask rule.

### Settings Precedence + Managed-Only
**Purpose:** Merge rules across scopes with deny-wins semantics; org-level enforcement.

**Mechanism:** High-precedence settings that cannot be overridden. Managed-only keys include allowManagedPermissionRulesOnly (only managed allow/ask/deny apply), disableBypassPermissionsMode, disableAutoMode. Precedence: Managed > CLI args > Local project > Shared project > User. If denied at any level, nothing can allow it. Embedder can tighten (not loosen) via managedSettings when parentSettingsBehavior=merge.

**Data model:** Source enum: userSettings | projectSettings | localSettings | session. Behavior enum: allow | deny | ask. Update.type: addRules | replaceRules | removeRules | setMode | addDirectories | removeDirectories.

**Config:** disableAutoMode / disableBypassPermissionsMode set to "disable" (any scope, typically managed). allowManagedPermissionRulesOnly prevents user/project allow/ask/deny rules.

### canUseTool Callback (SDK)
**Purpose:** Runtime interactive approval surfaced to embedding application.

**Mechanism:** SDK exposes `canUseTool(tool_name, input, context)` callback returning PermissionResultAllow (with updated_input + optional updated_permissions for 'always allow') or PermissionResultDeny (with message). In Python this callback requires streaming mode AND a PreToolUse hook returning {continue_:true} to keep the stream open. The callback can be pending indefinitely (defer decision to resume later). Also fires for AskUserQuestion clarifying questions. Hooks run BEFORE canUseTool and can allow/deny/modify.

**Data model:** types.py: PermissionResultAllow{behavior:"allow", updated_input, updated_permissions?}; PermissionResultDeny{behavior:"deny", message, interrupt?}. ToolPermissionContext{signal, suggestions: [PermissionUpdate]}. CanUseTool = Callable[[str, dict, ToolPermissionContext], Awaitable[PermissionResult]].

**Config:** Output format determined by --output-format (text|stream-json|json).

### NDJSON Control Protocol (CLI stdio)
**Purpose:** Wire protocol for embedding hosts to receive/approve permission prompts.

**Mechanism:** Headless CLI driven by host over stdin/stdout NDJSON. With `--permission-prompt-tool stdio`, when a tool needs approval CLI emits a `control_request` (subtype `can_use_tool`) and BLOCKS (~60s default) until host replies with matching `control_response`. Allow MUST include `updatedInput` (original or modified); deny MUST include `message`; request_id must match. Without this flag tools auto-deny in non-interactive mode. Dynamic mid-session mode switch via control_request subtype `set_permission_mode`.

**Data model:** control_request{type, request_id, request:{subtype:"can_use_tool"|"set_permission_mode", tool_name, input, decision_reason?, tool_use_id?, permission_suggestions?, mode?}}. control_response{type, response:{subtype:"success", request_id, response:{behavior:"allow"|"deny", updatedInput|message}}}.

**Config:** Flags required: --output-format stream-json --input-format stream-json --verbose --permission-prompt-tool stdio. DEBUG_CLAUDE_AGENT_SDK=1 or --debug for logs.

### Auto Mode Classifier
**Purpose:** Background model classifier that approves/blocks actions to eliminate routine prompts.

**Mechanism:** Auto mode (v2.1.83+, research preview) routes non-trivial actions to a server-side classifier model (independent of /model). Trusts working dir + configured remotes; everything else external. Reads + working-dir edits skip classifier; shell/network go through it. Blocked by default: curl|bash, sensitive data exfil, prod deploys, mass deletion, IAM grants, force push/push to main. On 3 consecutive OR 20 total blocks, auto mode pauses and resumes prompting; non-interactive `-p` mode aborts. Boundaries stated in conversation act as block signals (re-read from transcript each check, lost on compaction).

**Data model:** Non-configurable thresholds. Classifier sees user msgs + tool calls + CLAUDE.md; tool results STRIPPED (separate server-side probe flags suspicious tool-result content).

**Config:** On enter auto mode, dropped: Bash(*)/PowerShell(*), Bash(python*) wildcards, package-manager run commands, Agent allow rules. Narrow rules (Bash(npm test)) carry over. Restored on exit.

### Protected Paths
**Purpose:** Circuit breaker preventing corruption of repo state and Claude's own config.

**Mechanism:** A fixed set of dirs/files (repo state + Claude config + shell/package config) whose writes are never auto-approved except in bypassPermissions (as of v2.1.126). default/acceptEdits/plan -> prompt; auto -> classifier; dontAsk -> deny; bypassPermissions -> allow. Prompt for .claude/ write offers 'Yes, and allow Claude to edit its own settings for this session'.

**Data model:** Dirs: .git, .config/git, .vscode, .idea, .husky, .cargo, .devcontainer, .yarn, .mvn, .claude (except .claude/worktrees). Files: .gitconfig, .gitmodules, .bashrc, .zshrc, .profile, .envrc, .npmrc, .yarnrc.yml, .pnp.cjs, .bazelrc, .pre-commit-config.yaml, lefthook.yml, gradle-wrapper.properties, .devcontainer.json, .mcp.json, .claude.json, etc.

**Config:** permissions.allow rules do NOT pre-approve protected-path writes — safety check runs before allow rules. `.claude/worktrees` is exempt (Claude's own worktrees).

## Key behaviors
- Six modes total: default, acceptEdits, plan, auto, dontAsk, bypassPermissions. The Python SDK PermissionMode Literal only declares 4 (default/acceptEdits/plan/bypassPermissions) — auto and dontAsk are CLI-level and TypeScript-only for `auto`.
- auto mode requires v2.1.83+ AND plan + model (Opus 4.6+/Sonnet 4.6 on Anthropic API; Opus 4.7/4.8 only on Bedrock/Vertex/Foundry) AND on Bedrock/Vertex/Foundry the env var CLAUDE_CODE_ENABLE_AUTO_MODE=1 (v2.1.158+). Admins set permissions.disableAutoMode="disable" to lock off. auto is IGNORED in project/local settings as of v2.1.142 (must be in ~/.claude/settings.json or managed).
- bypassPermissions as of v2.1.126 NO LONGER prompts for protected-path writes (earlier versions did). It still prompts for explicit ask rules and for rm targeting / or ~. Refuses to run as root/sudo on Linux/macOS (auto-skipped in recognized sandbox). disableBypassPermissionsMode="disable" blocks it.
- dontAsk mode auto-DENIES every prompt; only permissions.allow rules and read-only Bash commands execute; explicit ask rules are DENIED (not prompted). Cloud (web) sessions ignore defaultMode dontAsk and bypassPermissions from settings files.
- acceptEdits auto-approves: Edit/Write + filesystem Bash cmds (mkdir, touch, rm, rmdir, mv, cp, sed) + their safe prefixes (LANG=C, NO_COLOR=1) + wrappers (timeout/nice/nohup). Only for paths inside cwd or additionalDirectories. PowerShell: Set-Content, Add-Content, Clear-Content, Remove-Item + aliases.
- Rule specificity does NOT change evaluation order: deny -> ask -> allow, first match wins. A matching ask prompts even if a more-specific allow also matches the same call.
- Bash pattern word-boundary subtlety: `Bash(ls *)` (space before *) matches `ls -la` NOT `lsof`; `Bash(ls*)` matches both. `:*` suffix == trailing ` *` but only at END of pattern (`Bash(git:* push)` treats colon literally).
- Bash compound commands: separators && || ; | |& & newline each split into subcommands; EVERY subcommand must independently match. Approving `git status && npm test` saves up to 5 separate rules (one per subcommand needing approval). Wrappers timeout/time/nice/nohup/stdbuf and bare xargs are stripped BEFORE matching; direnv/devbox/mise/npx/docker exec are NOT.
- Read/Edit deny applies to built-in file tools + cat/head/tail/sed in Bash, but NOT to arbitrary subprocesses (python/node scripts). For OS-level enforcement use the sandbox.
- Symlink asymmetry: allow requires BOTH symlink path AND target to match; deny fires if EITHER matches. So symlink inside allowed dir pointing to denied file is blocked.
- WebFetch domain: `*` crosses `.` only as leading `*.` or whole pattern; `domain:github.*` matches github.io but NOT github.evil.com (anti-homograph). Exact rule beats wildcard in same list.
- MCP rule glob constraint: allow rules accept tool-name globs ONLY after literal `mcp__<server>__` prefix (server segment glob-free). Unanchored allow globs like `*` or `mcp__*` are SKIPPED with a startup warning. Deny/ask globs are unrestricted (`mcp__*`, `*`).
- auto mode on-enter drops broad allow rules: Bash(*)/PowerShell(*), Bash(python*) wildcard interpreters, package-manager run commands, Agent allow rules. Narrow rules like Bash(npm test) carry over. Restored on exit.
- auto mode fallback thresholds are NON-configurable: 3 consecutive blocks OR 20 total blocks -> pause and resume prompting. Any allowed action resets consecutive counter; total counter persists for session. Non-interactive -p mode aborts on repeated blocks.
- Settings precedence (high->low): Managed > CLI args > Local project (.claude/settings.local.json) > Shared project (.claude/settings.json) > User (~/.claude/settings.json). Deny at ANY level is final. Settings files are hot-reloaded.
- additionalDirectories in settings grants FILE ACCESS only; --add-dir flag additionally loads some config (skills, partial plugin settings, CLAUDE.md only if CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1).
- Allow rules don't constrain bypassPermissions: allowed_tools only pre-approves listed tools; unlisted tools fall through to mode where bypassPermissions approves everything. Use disallowed_tools to block specific tools in bypass.
- Subagent inheritance: parent bypassPermissions/acceptEdits/auto is inherited by ALL subagents and cannot be overridden per-subagent; any permissionMode in subagent frontmatter is IGNORED in auto mode. Classifier checks subagents at 3 points (spawn task desc, each action, return history).
- Hook decisions do NOT bypass deny/ask rules: a hook returning allow still gets deny/ask rules evaluated; a hook exit code 2 (block) takes precedence over allow rules. PreToolUse runs before the prompt; PermissionRequest hook is for notifications.
- Tool names containing _ or * are exempt from the 'unknown tool' startup warning; otherwise deny/ask rules matching no known tool emit a warning.

## Open questions
- Exact default ~60s control_request blocking timeout value and whether it is configurable (docs say '~60s default', gist says not configurable).
- Whether SDKControlPermissionRequest (control can_use_tool) carries permission_suggestions populated by default in the CLI build, or only in SDK-wrapped modes.
- Exact behavior of the auto-mode classifier's server-side tool-result suspicious-content probe (separate from classifier) — implementation detail not fully documented.
- Full enumeration of which `git` subcommands are classified read-only by the built-in read-only command set (only 'read-only forms of git' is documented generically).

## Sources
- [Configure permissions - Claude Code Docs](https://code.claude.com/docs/en/permissions) — Primary source: full rule syntax (Tool/Tool(specifier)), deny->ask->allow evaluation, Bash/PowerShell/Read/Edit/WebFetch/MCP/Agent/Cd per-tool semantics, symlink handling, protected paths list, hooks interaction, settings precedence, managed-only keys.
- [Choose a permission mode - Claude Code Docs](https://code.claude.com/docs/en/permission-modes) — Primary source for all 6 modes (default/acceptEdits/plan/auto/dontAsk/bypassPermissions), auto-mode classifier details (v2.1.83+, model/provider gating, 3-consecutive/20-total fallback, subagent 3-point checks), v2.1.126/v2.1.142 version-specific behavior, protected-path per-mode matrix, disable flags.
- [Configure permissions (Agent SDK) - Claude Code Docs](https://code.claude.com/docs/en/agent-sdk/permissions) — Authoritative 6-step SDK evaluation order (Hooks->Deny->Ask->Mode->Allow->canUseTool), allowed_tools/disallowed_tools semantics, subagent mode inheritance, dontAsk/bypassPermissions edge cases, plan-mode forces edits through canUseTool.
- [Handle approvals and user input (Agent SDK) - Claude Code Docs](https://code.claude.com/docs/en/agent-sdk/user-input) — canUseTool callback signature/args, PermissionResultAllow/Deny shapes, updated_input/updated_permissions for 'approve and remember', ToolPermissionContext.suggestions, AskUserQuestion routing, dummy PreToolUse hook requirement in Python.
- [claude_code_sdk/types.py (PermissionMode/PermissionUpdate/PermissionResult dataclasses)](https://github.com/anthropics/claude-code-sdk-python/blob/cfdd28a2/src/claude_code_sdk/types.py) — Exact Python dataclass shapes for PermissionMode, PermissionUpdateDestination(userSettings/projectSettings/localSettings/session), PermissionRuleValue, PermissionUpdate(addRules/replaceRules/removeRules/setMode/addDirectories/removeDirectories), PermissionResultAllow/Deny, ToolPermissionContext.
- [ToolPermissionRequest struct - claude_codes Rust crate (docs.rs)](https://docs.rs/claude-codes/latest/claude_codes/io/struct.ToolPermissionRequest.html) — Authoritative CLI wire struct: {tool_name, input, permission_suggestions, blocked_path, decision_reason, tool_use_id} + builder methods allow/allow_with/allow_and_remember confirming updatedInput + permissions shape.
- [claude-cli-agent-protocol skill (NDJSON control_request/control_response)](https://playbooks.com/skills/bohdan-shulha/skills/claude-cli-agent-protocol) — Concrete NDJSON examples for control_request (subtype can_use_tool/set_permission_mode) and control_response (behavior allow needs updatedInput, deny needs message, request_id match, ~60s block, --permission-prompt-tool stdio requirement).
- [Claude Code settings - Claude Code Docs](https://code.claude.com/docs/en/settings) — Exact permissions.* settings keys (allow/ask/deny/additionalDirectories/defaultMode/disableBypassPermissionsMode/disableAutoMode/skipDangerousModePermissionPrompt), defaultMode valid values incl v2.1.142 auto-restriction, config scopes, hot-reload behavior, managed-only allowManagedPermissionRulesOnly.
