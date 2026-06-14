# Research: tool-exec-engine

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code's tool-exec engine sits between the model's `tool_use` content blocks and the `tool_result` blocks returned to the API. Every tool call — built-in (Read/Edit/Bash/Grep/Agent) or MCP — flows through one uniform 14-step pipeline (`checkPermissionsAndCallTool`): lookup → abort-check → Zod input validation → semantic `validateInput` → speculative classifier start → input backfill → PreToolUse hooks → permission resolution (deny→ask→allow rules + tool.checkPermissions + mode + interactive prompt) → deny hooks → `call()` execution → result budgeting (persist oversize to `~/.claude/tool-results/{hash}.txt`) → PostToolUse hooks → append newMessages → classifyToolError. Concurrency runs two layers: a greedy `partitionToolCalls()` groups consecutive concurrency-safe calls into parallel batches (isolating unsafe calls into serial singletons), and a `StreamingToolExecutor` starts tools speculatively *while the model is still streaming* its response. Results are buffered and yielded in submission order (not completion order) so conversation history stays coherent. Permission gating is layered: PreToolUse hooks can short-circuit, then static allow/ask/deny rules (`Tool` or `Tool(specifier)` format), then tool-specific checks, then one of 7 modes (default/acceptEdits/plan/auto/dontAsk/bypassPermissions/bubble). MCP tools are registered as `mcp__<server>__<tool>` and are indistinguishable to the agent loop.

## Components
### Tool-call lifecycle (API + in-process)
**Purpose:** Translate a model tool_use block into a validated, permission-gated, executed tool_result content block, preserving message-history invariants.

**Mechanism:** 1) Stream assistant response, parse each tool_use block. 2) For each: look up tool def (alias-fallback to getAllBaseTools for renamed tools in old transcripts), abort-check, Zod safeParse input (on failure append hint to call ToolSearch for deferred tools), semantic validateInput (e.g. FileEdit rejects no-ops, Bash blocks standalone sleep when MonitorTool present). 3) Speculatively start auto-mode classifier for Bash. 4) Backfill derived fields (expand ~/foo) into a CLONED input (original kept for transcript). 5) Run PreToolUse hooks — can allow/deny/modify/stop; hook allow does NOT bypass deny/ask rules; exit code 2 blocks before rule eval. 6) canUseTool(): if hook decided, final; else deny→ask→allow rule match → tool.checkPermissions() → mode default → interactive prompt or classifier. 7) On deny build error msg + run PermissionDenied hooks. 8) call(input=original). 9) Result budget. 10) PostToolUse hooks (can modify MCP output / block). 11) Append newMessages. 12) classifyToolError for telemetry.

**Data model:** API contract (Anthropic Messages): assistant turn with stop_reason='tool_use' contains 1+ tool_use blocks {id:'toolu_...', name, input}. Client must reply with ONE user message whose content array begins with tool_result blocks {tool_use_id, content?, is_error?} — text blocks MUST come AFTER all tool_results, else HTTP 400. Multiple tool_result blocks for one turn MUST be batched in a single user message (separate messages break future parallel-tool-use prompting). Server tools (web_search, code_execution) execute inside Claude and need no tool_result.

**Config:** settings.json: permissions.{allow,ask,deny} string arrays; permissions.defaultMode; --permission-mode / --dangerously-skip-permissions CLI flags. ENABLE_TOOL_SEARCH unset|true|auto|auto:N|false controls MCP deferral. MAX_MCP_OUTPUT_TOKENS, MCP_TOOL_TIMEOUT.

### Permission resolution chain
**Purpose:** Decide allow/deny/ask per tool invocation using deny→ask→allow precedence layered over 7 modes.

**Mechanism:** Rule string format 'Tool' or 'Tool(specifier)'. Bare deny removes tool from context entirely; scoped deny (Bash(rm *)) leaves tool visible and blocks the matching call. Bash rules: glob '*' (space before * = word boundary; ls* matches lsof, ls * does not); ':*' suffix == trailing ' *'; separators && || ; | |& & newline split compound commands and EACH subcommand must match (max 5 rules saved per compound approval); process wrappers timeout/time/nice/nohup/stdbuf and bare xargs are stripped; read-only set (ls cat echo pwd head tail grep find wc which diff stat du cd + read-only git) never prompts. Read/Edit use gitignore patterns with 4 anchors: //abs, ~/home, /project-rel, ./cwd-rel. WebFetch uses domain: prefix (* matches within a label except leading *. or whole-pattern). MCP rules: mcp__<server>, mcp__<server>__*, mcp__<server>__tool (allow globs only after literal mcp__server__ prefix; unanchored allow globs are warned+skipped). Protected paths (.git, .claude except worktrees, .vscode, .idea, .husky, etc + named rc/config files) never auto-approved except in bypassPermissions.

**Data model:** PermissionRule = { source, ruleBehavior: 'allow'|'deny'|'ask', ruleValue: 'Tool' | 'Tool(specifier)' }. Settings precedence (highest wins): Managed > CLI args > .claude/settings.local.json > .claude/settings.json > ~/.claude/settings.json. A deny at ANY level cannot be overridden.

**Config:** Seven modes: default, acceptEdits (auto-allows edits + mkdir/touch/rm/rmdir/mv/cp/sed in-scope), plan (read-only, denies writes), dontAsk (auto-deny prompts, CI), bypassPermissions (allow all; since v2.1.126 includes protected paths; rm -rf / and rm -rf ~ STILL prompt as circuit breaker; refuses root/sudo outside sandbox), auto (classifier model; v2.1.83+; consecutive 3 or total 20 blocks → fall back to prompting). Shift+Tab cycles default→acceptEdits→plan. disableBypassPermissionsMode / disableAutoMode = 'disable' locks them.

### Concurrency: partition + streaming executor
**Purpose:** Run independent read-only tools in parallel; serialize writes; overlap tool execution with model response streaming.

**Mechanism:** partitionToolCalls() walks calls L→R, safeParse input, calls isConcurrencySafe(parsedInput) in try-catch (failure→serial), merges consecutive-safe calls into one concurrent batch, isolates unsafe calls into single-tool serial batches. Concurrent: runToolsConcurrently via bounded async-generator all() with limit. Serial: apply contextModifier immediately. TWO OPTIMIZATIONS: (a) speculative execution — StreamingToolExecutor.addTool() is fire-and-forget called per parsed tool_use during streaming; processQueue() admits a tool iff noToolsRunning || (newToolSafe && allRunningSafe); (b) batch dispatch after stream completes. RESULTS YIELDED IN SUBMISSION ORDER not completion order — getCompletedResults() breaks the walk at any executing serial tool (order preservation via buffering). Context modifiers only applied for serial tools; concurrent-batch modifiers queued by tool_use_id and applied in submission order after batch. discard() escape hatch sets discarded=true so retry stream starts fresh.

**Data model:** Partition = []Group{ parallel:bool, calls:[]ToolCall }. TrackedTool states: queued|executing|completed|yielded. ToolResult<T>={ data, newMessages?, contextModifier? }. AbortController hierarchy: query-level (Ctrl+C) → sibling-level (Bash-error cascade) → per-tool.

**Config:** CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY (default 10) bounds concurrent batch size. Tools declare interruptBehavior() 'cancel'|'block' (block is default).

### Result budgeting
**Purpose:** Bound tool output size per-call and per-conversation to avoid context overflow.

**Mechanism:** Per-tool maxResultSizeChars threshold → oversize output persisted to ~/.claude/tool-results/{hash}.txt and replaced with <persisted-output> preview block (model re-Reads full content). ContentReplacementState tracks an aggregate conversation budget (death-by-a-thousand-cuts guard). BashTool detects image output by magic bytes → emits image content block; FileReadTool emits base64 image blocks, handles PDFs/notebooks/dirs, blocks /dev/zero /dev/random /dev/stdin.

**Data model:** Persisted file path ~/.claude/tool-results/{hash}.txt; wrapper replaces in-content.

**Config:** maxResultSizeChars per tool (Bash 30000, FileEdit 100000, Grep 100000, FileRead Infinity). MCP: MAX_MCP_OUTPUT_TOKENS default 25000, warning at 10000; per-server .mcp.json timeout overrides MCP_TOOL_TIMEOUT; tool can raise limit to 500000 via _meta['anthropic/maxResultSizeChars'].

### MCP tool routing & registry
**Purpose:** Expose external MCP server tools as first-class tools indistinguishable from built-ins to the agent loop.

**Mechanism:** Spawn server (stdio/SSE/HTTP) → JSON-RPC 2.0 initialize → tools/list discovers → register with mcp__ prefix → route tools/call transparently. assembleToolPool(): built-ins (deny-filtered, REPL-hidden, isEnabled-checked) sorted alphabetically THEN MCP tools sorted alphabetically, concatenated (built-ins prefix) so a prompt-cache breakpoint sits after the last built-in — flat-sorted interleaving would bust cache on MCP add/remove. MCP tools go through the SAME 14-step pipeline. Tool search/deferred loading (ENABLE_TOOL_SEARCH default-on for MCP): tools sent with defer_loading=true (name+desc only, no schema); model calls ToolSearchTool to load schema; calling a deferred tool without loading → Zod string-coercion failure + targeted recovery hint.

**Data model:** Tool name mcp__<server>__<tool> (chars outside [A-Za-z0-9_-] → _, capped 64). Plugin form mcp__plugin_<plugin>_<server>__<tool>. MCP tool schema = JSON Schema; input validated same as built-ins.

**Config:** MAX_MCP_OUTPUT_TOKENS, MCP_TOOL_TIMEOUT, ENABLE_TOOL_SEARCH, .mcp.json (project root, checked into VCS), .claude.json (user scope).

### Error classification & recovery
**Purpose:** Convert execution failures into model-actionable tool_result(is_error) without leaking internals, and keep conversation history coherent.

**Mechanism:** classifyToolError() extracts telemetry-safe string (errno, stable name) — never logs raw msg (minified builds mangle constructor.name). Parallel batch: only Bash non-zero-exit errors cascade (cancel sibling controller → synthetic 'Cancelled: parallel tool call <cmd/file first 40 chars> errored'); Read/Grep/Fetch errors are isolated (no sibling cancel). Dependencies across parallel calls (create-then-update) are NOT pre-detected: dispatch all, if one fails return is_error:true with natural message, model reissues next turn. Orphaned tool_use (interrupted parallel call) must still get a placeholder tool_result or API 400s. MaxTokens stop_reason with partial tool_use: still emit tool_result blocks for the partial calls.

**Data model:** tool_result.is_error=true with natural stderr-style content. Stop reasons: tool_use (run tools), end_turn, max_tokens, pause_turn, refusal, model_context_window_exceeded, etc.

**Config:** CLAUDE_CODE_MAX_OUTPUT_TOKENS bounds model output; MaxTokens stop surfaces that error.

## Key behaviors
- RESULTS ARE YIELDED IN SUBMISSION (tool_use arrival) ORDER, NOT COMPLETION ORDER. Buffer completed results; getCompletedResults() BREAKS the walk at any still-executing serial tool so nothing after it yields early. This is the single hardest correctness invariant to preserve in a reimpl.
- Concurrency safety is PER-INVOCATION, not per-tool. isConcurrencySafe(parsedInput) is called after safeParse; any parse failure or thrown exception → serial (fail-closed). BashTool parses compound commands via splitCommandWithOperators and returns true only if EVERY non-neutral subcommand is in search/read/list sets.
- Mutual exclusion contract in the streaming executor: a tool can start iff noToolsRunning OR (newToolSafe AND allRunningAreSafe). A single non-concurrent tool in flight blocks everyone.
- Bash errors are the ONLY errors that cascade to sibling cancellation in a parallel batch (synthesize 'Cancelled: parallel tool call <x> errored'). This is confirmed production behavior (v2.1.158, issue #64247) and a known bug source — Opus 4.8 spirals on the synthetic cancel messages. Read/Grep errors do NOT cancel siblings.
- tool_result blocks for a parallel turn MUST be batched in a single user message and MUST come before any text blocks. Splitting results across messages or putting text first 'teaches' the model to stop using parallel tools and can cause HTTP 400.
- Permission rule precedence is deny → ask → allow (first match), REGARDLESS of specificity. A matching ask rule prompts even if a more specific allow matches. A deny at ANY settings level is absolute. Hook decisions do not bypass deny/ask rules; hook exit-code-2 blocks before rule eval.
- Bare deny rule (e.g. 'Bash') REMOVES the tool from model context entirely; scoped deny ('Bash(rm *)') keeps the tool visible and blocks only matching calls. Bash wildcard space sensitivity: 'Bash(ls *)' matches 'ls -la' not 'lsof'; 'Bash(ls*)' matches both. ':*' suffix == trailing ' *' but only at pattern end.
- Speculative execution during streaming: StreamingToolExecutor.addTool() is fire-and-forget (does not await processQueue) so response parsing never stalls; tools can finish before the model response completes. Abort-controller hierarchy is 3 levels (query→sibling→per-tool); per-tool abort bubbles to query controller unless reason is a sibling error (so permission denial ends the whole turn).
- FileReadTool is the ONLY built-in with maxResultSizeChars=Infinity (persisting Read output would loop). It self-bounds via token estimation. MCP default output token limit is 25000 (warn at 10000); a tool can raise to hard ceiling 500000 via _meta['anthropic/maxResultSizeChars'].
- assembleToolPool sorts built-ins and MCP tools alphabetically SEPARATELY then concatenates (built-ins prefix) to keep a stable prompt-cache breakpoint after the last built-in — flat-sorting all tools would invalidate cache when MCP servers change.
- Tool search/defer_loading (default-on for MCP): sends name+description only; model calls ToolSearch to load schema. Disabled by default on Vertex AI and when ANTHROPIC_BASE_URL is non-first-party. Requires tool_reference support (no Haiku). Calling a deferred tool un-triggered → Zod string-coercion failure + recovery hint.
- bypassPermissions (v2.1.126+) includes protected-path writes but rm -rf / and rm -rf ~ still prompt as a circuit breaker; refuses to start as root/sudo outside recognized sandboxes. auto mode classifier thresholds (consecutive 3 / total 20 blocks) are NOT configurable.

## External interfaces
- Anthropic Messages API: stop_reason='tool_use' with tool_use{id,name,input} blocks; reply user message with tool_result{tool_use_id,content,is_error} blocks (all results in ONE user message, no text before tool_results)
- Internal: checkPermissionsAndCallTool() 14-step pipeline; partitionToolCalls() in toolOrchestration.ts; StreamingToolExecutor{addTool,processQueue,executeTool,getCompletedResults,getRemainingResults,discard}; canUseTool()
- Tool interface: call(input)→ToolResult{data,newMessages,contextModifier}; inputSchema (Zod→JSON Schema); isConcurrencySafe(input); isReadOnly(input); checkPermissions(input); validateInput(); isEnabled(); interruptBehavior(); maxResultSizeChars
- Config files: ~/.claude/settings.json, .claude/settings.json, .claude/settings.local.json (permissions.{allow,ask,deny,defaultMode}); .mcp.json (project MCP), .claude.json (user MCP); ~/.claude/tool-results/{hash}.txt (persisted oversize output)
- MCP JSON-RPC 2.0: initialize, tools/list (supports _meta anthropic/maxResultSizeChars up to 500000), tools/call
- CLI flags: --permission-mode, --dangerously-skip-permissions, --allow-dangerously-skip-permissions, --add-dir, --allowedTools, --disallowedTools
- Env vars: CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY(10), MAX_MCP_OUTPUT_TOKENS(25000), MCP_TOOL_TIMEOUT, ENABLE_TOOL_SEARCH, CLAUDE_CODE_MAX_OUTPUT_TOKENS, CLAUDE_CODE_ENABLE_AUTO_MODE

## Open questions
- Exact set and order of fields in the Zod input backfill / _simulatedSedEdit injection (only approximate from secondary source)
- Whether contextModifier queuing for concurrent batches is actually exercised by any current built-in (source comment says none are)
- Precise mapping of the auto-mode classifier's decision order vs the in-process 14-step pipeline (two slightly different orderings are described)
- Exact behavior when an orphaned tool_use from an interrupted parallel turn is repaired (placeholder tool_result content text)

## Sources
- [Handle tool calls — Claude API Docs](https://platform.claude.com/docs/en/agents-and-tools/tool-use/handle-tool-calls) — Authoritative API contract: tool_use/tool_result block shapes, is_error, ordering rules (tool_result must immediately follow, must be first in user content, HTTP 400 cases).
- [Parallel tool use — Claude API Docs](https://platform.claude.com/docs/en/agents-and-tools/tool-use/parallel-tool-use) — disable_parallel_tool_use semantics, unordered execution, dependency recovery via is_error, single-user-message batching rule.
- [Ch 6. Tools — From Definition to Execution (Claude Code from Source)](https://claude-code-from-source.com/ch06-tools/) — Best secondary source: 14-step checkPermissionsAndCallTool pipeline, buildTool fail-closed defaults, Tool interface (5 key members), ToolResult/ToolUseContext, registry assembleToolPool, deferred loading, per-tool maxResultSizeChars table.
- [Ch 7. Concurrent Tool Execution (Claude Code from Source)](https://claude-code-from-source.com/ch07-concurrency/) — partitionToolCalls algorithm, streaming executor lifecycle (queued/executing/completed/yielded), mutual-exclusion admission, order-preservation, Bash-only sibling cascade, discard() escape hatch, per-tool concurrency table.
- [Configure permissions — Claude Code Docs](https://code.claude.com/docs/en/permissions) — Official rule syntax: deny→ask→allow precedence, Bash wildcards (space-before-*, :* suffix), compound command splitting, process-wrapper stripping, Read/Edit gitignore anchors, WebFetch domain:, MCP mcp__server__tool rules, protected paths, settings precedence.
- [Choose a permission mode — Claude Code Docs](https://code.claude.com/docs/en/permission-modes) — Six modes table (default/acceptEdits/plan/auto/dontAsk/bypassPermissions), what each auto-approves, auto-mode classifier thresholds (3 consecutive / 20 total), v2.1.126 protected-path change, rm -rf / circuit breaker, auto-mode model requirements.
- [Connect Claude Code to tools via MCP — Claude Code Docs](https://code.claude.com/docs/en/mcp) — MCP tool naming mcp__server__tool (64-char cap, char substitution), plugin form mcp__plugin_X_Y__Z, MAX_MCP_OUTPUT_TOKENS=25000 default (warn 10000), _meta anthropic/maxResultSizeChars ceiling 500000, tool search/defer_loading (ENABLE_TOOL_SEARCH), JSON-RPC 2.0 tools/list + tools/call.
- [[Bug] Parallel tool calls cancel all siblings on single error (#64247)](https://github.com/anthropics/claude-code/issues/64247) — Confirms exact behavior + version (v2.1.158): 'Cancelled: parallel tool call ... errored', isConcurrencySafe→annotations.readOnlyHint, Bash-error sibling cascade.
- [Environment variables — Claude Code Docs](https://code.claude.com/docs/en/env-vars) — Confirms CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY default 10 governs read-only tool + subagent parallelism.
- [toolOrchestration.ts (openonion/claude-code mirror)](https://github.com/openonion/claude-code/blob/main/src/services/tools/toolOrchestration.ts) — Source confirmation of getMaxToolUseConcurrency() = parseInt(env.CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY)||10 and runToolsConcurrently signature.
