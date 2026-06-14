# Research: hooks

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code's hooks system lets users attach deterministic handlers (shell commands, HTTP endpoints, MCP tool calls, or LLM prompt/agent evaluations) to ~30 named lifecycle events (PreToolUse, PostToolUse, PostToolUseFailure, PostToolBatch, PermissionRequest, PermissionDenied, UserPromptSubmit, UserPromptExpansion, Notification, Stop, StopFailure, SubagentStart, SubagentStop, TeammateIdle, TaskCreated, TaskCompleted, SessionStart, Setup, SessionEnd, PreCompact, PostCompact, ConfigChange, CwdChanged, FileChanged, WorktreeCreate, WorktreeRemove, InstructionsLoaded, MessageDisplay, Elicitation, ElicitationResult). Hooks are configured in settings.json under a top-level `hooks` key (3-level nesting: event -> matcher group -> handler array). Command hooks receive event JSON on stdin and signal via exit code (0=success/JSON, 2=blocking error, other=non-blocking error) plus optional stdout JSON. The JSON output supports universal fields (continue, stopReason, suppressOutput, systemMessage, terminalSequence) plus event-specific decision fields: PreToolUse uses hookSpecificOutput.permissionDecision (allow/deny/ask/defer); PermissionRequest uses hookSpecificOutput.decision.behavior (allow/deny) + updatedPermissions; PostToolUse/Stop/etc use top-level decision:"block"+reason; PermissionDenied uses hookSpecificOutput.retry. PreToolUse precedence is deny>defer>ask>allow, and PreToolUse hooks fire BEFORE permission-mode checks (a deny hook blocks even in bypassPermissions). Hooks run in parallel with dedup; output capped at 10000 chars.

## Components
### Configuration schema & resolution
**Purpose:** Defines where/how hooks are declared and merged across scopes

**Mechanism:** JSON config at 3 nesting levels: hook event name -> array of matcher groups (each {matcher, hooks:[]}) -> array of hook handler objects. On event fire: matcher evaluated against the input field (tool_name for tool events, source/reason/type for others); matched groups' handlers run in PARALLEL; identical handlers auto-deduped (command dedup by command+args, HTTP by URL). For tool events, an optional per-handler `if` field (permission-rule syntax like "Bash(git *)") filters further before spawning the process. Hooks run with user's full permissions and cwd = session cwd; env inherits parent plus CLAUDE_PROJECT_DIR, CLAUDE_PLUGIN_ROOT, CLAUDE_PLUGIN_DATA, CLAUDE_ENV_FILE, CLAUDE_CODE_REMOTE, CLAUDE_EFFORT. As of v2.1.139 macOS/Linux hooks run in their own session WITHOUT a controlling terminal (no /dev/tty).

**Data model:** settings.json: {"hooks": {<EventName>: [ {"matcher": "<pattern>", "hooks": [ <handlerObj> ] } ] }}. Matcher group = {matcher, hooks[]}. Handler (command) = {type:"command", command, args?, timeout?, async?, asyncRewake?, shell?, if?, statusMessage?, once?}. HTTP = {type:"http", url, headers?, allowedEnvVars?, timeout?}. mcp_tool = {type:"mcp_tool", server, tool, input?, timeout?}. prompt = {type:"prompt", prompt, model?, timeout?, continueOnBlock?}. agent = {type:"agent", prompt, model?, timeout?}.

**Config:** Hook timeout defaults: command/http/mcp_tool = 600s (10 min); UserPromptSubmit lowers these to 30s; MessageDisplay lowers to 10s; prompt = 30s; agent = 60s; SessionEnd = 1.5s default (raised to highest per-hook timeout up to 60s; CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS overrides). disableAllHooks:true disables all (managed hooks need managed-level disable). allowManagedHooksOnly blocks user/project/plugin hooks.

### Hook event catalog
**Purpose:** Enumerates every lifecycle point that can fire a hook

**Mechanism:** Events: SessionStart, Setup, UserPromptSubmit, UserPromptExpansion, PreToolUse, PermissionRequest, PermissionDenied, PostToolUse, PostToolUseFailure, PostToolBatch, Notification, MessageDisplay, SubagentStart, SubagentStop, TaskCreated, TaskCompleted, Stop, StopFailure, TeammateIdle, InstructionsLoaded, ConfigChange, CwdChanged, FileChanged, WorktreeCreate, WorktreeRemove, PreCompact, PostCompact, Elicitation, ElicitationResult, SessionEnd. Cadences: once/session (SessionStart/SessionEnd), once/turn (UserPromptSubmit/Stop/StopFailure), every tool call (PreToolUse/PostToolUse/etc.). Events without matcher support (always fire): UserPromptSubmit, PostToolBatch, Stop, TeammateIdle, TaskCreated, TaskCompleted, WorktreeCreate, WorktreeRemove, CwdChanged, MessageDisplay.

**Data model:** 30+ events total. Tool-loop: PreToolUse, PermissionRequest, PermissionDenied, PostToolUse, PostToolUseFailure, PostToolBatch. Per-turn: UserPromptSubmit, UserPromptExpansion, Stop, StopFailure. Per-session: SessionStart, Setup, SessionEnd. Subagent/team: SubagentStart, SubagentStop, TeammateIdle, TaskCreated, TaskCompleted. Display: MessageDisplay. Async/side-effect: Notification, InstructionsLoaded, ConfigChange, CwdChanged, FileChanged, WorktreeCreate, WorktreeRemove. Compaction: PreCompact, PostCompact. MCP elicitation: Elicitation, ElicitationResult.

**Config:** Event-specific matchers: PreToolUse/PostToolUse/PostToolUseFailure/PermissionRequest/PermissionDenied on tool_name; SessionStart on source(startup|resume|clear|compact); Setup on init|maintenance; SessionEnd on reason(clear|resume|logout|prompt_input_exit|bypass_permissions_disabled|other); Notification on permission_prompt|idle_prompt|auth_success|elicitation_dialog|elicitation_complete|elicitation_response; SubagentStart/SubagentStop on agent_type; PreCompact/PostCompact on manual|auto; ConfigChange on user_settings|project_settings|local_settings|policy_settings|skills; StopFailure on error type; InstructionsLoaded on load reason; UserPromptExpansion on command name; Elicitation/ElicitationResult on MCP server name; FileChanged = literal filenames split on |.

### Stdin JSON input contract
**Purpose:** The exact JSON payload passed to every hook

**Mechanism:** Every event's stdin JSON carries common fields plus event-specific fields. The matcher is evaluated against a specific field from this JSON (e.g. tool_name for PreToolUse).

**Data model:** Common stdin JSON: {session_id, transcript_path, cwd, permission_mode (default|plan|acceptEdits|auto|dontAsk|bypassPermissions), hook_event_name, effort:{level:low|medium|high|xhigh|max}}. Under --agent/subagent also: agent_id, agent_type. PreToolUse adds: tool_name, tool_input (tool-specific), tool_use_id. PostToolUse adds: tool_input, tool_response, tool_use_id, duration_ms. PermissionRequest adds: tool_name, tool_input, permission_suggestions[] (NO tool_use_id). Notification adds: message, title?, notification_type. Stop adds: stop_hook_active, last_assistant_message, background_tasks[], session_crons[]. SubagentStop adds: agent_id, agent_type, agent_transcript_path, last_assistant_message, stop_hook_active, background_tasks, session_crons. SessionStart adds: source, model?, agent_type?, session_title?. SessionEnd adds: reason. PreCompact/PostCompact add: trigger, custom_instructions/compact_summary.

**Config:** agent_id/agent_type only added when running under --agent or inside subagent. model field ONLY on SessionStart and not guaranteed. effort/CLAUDE_EFFORT only when model supports effort param.

### Exit code / stdout contract
**Purpose:** How a hook signals block/allow/error

**Mechanism:** Exit 0 = success; stdout parsed for JSON (only on exit 0). For UserPromptSubmit/UserPromptExpansion/SessionStart, stdout (even non-JSON) is added to Claude context. Exit 2 = BLOCKING error: stdout/JSON IGNORED, stderr fed back to Claude as error. Effect per event (PreToolUse blocks tool, UserPromptSubmit rejects prompt, Stop prevents stopping, PostToolUse just shows stderr since tool already ran, etc.). Any other exit code (incl 1) = NON-blocking error; transcript shows notice + first stderr line, execution continues. WorktreeCreate is the exception: ANY non-zero exit aborts creation.

**Data model:** Exit 0 + JSON: {continue:true, stopReason?, suppressOutput:false, systemMessage?, terminalSequence?, [decision/reason for block-events], [hookSpecificOutput:{hookEventName, ...}]}. Exit 2 + stderr -> blocking. Exit other -> non-blocking error notice '<hookname> hook error' + first stderr line in transcript.

**Config:** exclusive: exit codes OR exit-0 JSON, never both (exit 2 ignores JSON). stdout must be ONLY the JSON object (shell profile echoes break parsing). terminalSequence allowlist: OSC 0/1/2/9/99/777 + BEL only; anything else (CSI, OSC 8/52/1337) ignored. terminalSequence requires v2.1.141+.

### Decision control / output fields
**Purpose:** Per-event structured control beyond exit codes

**Mechanism:** Different events use different JSON shapes. (1) Top-level decision: UserPromptSubmit, UserPromptExpansion, PostToolUse, PostToolUseFailure, PostToolBatch, Stop, SubagentStop, ConfigChange, PreCompact -> {decision:"block", reason}. (2) hookSpecificOutput.permissionDecision: PreToolUse (allow/deny/ask/defer + reason + updatedInput + additionalContext). (3) hookSpecificOutput.decision.behavior: PermissionRequest (allow/deny + updatedInput + updatedPermissions + message + interrupt). (4) hookSpecificOutput.retry: PermissionDenied. (5) Exit code or continue:false: TeammateIdle, TaskCreated, TaskCompleted. (6) Path return: WorktreeCreate. (7) hookSpecificOutput.action: Elicitation/ElicitationResult. (8) hookSpecificOutput.displayContent: MessageDisplay. (9) Context only: SessionStart, Setup, SubagentStart. (10) None: Notification, SessionEnd, PostCompact, InstructionsLoaded, StopFailure, CwdChanged, FileChanged, WorktreeRemove.

**Data model:** Top-level decision: {decision:"block", reason}. PreToolUse: {hookSpecificOutput:{hookEventName:"PreToolUse", permissionDecision:"allow|deny|ask|defer", permissionDecisionReason?, updatedInput?, additionalContext?}}. PermissionRequest: {hookSpecificOutput:{hookEventName:"PermissionRequest", decision:{behavior:"allow|deny", updatedInput?, updatedPermissions?, message?, interrupt?}}}. PermissionDenied: {hookSpecificOutput:{hookEventName:"PermissionDenied", retry:true}}. PostToolUse: {hookSpecificOutput:{hookEventName:"PostToolUse", decision?, reason?, additionalContext?, updatedToolOutput?, updatedMCPToolOutput?}}. Stop/SubagentStop: top-level {decision:"block", reason} OR {hookSpecificOutput:{hookEventName:"Stop", additionalContext}}. SessionStart: {hookSpecificOutput:{hookEventName:"SessionStart", additionalContext?, initialUserMessage?, sessionTitle?, watchPaths?, reloadSkills?}}.

**Config:** PreToolUse precedence deny>defer>ask>allow. defer only in -p non-interactive (v2.1.89+), only single tool call in turn. additionalContext/updatedInput ignored on defer. PreToolUse deny fires BEFORE permission-mode checks (blocks even in bypassPermissions). Hooks can tighten but never loosen past deny rules.

### Prompt & agent hooks
**Purpose:** LLM-based judgment hooks vs deterministic command hooks

**Mechanism:** prompt hook: sends prompt+input to a Claude model (Haiku default, overridable via model field) single-turn; model returns {ok:true|false, reason}. ok:false -> decision:block with per-event behavior (Stop/SubagentStop feeds reason to Claude; PreToolUse denies; PostToolUse ends turn/warning). continueOnBlock:true feeds reason back instead of ending. agent hook: spawns subagent w/ Read/Grep/Glob, up to 50 turns, returns same {ok,reason}. Both support only the 13 events that allow prompt/agent type.

**Data model:** prompt hook: {type:"prompt", prompt:"...$ARGUMENTS...", model?, timeout:30, continueOnBlock?:false}. agent hook: {type:"agent", prompt, model?, timeout:60}.

**Config:** SessionStart/Setup only support command+mcp_tool (not http/prompt/agent). prompt default timeout 30s, agent 60s (up to 50 turns). continueOnBlock default false.

### Async hooks
**Purpose:** Non-blocking background execution

**Mechanism:** async:true (command hooks only): runs in background, Claude continues immediately. On exit, additionalContext delivered on NEXT turn (waits if idle). Cannot block/return decisions. asyncRewake:true implies async AND wakes Claude on exit code 2 (stderr or stdout shown as system reminder). No dedup across async firings.

**Data model:** async command hook: {type:"command", command, async:true, timeout?:600}. asyncRewake: {type:"command", command, asyncRewake:true}.

**Config:** async only on type:command. async hooks cannot block. asyncRewake implies async.

## Key behaviors
- PreToolUse fires BEFORE permission-mode checks: a hook returning permissionDecision:deny blocks the tool even in bypassPermissions mode or with --dangerously-skip-permissions. The reverse is NOT true — a hook allow does not override deny rules from any settings scope (incl managed). Hooks tighten but never loosen.
- Exit code 1 is NON-blocking (conventional Unix failure but treated as non-blocking error; action proceeds). ONLY exit code 2 blocks (exception: WorktreeCreate, where any non-zero aborts). Use exit 2 to enforce policy.
- Exit 2 and JSON output are mutually exclusive: exit 2 ignores stdout/JSON entirely. JSON is only parsed on exit 0. stdout must contain ONLY the JSON object (shell profile echoes break parsing — wrap in `if [[ $- == *i* ]]`).
- All matching hooks run to completion in parallel before results merge (one hook's deny does NOT stop sibling hooks). For PreToolUse the most restrictive wins: deny > defer > ask > allow. additionalContext from ALL hooks is kept and combined.
- PreToolUse previously used top-level decision/reason (now DEPRECATED for this event); legacy values 'approve'/'block' map to 'allow'/'deny'. Use hookSpecificOutput.permissionDecision instead. Other events (PostToolUse, Stop, etc.) STILL use top-level decision/reason as current format.
- Stop hooks have an 8-consecutive-block cap (CLAUDE_CODE_STOP_HOOK_BLOCK_CAP env raises it). Hooks receive stop_hook_active=true to detect re-entry and exit early. Stop hooks do NOT fire on user interrupts; API errors fire StopFailure instead (whose output/exit code are ignored).
- defer (PreToolUse) only works in -p non-interactive mode (v2.1.89+), only when Claude makes a SINGLE tool call in the turn, and exits with stop_reason:tool_deferred preserving deferred_tool_use{id,name,input}. Resume with claude -p --resume <session-id>. If deferred tool gone on resume -> stop_reason:tool_deferred_unavailable + is_error.
- Output cap: additionalContext, systemMessage, and plain stdout capped at 10000 chars. Over-cap saved to a file in session dir and replaced with preview+path. description fields in background_tasks/session_crons capped at 1000 chars.
- PostToolUse updatedToolOutput must match the tool's output schema (e.g. Bash returns {stdout,stderr,interrupted,isImage}); mismatched shape is IGNORED and original used. MCP tool output passes through without schema validation. Telemetry captures ORIGINAL output before hook.
- when multiple PreToolUse hooks return updatedInput, the LAST to finish wins (non-deterministic since parallel). Avoid >1 hook modifying same tool's input.
- Matchers are CASE-SENSITIVE. A matcher with ONLY letters/digits/_/| is exact-match or |-separated exact list. Any other char => treated as JavaScript regex. mcp__memory (only letters/_) matches NO tool — must use mcp__memory__.* (the .* makes it a regex).
- MessageDisplay is display-only (transcript + Claude see original; only on-screen rendered text changes), runs per-batch-of-lines interactively (once per full message in -p/SDK). default timeout 10s. No matcher. Only fires for assistant text messages, not tool results or typed text.
- PermissionRequest does NOT fire in -p non-interactive mode — use PreToolUse for automated decisions. updatedPermissions entries: addRules/replaceRules/removeRules/setMode/addDirectories/removeDirectories, each with destination session|localSettings|projectSettings|userSettings. setMode bypassPermissions only if session launched with bypass available; never persisted as defaultMode.
- ConfigChange can block all sources EXCEPT policy_settings (managed settings always apply; hooks fire for audit but block ignored). SessionEnd has 1.5s default timeout, budget raisable to 60s via per-hook timeout or CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS.
- Hooks in skills/agents use YAML frontmatter (same nested format). For subagents, Stop hooks auto-convert to SubagentStop. `once:true` only honored in skill frontmatter (ignored in settings/agent frontmatter).

## Open questions
- Exact JSON shape returned to the SDK for each exit-code/decision combination (e.g. the precise fields of the SDK result object beyond stop_reason:tool_deferred) — requires reading the claude-code-sdk TypeScript types, not just docs.
- Precise merge order when hooks from multiple scopes (user/project/local/managed/plugin/skill) collide on the same event+matcher — docs say plugin hooks 'merge' but the precedence on conflicts is underspecified.
- How `if` permission-rule syntax parses non-Bash tools (Edit(*.ts) etc.) at the token level — docs give a Bash table but not the full grammar for other tools.

## Sources
- [Hooks reference - Claude Code Docs](https://code.claude.com/docs/en/hooks) — Primary authoritative source: full reference for all 30+ hook events, config schema (matcher/handler fields), stdin JSON input, exit-code/JSON output contract, decision control table, async/prompt/agent/HTTP/mcp_tool hook types, and version-specific thresholds (v2.1.139/141/145/174/85/89, 10000-char cap, 1.5s SessionEnd, 8-block cap). Fetched via .md for complete untruncated content.
- [Automate actions with hooks - Claude Code Docs](https://code.claude.com/docs/en/hooks-guide) — Official guide confirming exit-code semantics (0=proceed/2=block/other=non-blocking error), PreToolUse permissionDecision allow/deny/ask + defer precedence, hooks-and-permission-modes interaction (deny blocks even in bypassPermissions), prompt/agent hook ok/reason schema, hook-not-firing and Stop-cap troubleshooting.
- [Claude Code & Agent SDK Hooks (2026) - morphllm](https://www.morphllm.com/claude-code-hooks) — Independent 2026 corroboration of the 30 hook events, stdin JSON shapes, exit codes, matchers, and timeouts; cross-checks official docs for currentness.
- [Claude Code Hooks: Complete Guide - claudefa.st](https://claudefa.st/blog/tools/hooks/hooks-guide) — Community cross-check confirming PreToolUse exit 2 stops the tool and the decision/JSON-output control flow.
- [Hooks reference - Claude Wiki](https://claude-wiki.com/hooks-reference.html) — Secondary corroboration of the command-vs-HTTP input/output contract and stdin/stdout/exit-code semantics.
