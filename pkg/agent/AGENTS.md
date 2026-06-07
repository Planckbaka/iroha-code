<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-23 | Updated: 2026-06-05 -->

# agent

## Purpose
Core agent orchestration: runner lifecycle, SWE tool definitions (40+ tools), human-in-the-loop permission system, multi-type hook pipeline (command/HTTP/LLM-prompt), cross-session memory with LLM-assisted consolidation, prompt builder, task DAG, cron scheduler, background execution, team coordination with process isolation, protocol handshake, autonomous polling, git worktree isolation, MCP plugin routing, LSP client with 5 code-intelligence tools, multi-agent pool, subagent delegation with worktree isolation, session persistence, diff generation, CI monitoring, audit logging, OS-level sandboxing (macOS sandbox-exec / Linux bwrap), plugin manifest system, skill discovery and trigger matching, tokenizer, watchdog crash recovery, web fetch/search with SSRF protection, and IPC bridge for inter-process communication.

## Key Files
| File | Description |
|------|-------------|
| `runner.go` | `CustomRunner` -- wraps ADK runner, manages async execution, `ConfirmationBridge` channels, `ToolStatusBridge`, `blockingConfirmationTool` wrapper, hook pipeline (PreToolUse -> execute -> PostToolUse), `ToolCircuitBreaker` (3 consecutive failures -> auto-block) |
| `runner_bridge.go` | `ConfirmationBridge` -- async channel pair (`PromptChan`/`ResponseChan`) between runner goroutine and TUI main thread, plus cancellation via `CancelChan`; `ToolStatusBridge` -- buffered status channel with background drain worker for real-time tool state updates |
| `runner_confirmation.go` | `blockingConfirmationTool` -- intercepts every tool call for permission check (`GlobalPermissionManager.Check`), auto-review via `ReviewCommand`/`ReviewFileOperation`, human confirmation loop with support for `y`/`n`/`always`/`bypass`/`edit:`/`explain` responses, LLM-powered explanation on demand |
| `runner_confirmation_hooks.go` | `ToolCircuitBreaker` -- tracks consecutive identical-arg failures per tool, blocks after 3 strikes; `runWithHooks` -- three-stage tool execution pipeline: Stage A (PreToolUse hooks with input rewrite), Stage B (execute tool + circuit breaker check), Stage C (PostToolUse hooks + self-healing post-edit compile verification) |
| `runner_edit.go` | File edit snapshot and rollback helpers (`snapshotFile`, `rollbackPendingEdits`, `whitespaceTolerantEdit`) for atomic edit operations |
| `runner_exec.go` | Shell command execution, streaming output, sandbox wrapping |
| `tools.go` | Tool registration and dispatch -- registers all 40+ SWE tools with the ADK agent builder, delegates handlers to `tools_*.go` files |
| `tools_file.go` | File tools: `file_read` (line-range support, self-repair suggestions), `file_write` (auto-mkdir), `file_edit` (exact match, whitespace-tolerant fallback, dry-run mode) |
| `tools_file_batch.go` | `file_edit_batch` -- atomic multi-edit with two-phase validation and full rollback on any failure, up to 50 edits per batch |
| `tools_file_search.go` | `list_directory` (recursive up to depth 4, 200 entry cap), `search_grep` (regex, 1MB file limit, 50 match cap, `.git`/`node_modules` exclusion), `find_files` (glob with `**` support) |
| `tools_shell.go` | Shell tool: `shell_run` -- command execution with 30s timeout, 500-line stream cap, sandbox validation |
| `tools_mcp.go` | MCP tools: `mcp_server_list` -- lists connected MCP plugin servers |
| `tools_memory.go` | Memory tools: `memory_save`, `memory_list` -- persistent memory CRUD via `MemoryManager` |
| `tools_schedule.go` | Schedule tools: `schedule_create`, `schedule_list`, `schedule_delete` -- cron job management via `CronScheduler` |
| `tools_task.go` | Task tools: `task_create`, `task_update`, `task_list`, `task_get` -- DAG task management via `TaskManager` |
| `tools_team.go` | Team tools: `spawn_teammate`, `send_message`, `team_status` -- team coordination via `TeamManager` |
| `tools_todo.go` | Todo tool: `todo` -- session-level progress planning via `TodoManager` |
| `tools_worktree.go` | Worktree tools: `worktree_create`, `worktree_remove`, `worktree_status` -- git worktree isolation via `WorktreeManager` |
| `tools_subagent.go` | `spawn_subagent` tool -- delegates synchronous subagent execution via `GlobalSubagentManager.RunSubagent` |
| `tools_web.go` | `web_fetch` (HTTP GET with HTML-to-text conversion, 5MB limit, rate-limited 10/min) and `web_search` (DuckDuckGo HTML scraping or SearXNG JSON backend), both using SSRF-safe HTTP client |
| `tools_web_safety.go` | SSRF protection infrastructure: `rateLimiter` (sliding window), private IP detection (`isPrivateIP`), DNS-rebinding-safe `http.Transport` (`ssrfSafeTransport`), HTML-to-text converter stripping script/style/svg/iframe |
| `pool.go` | `AgentPool` -- multi-agent runner pool with per-agent LLM config, `GlobalAgentPool` singleton, dynamic runner creation with tool injection |
| `lsp.go` | `LSPClient` -- Language Server Protocol client over stdio JSON-RPC 2.0, supports `textDocument/completion`, `textDocument/definition`, `textDocument/references`, `textDocument/hover`, `textDocument/diagnostics` |
| `lsp_tools.go` | LSP tool handlers: `lsp_goto_definition`, `lsp_find_references`, `lsp_document_symbols`, `lsp_hover`, `lsp_diagnostics` -- each resolves paths, validates sandbox, calls LSP server, returns structured results with file snippets |
| `lsp_types.go` | LSP JSON-RPC types (`jsonrpcRequest`/`Response`/`Error`), LSP protocol types (`lspPosition`/`Range`/`Location`/`DocumentSymbol`), tool argument/result structs for all 5 LSP tools, `DefaultLSPServers` (gopls, typescript-language-server, pyright-langserver, rust-analyzer), `SetLSPServers` merge logic |
| `lsp_utils.go` | LSP utility functions: `pathToURI`/`uriToPath` conversion, `parseLocations` (handles single Location, Location array, LocationLink array), `getSnippet` (reads 15-line code preview), `symbolKindToString`, `registerLSPTools` (lazy config loading) |
| `git_helper.go` | Git utilities: `GitHasChanges`, `GitGetStagedDiff`, `GitGetCurrentBranch` -- porcelain helpers for CI/worktree integrations |
| `session_store.go` | `PersistentSessionService` -- wraps ADK `session.InMemoryService` with JSON persistence in `~/.iroha/sessions/`, CRUD + fork, session metadata, stale session GC |
| `session_store_helpers.go` | Session helpers: `estimateTokens` (text-len/4), `estimateCost`, `getFirstPrompt` (session title extraction, 60-char cap), `GetSessionsDir`, `CleanOldSessions` (age-based GC), `ValidateResume` (integrity checks for CWD, events, state, archive) |
| `permission.go` | `PermissionManager` -- rule-based allow/deny/ask with bash security validation, three modes (default/plan/auto), path and content pattern matching |
| `hooks.go` | `HookManager` -- external hook scripts loaded from `~/.iroha/hooks.json` and `./.iroha/hooks.json`, exit-code protocol (0=continue, 1=block, 2=inject), matcher support |
| `hooks_types.go` | Hook type definitions: 12 `HookEvent` constants (SessionStart/End, UserPrompt, AgentResponse, PreToolUse, PostToolUse, ToolError, Compaction, SubagentStop, Notification, PreCompact, PostCompact), 3 `HookType` (command, http, llm-prompt), `HookDef`/`HookConfig`/`HookContext`/`HookResult` structs, `hookTimeoutForEvent` per-category timeouts, `parseJSONResult` for JSON-mode hooks, `mergePluginHooks` |
| `hooks_exec.go` | Hook execution engine: `RunHooks` (dispatches by matcher, handles async hooks), `runHTTP` (POST with JSON payload, env-var header expansion, allowed-env-vars restriction), `runLLMPrompt` (LLM-based compliance audit with strict JSON decision parsing), `runCommand` (shell subprocess with whitelisted env vars, stdin JSON payload, dual JSON/exit-code protocol) |
| `memory.go` | `MemoryManager` -- file-based persistent memory with YAML frontmatter, four types (user/feedback/project/reference), two-layer storage (global `~/.iroha/memory/` + project `.iroha/memory/`), `MEMORY.md` index |
| `memory_frontmatter.go` | Memory type system (`MemoryType` constants: user/feedback/project/reference), `MemoryEntry` struct, `MaxMemoryEntries` cap (100), YAML frontmatter parse/render (`parseFrontmatter`/`renderFrontmatter`), `slugify` for safe filenames |
| `memory_helpers.go` | Memory utility helpers: `tokenizeKeywords` (lowercase word splitter with stop-word filter), `projectMemoryDir` (resolves `./.iroha/memory` with auto-create) |
| `memory_agents_sync.go` | `syncToAgentsMD` -- bidirectional sync between `MemoryManager` entries and the `## Agent Dynamic Learnings
- **test-mem** (user): desc
  - *Content*:
    hello
- **a** (user): a
  - *Content*:
    x
- **b** (feedback): b
  - *Content*:
    y
- **alpha** (user): alpha
  - *Content*:
    alpha content
- **up** (user): new desc
  - *Content*:
    new content

` section of `AGENTS.md`; `syncFromAgentsMDLocked` -- parses AGENTS.md blocks back into memory files with mutex protection |
| `memory_dream.go` | `DreamConsolidator` -- automated memory consolidation with 7-gate validation (enabled, memory dir exists, not plan mode, cooldown, throttle, session count, PID lock); 4-phase consolidation: Orient, Gather, Consolidate (exact dedup + LLM semantic merge), Prune (enforce 100-entry cap) |
| `prompt.go` | `SystemPromptBuilder` -- dynamic prompt assembly with cache-friendly stable/dynamic boundary (`=== DYNAMIC_BOUNDARY ===`), CLAUDE.md layering, skill injection, live task/team/worktree context |
| `todo_manager.go` | `TodoManager` -- session-level task planning with status tracking (pending/in_progress/completed), max 12 items, nag reminder after 3 rounds without update |
| `task.go` | `TaskManager` -- durable work graph (DAG) persisted as JSON files in `.tasks/`, bidirectional edge reconciliation, DFS cycle detection, auto-created placeholder nodes |
| `background.go` | `BackgroundManager` -- slow-running shell commands in background goroutines, 5-min timeout, result preview, notification queue for next-turn delivery |
| `cron.go` | `CronScheduler` -- 5-field cron expression evaluator, PID-based lock for multi-session safety, durable/session storage, jitter on :00/:30 marks, 7-day auto-expiry, missed-task detection |
| `cron_helpers.go` | `CronLock` -- PID-based file lock with stale detection (`isPIDAlive` via signal 0), `cronMatches`/`fieldMatches` -- 5-field cron expression parser with range/step/comma/Sunday(0|7) support, `hashString` for jitter |
| `team.go` | `TeamManager` -- persistent specialist teammates with JSONL mailbox inbox, background polling loops, broadcast, `ProcessMessage` callback for LLM integration |
| `team_types.go` | Team type definitions: `TeamMessage` (sender/content/timestamp/extra), `Teammate` (name/role/type/status/lastActive), `TeamConfig` (roster), `TeamManager` struct with isolation mode fields (IPC bridge, watchdogs, binary path, cancel funcs) |
| `team_message.go` | Team mailbox operations: `AppendToInbox` (JSONL append), `ReadAndClearInbox` (atomic read+truncate), `PeekInbox` (non-destructive read), `Broadcast` (fan-out to all teammates except sender), `splitJSONLines` helper |
| `team_process.go` | Process-isolated team execution: `StartTeammateLoop` (goroutine or child process mode), `EnableProcessIsolation` (configures IPC bridge), `StartTeammateProcess` (spawns child with watchdog + heartbeat checker), `StopTeammateProcess`, `RunTeammateMode` (child-process entry point with IPC message loop and heartbeat ticker) |
| `subagent.go` | `SubagentManager` + `SubagentSpec`/`SubagentResult` -- synchronous subagent execution with worktree isolation for executor types, curated toolsets per type (explore=read-only, executor=all), cheaper model routing (haiku/flash/mini), JSONL execution logging, git diff analysis for file change detection |
| `protocol.go` | `ProtocolManager` -- structured request-response handshake (shutdown/plan_approval) persisted as JSON, single-use pending->approved/rejected lifecycle |
| `autonomous.go` | `AutonomousManager` -- task auto-polling and state transitions (WORK/IDLE), keyword-based task claiming for specialist agents |
| `mcp.go` | `MCPClient` + `MCPToolRouter` -- stdio-based JSON-RPC 2.0 lifecycle over child processes, dynamic tool discovery and ADK wrapping, plugins loaded from `.iroha/plugins.json` |
| `mcp_client.go` | Standalone `MCPClient` -- stdio JSON-RPC 2.0 client with pending-request map, 10s call timeout, MCP initialize handshake (`protocolVersion: 2024-11-05`), `SendNotification`, `Close` with process kill |
| `worktree.go` | `WorktreeManager` -- git worktree creation/removal/keep, JSON index + JSONL event log, cascading task status updates on closeout |
| `plugin.go` | `PluginManager` -- discovers and validates `plugin.json` manifests from `~/.iroha/plugins/*/` and `.iroha/plugins/*/`; `PluginManifest` (ID, name, semver version, MCP servers, hooks, skills, permissions); `ValidateManifest` (semver regex, plugin ID regex, no double underscores); `MigratePluginsConfig` (legacy flat -> manifest); `MergeMCPServers` (namespaced `pluginID__serverName`); `MergeHooks` |
| `skills.go` | `SkillManager` -- discovers `skill.json` manifests from `~/.iroha/skills/*/` and `.iroha/skills/*/`; `SkillManifest` (ID, name, triggers, type); three `SkillType`: `model_invoked` (keyword-triggered auto-injection), `user_invoked` (`/skill` command), `always` (permanent injection); `MatchTriggers` (case-insensitive keyword scan); `LoadInstructions` (path-escaped SKILL.md reader) |
| `sandbox.go` | OS-level sandboxing: `WrapSandboxCommand` dispatches to `wrapMacSandbox` (sandbox-exec with deny-write profile for /System, /usr, ~/.ssh, ~/.aws + allow-write for workspace/tmp/caches) or `wrapLinuxSandbox` (bwrap with read-only root + writable workspace/bind cache); `tokenizeCommand` (quote-aware shell tokenizer that blocks backticks, `$()`, pipes, `&&`, `;`, `>`, `<`); `safePrefixes` (configurable via `IROHA_SAFE_PREFIXES`) |
| `auto_review.go` | Hybrid safety review for `shell_run`: heuristic rules first, then LLM semantic analysis, then local dangerous-pattern double-check |
| `auto_review_apply.go` | `heuristicReview` -- rule-based safety check: newline injection, tokenizer-based subcommand splitting, command substitution detection, dangerous command names (rm/curl/sudo/etc), shell metacharacters (`;|&$<>\``), safe read-only command whitelist, path traversal detection |
| `auto_review_diff.go` | Phase 2 expanded security checks: `normalizeCommand` (strip quotes/backslashes/collapse whitespace/lowercase), 10 regex-based detectors for heredoc abuse, env expansion in write context, process substitution, named pipes, TTY escape sequences, file descriptor manipulation, unsafe source, encoding attacks, proxy injection, unsafe find-pipe-to-rm |
| `compaction.go` | Conversation micro-compaction and archival -- large tool outputs archived to transcripts, LLM-based conversational summarization (falls back to text extraction when no LLM provided) |
| `compaction_helpers.go` | Compaction helpers: `extractStickyBlocks`/`capStickyContent` (sticky block extraction with byte-budget trimming), `truncateOnlySummary` (circuit-breaker fallback), `extractStructuredSummary` (tool names, file paths, key decisions -> `[SUMMARY]` block), `summarizeRounds` (LLM-based or text-extraction fallback with 8K transcript cap) |
| `diff.go` | LCS-based unified diff generator for file edit previews |
| `ci_watcher.go` | GitHub Actions CI status monitoring via `gh` CLI |
| `logger.go` | Dual JSONL + plaintext audit logger with secret redaction |
| `ipc.go` | `IPCBridge` -- Unix domain socket inter-process communication; length-prefixed JSON messages (4-byte big-endian header, 10MB safety cap); `Start` (parent listener), `Connect` (child dial), `Send`/`SendToParent`, `Receive` channel, `SetOnMessage` callback; `readMessage`/`writeMessage` framing |
| `watchdog.go` | `Watchdog` -- child process crash-tolerance manager: configurable crash budget with time-window pruning, `Start`/`Monitor` (auto-restart loop), `Stop` (SIGINT + 5s kill timeout), `Checkpoint`/`Recover` (JSON state persistence), `EnqueueDeadLetter`/`DrainDeadLetters` (disk-backed message queue for crash recovery) |
| `tokenizer.go` | `tokenizeShellCommand` -- state-machine shell command splitter that correctly handles single/double quotes, backslash escapes, and operators (`;`, `|`, `||`, `&&`); `isPathDangerous` -- directory traversal and sensitive path detection with whitelist |
| `migrate_legacy.go` | `migrateGoClaudeIfNeeded` -- one-time migration of memory files from legacy `~/.go-claude/` to `~/.iroha/` (global + project), writes `~/.iroha/.migrated` sentinel |
| `runner_test_helper.go` | `testLLMModel` (no-op LLM for tests) and `NewTestRunner` (creates minimal `CustomRunner` without network calls) |

## For AI Agents

### Working In This Directory
- Global singletons: `GlobalPermissionManager`, `GlobalHookManager`, `GlobalMemoryManager`, `GlobalTodoManager`, `GlobalTaskManager`, `GlobalBackgroundManager`, `GlobalCronScheduler`, `GlobalTeamManager`, `GlobalProtocolManager`, `GlobalAutonomyManager`, `GlobalWorktreeManager`, `GlobalMCPRouter`, `GlobalToolCircuitBreaker`, `GlobalAgentPool`, `GlobalPluginManager`, `GlobalSkillManager`, `GlobalSubagentManager`, `GlobalSandboxEnabled`
- `ConfirmationBridge` is the async channel between runner (goroutine) and TUI (main thread): `PromptChan`/`ResponseChan`/`CancelChan`
- `ToolStatusBridge` provides real-time tool status to TUI via `StatusChan` with background drain worker
- `blockingConfirmationTool` wraps every tool to intercept and confirm before execution
- `SystemPromptBuilder` assembles the system instruction with a caching boundary
- `ToolCircuitBreaker` halts after 3 consecutive identical-arg failures on the same tool
- `DreamConsolidator` runs automated memory deduplication through a 7-gate validation system
- `IPCBridge` enables process-isolated teammates via Unix domain sockets
- `Watchdog` manages child process teammates with crash budget, checkpoint/recovery, and dead-letter queue

### Testing Requirements
- `go test ./pkg/agent/...`
- Tests exist for: hooks, hooks_types, memory, permission, todo_manager, autonomous, background, cron, mcp, protocol, task, team, worktree, prompt, auto_review, compaction, diff, ci_watcher, logger, session_store, runner, git_helper, lsp, pool, error_recovery, plugin, sandbox, skills, subagent, tokenizer, tools_file, tools_web
- **Gap**: `tools.go` has no dedicated test file (tool registration is integration-tested via runner tests)

### Common Patterns
- Mutex-protected global singletons (`sync.RWMutex`)
- Tool handlers follow `func(ctx tool.Context, args T) (R, error)` signature via `functiontool.New()`
- Hook config uses two-layer merge (global `~/.iroha/` + project `.iroha/`)
- Memory files use YAML frontmatter with auto-generated `MEMORY.md` index
- DAG edge reconciliation is bidirectional with auto-unblocking cascade
- MCP tools are dynamically discovered and wrapped as `DynamicMCPTool` implementing `tool.Tool`
- Plugin manifests (`plugin.json`) and skill manifests (`skill.json`) follow global-then-project overlay, project overrides global by ID
- Hook execution supports three types: shell command (exit-code protocol), HTTP POST (JSON decision), LLM prompt (JSON audit)
- Subagents use worktree isolation for executor types and read-only CWD for explore/planner/reviewer/researcher types
- Teammates support two modes: in-process goroutine (default) or child process with IPC bridge + watchdog
- Sandbox wrapping is OS-aware: macOS uses `sandbox-exec` with deny-write profile, Linux uses `bwrap` with read-only root
- Web tools use SSRF-safe HTTP transport that validates resolved IPs at connection time
- File edit batch uses two-phase commit: validate all -> snapshot -> apply all -> rollback on failure
- Config path: `~/.iroha/` (auto-migrates from legacy `~/.go-claude/`)

## Dependencies

### Internal
- `pkg/llm` -- Model adapter (`llm.NagReminderTrigger`, `llm.NoteRoundWithoutUpdate`, `llm.SystemPromptTrigger` callbacks)
- `pkg/config` -- Configuration loading (`config.LoadConfig` for LSP servers, SearXNG URL)

### External
- `google.golang.org/adk/agent` -- Agent framework
- `google.golang.org/adk/agent/llmagent` -- LLM agent builder
- `google.golang.org/adk/tool` / `functiontool` -- Tool system
- `google.golang.org/adk/runner` -- Agent runner
- `google.golang.org/adk/session` -- Session management
- `google.golang.org/genai` -- Generative AI types
- `github.com/google/uuid` -- Unique ID generation (background tasks, cron jobs)
- `golang.org/x/net/html` -- HTML parsing for web fetch/search

<!-- MANUAL: -->
