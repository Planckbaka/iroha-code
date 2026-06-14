# Research: system-prompt-assembly

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code's system prompt is not a static string but a per-turn assembled array of blocks (branded `SystemPrompt` type) built by `getSystemPrompt()` in `src/constants/prompts.ts` and resolved by `buildEffectiveSystemPrompt()`. It is split into a STATIC, globally-cacheable zone (~12 sections: identity, intro, system rules, doing-tasks, actions, using-tools, tone/style, output-efficiency, token-budget, proactive) and a DYNAMIC, per-session zone (env info, scratchpad, function-result-clearing, MCP instructions, memory, CLAUDE.md, output-style, git-status, append-prompt) divided by a `__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__` marker that is stripped before the API call. Each section is either memoized via `systemPromptSection()` (cached until `/clear` or `/compact`) or recomputed every turn via `DANGEROUS_uncachedSystemPromptSection()` (used for MCP instructions and env info). CLAUDE.md content is injected as a USER message (project context), NOT into the system prompt in the SDK; in the interactive CLI it appears in the prompt assembly. Hooks inject `<system-reminder>` tags via `additionalContext`/`systemMessage` at event-appropriate positions. The Agent SDK exposes preset/custom/append options and `excludeDynamicSections` (v0.2.98+) to move per-session context into the first user message for cross-session cache reuse.

## Components
### Effective Prompt Resolution (priority system)
**Purpose:** Decides the final prompt base before per-turn assembly.

**Mechanism:** buildEffectiveSystemPrompt() resolves which prompt base is used via a strict priority ladder: (0) overrideSystemPrompt non-empty replaces everything; (1) COORDINATOR_MODE feature => dedicated coordinator prompt (strips toolset to Agent + TaskStop + SendMessage); (2) mainThreadAgentDefinition exists => proactive mode appends to default, else replaces; (3) --system-prompt CLI arg replaces default; (4) default = full getSystemPrompt() output. The SDK exposes three starting points: minimal default (omitted systemPrompt), claude_code preset (object {type:'preset',preset:'claude_code', append?:string, excludeDynamicSections?:boolean}), or a custom string.

**Data model:** Priority tiers: 0 Override, 1 Coordinator (feature active => toolset stripped to Agent+TaskStop+SendMessage), 2 mainThreadAgentDefinition (proactive: append; else replace), 3 --system-prompt CLI (replace), 4 Default = getSystemPrompt(). The branded SystemPrompt type prevents passing raw string[] to the API.

**Config:** systemPrompt: { type:'preset', preset:'claude_code', append?:string, excludeDynamicSections?:boolean } (TS); system_prompt={'type':'preset','preset':'claude_code','append':...} (Python). Custom: systemPrompt: string. None => minimal default. excludeDynamicSections added v0.2.98 (TS) / v0.1.58 (Python). CLI flags: --append-system-prompt, --exclude-dynamic-system-prompt-sections, --system-prompt. Env: CLAUDE_CODE_SIMPLE truthy => single-line minimal prompt.

### getSystemPrompt() — section factory
**Purpose:** The core factory that concatenates ~18 ordered sections split by a cache boundary.

**Mechanism:** Static zone (cacheable, scope 'global'): 1 CLI System Prefix ('You are Claude Code, Anthropic's official CLI for Claude.'), 2 Intro (interactive vs headless swaps 'assist' for 'complete'), 3 Cyber Risk Instruction, 4 URL Safety ('NEVER generate or guess URLs'), 5 System Rules (output format, prompt-injection defense, system-reminder handling, compaction), 6 Doing Tasks (anti-YAGNI; conditional on output_style keepCodingInstructions), 7 Executing Actions (LOW/MEDIUM/HIGH blast-radius taxonomy; always-confirm set: rm -rf/DROP TABLE, git push/publish, migrations/force-push), 8 Using Your Tools (prefer dedicated tools Read/Edit/Glob/Grep over Bash; varies by repl_mode/embedded_search/task_tool_enabled), 9 Tone & Style (no emojis; varies user_type_external), 10 Output Efficiency (internal 'between-tool calls ≤25 words' vs external 'go straight to the point'), 11 Token Budget (GATED on feature('TOKEN_BUDGET')), 12 Proactive/KAIROS (GATED on feature('PROACTIVE')). Then the cache boundary marker, then the Dynamic zone (scope 'org' or uncached): 13 Env Info (cwd, isGit, platform, shell, osVersion, model name, knowledge cutoff; varies undercover/worktree), 14 Scratchpad, 15 Function Result Clearing (microcompact_enabled; '5 most recent results always kept'), 16 Summarize Tool Results, 17 MCP Server Instructions (DANGEROUS_uncached — recomputed every turn), 18 Memory, plus Language, Output Style, Git Status Snapshot (current branch / recent commits / working tree — snapshot in time), Numeric Length Anchors (user_type_ant), Brief (kairos_brief), and Append System Prompt at the very end.

**Data model:** Sections registered via systemPromptSection(name, compute) [cached, invalidated only on /clear or /compact] or DANGEROUS_uncachedSystemPromptSection(name, compute, reason) [recomputed every turn — used for getMcpInstructionsSection, Env Info]. clearSystemPromptSections() invalidates the memo AND clears beta-header latches.

**Config:** Gates: ask_user_enabled, non_interactive (omits shell-shortcut section in SDK/headless), agent_tool_enabled (+ fork_subagent + explore_plan_agents), skills_enabled (+ experimental_skill_search), verification_agent, memory_configured, user_type_ant, language_set, output_style, mcp_connected (+ mcp_delta_mode), scratchpad_enabled, microcompact_enabled, token_budget, kairos_brief, is_git_repo & !remote & git_instructions_enabled, append_system_prompt.

### Environment / System Context section
**Purpose:** Inject cwd, platform, shell, model, OS version, git status so the model knows its execution environment.

**Mechanism:** Env Info is a DANGEROUS_uncachedSystemPromptSection recomputed per turn. It reads osType/osVersion/osRelease, getCwd(), getIsGit(). A separate 'Git Status Snapshot' block (gated is_git_repo && not remote && git_instructions_enabled) injects current branch, default (main) branch, git user, and a working-tree status with recent commits. The whole env block is what breaks the prefix cache for the static zone — excludeDynamicSections moves it into the first user message instead.

**Data model:** Env fields read: osType, osVersion, osRelease, getCwd(), getIsGit(). The gitStatus block carries currentBranch, mainBranch (default branch for PRs), gitUser, and a working-tree status string + recent commits list.

**Config:** Env var sources: osType, osVersion, osRelease (platform runtime), getCwd(), getIsGit(). CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1 loads CLAUDE.md/rules from --add-dir paths.

### CLAUDE.md cascade (memory)
**Purpose:** Persistent project/user/org instructions, loaded per session and lazily.

**Mechanism:** IMPORTANT asymmetry: in the Agent SDK CLAUDE.md is NOT injected into the system prompt — the SDK reads it and injects it as a USER message (project context) alongside the conversation. Per the memory docs: 'CLAUDE.md content is delivered as a user message after the system prompt, not as part of the system prompt itself.' Resolution walks up the directory tree from cwd collecting CLAUDE.md and CLAUDE.local.md, concatenating root-down with .local appended after .md at each level. Managed policy CLAUDE.md (/Library/Application Support/ClaudeCode/CLAUDE.md on macOS, /etc/claude-code/ on Linux, C:\Program Files\ClaudeCode\ on Windows) loads first and cannot be excluded. @path imports resolve relative to the importing file with max depth 4 hops. Subdirectory CLAUDE.md files load lazily when Claude reads files there. Project-root CLAUDE.md is re-injected after /compact.

**Data model:** Discovery order: managed policy (cannot be excluded) -> ~/.claude/CLAUDE.md -> ancestor dirs root-down (CLAUDE.md then CLAUDE.local.md at each level) -> ./CLAUDE.md or ./.claude/CLAUDE.md -> ./CLAUDE.local.md. .claude/rules/*.md (no paths frontmatter) join at CLAUDE.md priority; path-scoped rules (paths: glob YAML) load on file read. HTML block comments <!-- ... --> stripped (code-block comments preserved). Imports expanded recursively up to 4 hops. Auto-memory MEMORY.md first 200 lines or 25KB loaded; topic files on demand only.

**Config:** settingSources / setting_sources controls whether 'project' and 'user' files load (default both enabled). CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1 loads memory from --add-dir paths. claudeMdExcludes (glob, arrays merge across layers) skips files. --setting-sources may exclude 'local'.

### MCP Server Instructions injection
**Purpose:** Inject per-server 'how to use this server' guidance into the dynamic prompt zone.

**Mechanism:** When MCP servers are connected, each server's instructions field (returned in InitializeResult during the initialize handshake) is injected as a '# MCP Server Instructions' section, one subsection per server, in the dynamic/uncached zone (DANGEROUS_uncachedSystemPromptSection => recomputed every turn). If mcp_delta_mode is enabled, instructions are delivered as a per-turn attachment instead of inline in the system prompt. Empty/missing instructions are omitted.

**Data model:** instructions: string from InitializeResult. Per-server section header '## <serverName>'. Composite prompt text assembled under '# MCP Server Instructions'.

**Config:** mcp_connected gate; mcp_delta_mode toggles per-turn attachment vs inline. Instructions are re-fetched because tools/list can change (MCP list_changed).

### Hook injection (system-reminder wrapping)
**Purpose:** Run user-defined shell/HTTP/MCP/prompt/agent interceptors at lifecycle events and inject their output as model-visible reminders.

**Mechanism:** Five handler types: command (stdin JSON / stdout+exit), http (POST body / 2xx response JSON), mcp_tool (calls a tool on a connected server; text output treated as command stdout), prompt (single-turn Claude yes/no), agent (spawns a tool-using subagent). The additionalContext field in hookSpecificOutput is wrapped by Claude Code in a <system-reminder> tag and inserted at a position determined by the firing event: SessionStart/Setup/SubagentStart => start of conversation before first prompt; UserPromptSubmit/UserPromptExpansion => alongside submitted prompt; PreToolUse/PostToolUse/PostToolUseFailure/PostToolBatch => next to the tool result; Stop/SubagentStop => end of turn. Matches: 'Claude Code wraps the string in a system reminder and inserts it into the conversation at the point where the hook fired.' Exit 0 with stdout on UserPromptSubmit/UserPromptExpansion/SessionStart also adds the text as Claude-visible context (these three events only). Exit 2 blocks per the per-event blocking table.

**Data model:** Output schema: { continue?:bool, stopReason?:string, suppressOutput?:bool, systemMessage?:string, terminalSequence?:string(allowlist OSC 0/1/2/9/99/777 + BEL), decision?:'block', reason?:string, hookSpecificOutput:{ hookEventName, permissionDecision?:'allow'|'deny'|'ask', permissionDecisionReason?, additionalContext?, retry?:bool } }. additionalContext/systemMessage/plain stdout capped 10,000 chars; overflow => file + preview. Exit codes: 0 success (JSON parsed), 2 blocking error (stderr fed to Claude), other = non-blocking. HTTP: 2xx+body=JSON, non-2xx=non-blocking.

**Config:** Boundaries: UserPromptSubmit default timeout lowered to 30s; MessageDisplay 10s. Tokens/effort injected as $CLAUDE_EFFORT env and effort:{level} in hook JSON. Managed hooks survive disableAllHooks from lower layers.

### Hook event matchers & tool-name namespacing
**Purpose:** Filter which hooks fire for which tool/event.

**Mechanism:** Tool-event hooks (PreToolUse, PostToolUse, PostToolUseFailure, PermissionRequest, PermissionDenied) match by tool_name. matcher rules: '*' / '' / omitted => all; only [A-Za-z0-9_|] => exact or |-separated exact list; any other char => JS regex. MCP tools are named mcp__<server>__<tool>; match-all-from-server needs mcp__<server>__.* (the .* makes it a regex; bare mcp__memory is treated as exact string and matches nothing). Optional per-handler 'if' uses permission-rule syntax (e.g. Bash(rm *), Edit(*.ts)) and only evaluates on tool events. SessionStart matches startup|resume|clear|compact; InstructionsLoaded matches session_start|nested_traversal|path_glob_match|include|compact.

**Data model:** Input: { session_id, transcript_path, cwd, permission_mode:'default'|'plan'|'acceptEdits'|'auto'|'dontAsk'|'bypassPermissions', effort:{level}, hook_event_name, plus event-specific (tool_name, tool_input). agent_id/agent_type added in subagents. Output: permissionDecision allow/deny/ask + reason (PreToolUse), retry:bool (PermissionDenied), additionalContext (model-facing), systemMessage (user-facing warning), suppressOutput, terminalSequence, continue:false + stopReason.

**Config:** Matched by tool name. Settings keys: hooks.<Event>[].matcher, hooks[].if (permission-rule syntax), disableAllHooks, allowManagedHooksOnly, once (skill-frontmatter only). Hook sources: ~/.claude/settings.json, .claude/settings.json, .claude/settings.local.json, managed policy, plugin hooks/hooks.json, skill/agent frontmatter.

### Dynamic reminders: todo / plan mode / skill surfacing
**Purpose:** Steer the model mid-conversation without rebuilding the system prompt.

**Mechanism:** These are NOT part of the system prompt. They are injected as attachments appended to user messages each turn: (a) todo/task state ('The task tools haven't been used recently... consider using TaskCreate'), (b) active plan-mode ('plan only, do not code yet'), (c) auto-surfaced relevant skills ('Skills relevant to your task:'), (d) hook-produced additionalContext, (e) git/file-change diff reminders after tool edits. They are wrapped in <system-reminder> tags and the model is instructed (via System Rules section) to read and apply them.

**Data model:** Reminders are <system-reminder> blocks attached as attachments to user messages (not stored in the system prompt array).

**Config:** Todo tracking built into Agent SDK (TaskCreate/TaskUpdate/TaskList). Plan mode is permission_mode:'plan'. Reminders are non-system-prompt context — they appear as <system-reminder> tags in the message stream.

## Key behaviors
- CLAUDE.md lives in the CONVERSATION (user message), not the system prompt, in the Agent SDK — it does not affect the system-prompt cache entry. The env-info block (cwd/platform/git/shell/model) DOES live in the system prompt and is what normally prevents cache reuse across directories.
- excludeDynamicSections moves the env-info block into the FIRST USER MESSAGE so the system prompt (preset + append) becomes byte-identical across users/machines and shares a cache entry. Tradeoff: text in a user message carries marginally less weight than in the system prompt. Requires claude-agent-sdk TS v0.2.98 / Python v0.1.58.
- Three caching modes in splitSysPromptPrefix(): Mode 1 (MCP present) => no global cache, whole prompt scope 'org' because MCP tool defs change; Mode 2 (1P default, no MCP) => split at boundary, static=scope 'global' (cross-org cacheable), dynamic=uncached; Mode 3 (3P providers Bedrock/Vertex/OpenAI) => whole prefix scope 'org'.
- The boundary marker __SYSTEM_PROMPT_DYNAMIC_BOUNDARY__ is inserted into the prompt array but REMOVED before sending to the API — the model never sees it. It exists only so splitSysPromptPrefix can find the split point.
- systemPromptSection() memoizes compute results and is only cleared by /clear or /compact (clearSystemPromptSections also clears beta-header latches). DANGEROUS_uncachedSystemPromptSection forces per-turn recompute and is deliberately named to discourage use — reserved for genuinely per-turn content (MCP instructions, env info).
- Output styles: a custom output style by DEFAULT REPLACES the preset's software-engineering instructions; set keep-coding-instructions: true in frontmatter to layer on top instead. Stored in ~/.claude/output-styles/ (user) or .claude/output-styles/ (project). Loaded via settingSources user/project. Python SDK has no programmatic outputStyle selector.
- CLAUDE.md loading is gated by settingSources — an empty array disables CLAUDE.md entirely even though the claude_code preset is active. 'project' loads ./CLAUDE.md or ./.claude/CLAUDE.md; 'user' loads ~/.claude/CLAUDE.md.
- CLAUDE.md import depth is capped at 4 hops; relative @paths resolve against the importing file, not cwd. Block HTML comments <!-- --> are stripped before injection (code-block comments preserved). Subdirectory CLAUDE.md files load lazily on file reads, not at launch.
- Auto-memory MEMORY.md: only first 200 lines OR 25KB (whichever first) loaded at session start; topic files loaded on demand. Storage at ~/.claude/projects/<project>/memory/, shared across worktrees of one git repo. Requires Claude Code v2.1.59+. Toggle: autoMemoryEnabled setting, CLAUDE_CODE_DISABLE_AUTO_MEMORY=1, or /memory UI.
- managed-policy CLAUDE.md cannot be excluded by claudeMdExcludes and cannot be disabled — it always applies. The claudeMd key in managed-settings.json is an alternative to deploying a managed CLAUDE.md file (only honored in managed/policy settings).
- Git Status Snapshot injected only when is_git_repo && not remote && git_instructions_enabled. It is explicitly a 'snapshot in time' and the prompt warns it will not update during the conversation.
- MCP server instructions come from the instructions field of the MCP InitializeResult; Claude Code injects them as a per-server subsection. If mcp_delta_mode is on, they are attached per-turn instead. Because MCP tool lists can change (list_changed), the MCP instructions section is DANGEROUS_uncached.
- Hook additionalContext/systemMessage/plain stdout are CAPPED at 10,000 chars; overflow is written to a file and replaced with a preview + path. additionalContext is wrapped in a <system-reminder> tag and inserted at the event-appropriate position (start of convo / alongside prompt / next to tool result / end of turn) — it is model-visible but not shown as a chat message.
- Exit code 2 is the ONLY blocking signal for most hook events (exit 1 = non-blocking error, action proceeds). UserPromptSubmit exit 2 erases the prompt; PreToolUse exit 2 blocks the tool; Stop exit 2 keeps Claude going. JSON output is only parsed on exit 0.
- As of v2.1.139 command hooks run without a controlling terminal on macOS/Linux (/dev/tty unavailable); use terminalSequence JSON field (allowlisted OSC 0/1/2/9/99/777 + BEL, v2.1.141+) for notifications instead.
- For OpenAI-compatible providers, normalizeMessagesForAPI() flattens the SystemPrompt[] by joining with \n\n into a single 'system' role message and strips cache_control / Anthropic beta headers.
- Plan mode injects an attachment to user messages ('plan only, do not code yet') and is reflected as permission_mode:'plan' in hook input. Plan mode actually writes plan markdown files then wipes the planning context before execution.

## External interfaces
- SDK (TS): systemPrompt: {type:'preset',preset:'claude_code',append?,excludeDynamicSections?}
- SDK (Python): system_prompt={'type':'preset','preset':'claude_code','append':...,'exclude_dynamic_sections':bool}
- SDK: settingSources=['user','project'] / setting_sources=['user','project'] (empty array disables CLAUDE.md)
- SDK: settings.outputStyle (string) selects ~/.claude/output-styles/<name>.md
- CLI flags: --append-system-prompt, --system-prompt, --exclude-dynamic-system-prompt-sections, --add-dir, --setting-sources
- Env: CLAUDE_CODE_SIMPLE, CLAUDE_CODE_USE_BEDROCK/VERTEX/OPENAI, CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD, CLAUDE_CODE_DISABLE_AUTO_MEMORY
- Managed CLAUDE.md paths: /Library/Application Support/ClaudeCode/CLAUDE.md (macOS), /etc/claude-code/CLAUDE.md (Linux/WSL), C:\Program Files\ClaudeCode\CLAUDE.md (Windows)
- settings.json keys: claudeMd, claudeMdExcludes (glob array), autoMemoryEnabled, autoMemoryDirectory, outputStyle, hooks.{Event}[]
- Output styles: ~/.claude/output-styles/*.md and .claude/output-styles/*.md with frontmatter name/description/keep-coding-instructions
- Hook config JSON: hooks.<Event>[].matcher + [].hooks[].{type,command/args|url|server+tool|prompt,if,timeout,async,asyncRewake,statusMessage,once}
- Internal TS functions: getSystemPrompt(), buildEffectiveSystemPrompt(), systemPromptSection(), DANGEROUS_uncachedSystemPromptSection(), clearSystemPromptSections(), splitSysPromptPrefix(), normalizeMessagesForAPI()
- Type: branded SystemPrompt = string[] & {__brand:'SystemPrompt'}
- Cache-control scopes: 'global' (cross-org) and 'org' (per-org)

## Open questions
- Exact byte content / wording of the 12 static sections in the CURRENT (2026) public build — Piebald-AI repo tracks this per version; should be sampled directly from the target version for a 1:1 replica.
- Full current set of feature-flag gates (TOKEN_BUDGET, CACHED_MICROCOMPACT, PROACTIVE/KAIROS, COORDINATOR_MODE, experimental_skill_search, verification_agent, fork_subagent, explore_plan_agents, undercover) and their default on/off state per build.
- Precise wording of the env-info template line (Working directory / Is a git repository / Platform / Shell / OS Version / model name / knowledge cutoff) and whether 'date' is still injected in 2026 builds.
- Whether managed-policy and ~/.claude/CLAUDE.md are injected into the SYSTEM PROMPT (as the CLI does) or only the user message (as the SDK does) — the two surfaces diverge; the Go replica must pick per surface.
- Exact implementation of mcp_delta_mode (per-turn attachment format) and scratchpad path scheme.

## Sources
- [Modifying system prompts — Claude Code Docs (official)](https://code.claude.com/docs/en/agent-sdk/modifying-system-prompts) — Authoritative: preset/append/custom/excludeDynamicSections, CLAUDE.md goes to conversation not system prompt, excludeDynamicSections min versions (TS v0.2.98 / Python v0.1.58), what env fields embed in the prompt and break cache.
- [How Claude remembers your project — Claude Code Docs (official)](https://code.claude.com/docs/en/memory) — Authoritative CLAUDE.md cascade: 4 scopes + load order, ancestor walk, CLAUDE.local.md appended per level, @import max depth 4, HTML comment stripping, /compact re-injection of project root, claudeMdExcludes, managed CLAUDE.md paths, auto-memory first-200-lines/25KB cap.
- [Hooks reference — Claude Code Docs (official)](https://code.claude.com/docs/en/hooks) — Authoritative hook lifecycle, all 30 events, matcher semantics (exact vs regex), mcp__<server>__<tool> namespacing, 5 handler types, JSON output schema (additionalContext/systemMessage/permissionDecision/decision block/terminalSequence), exit-2 blocking, 10k char cap, <system-reminder> wrapping and insertion-point rules.
- [System Prompt Assembly — DeepWiki (claude-code-best, indexed 2026-06-12)](https://deepwiki.com/claude-code-best/claude-code/2.3-system-prompt-assembly) — Reverse-engineered from leaked source: getSystemPrompt() in src/constants/prompts.ts, branded SystemPrompt type, SYSTEM_PROMPT_DYNAMIC_BOUNDARY marker removed pre-send, systemPromptSection vs DANGEROUS_uncachedSystemPromptSection, buildEffectiveSystemPrompt priority ladder, splitSysPromptPrefix 3 cache modes, CLAUDE_CODE_SIMPLE fast path.
- [How Claude Code Builds Its System Prompt — 18 Layers (Cadences)](https://codex.cadences.app/en/blog/claude-code-system-prompt/) — Independent corroboration of the 18 ordered sections, static/dynamic boundary placement at section 12-13, anti-YAGNI section content, risk taxonomy LOW/MED/HIGH, conditional feature-flag gates (TOKEN_BUDGET, PROACTIVE/KAIROS, CACHED_MICROCOMPACT, COORDINATOR_MODE).
- [How Claude Code Builds a System Prompt — dbreunig (2026-04-04)](https://www.dbreunig.com/2026/04/04/how-claude-code-builds-a-system-prompt.html) — Most granular per-section inventory with conditional gates and variation triggers (output_style, user_type_ant, repl_mode, embedded_search, task_tool_enabled, agent_tool_enabled+fork_subagent, skills_enabled, experimental_skill_search, verification_agent, memory_configured, undercover, is_worktree, language_set, microcompact_enabled, token_budget, kairos_brief, is_git_repo&&!remote&&git_instructions_enabled, append_system_prompt), plus env-info template text and git snapshot block.
- [Server Instructions: Giving LLMs a user manual — MCP Blog](https://blog.modelcontextprotocol.io/posts/2025-11-03-using-server-instructions/) — Confirms MCP servers return instructions in InitializeResult and hosts (including Claude Code) inject them into the system prompt; basis for the DANGEROUS_uncached MCP instructions section.
- [Piebald-AI/claude-code-system-prompts (GitHub)](https://github.com/Piebald-AI/claude-code-system-prompts) — Version-tracked dump of the actual assembled system prompt text, 27 builtin tool descriptions, and sub-agent prompts (Explore/Plan/Task) — ground truth for exact wording per version.
- [Server instructions issue — anthropics/claude-code #43749](https://github.com/anthropics/claude-code/issues/43749) — Documents the instructions field consumption from InitializeResult into session context.
- [Inside Claude Code's System Prompt — claudecodecamp](https://www.claudecodecamp.com/p/inside-claude-code-s-system-prompt) — Community corroboration of 110+ conditionally assembled instructions and section ordering.
