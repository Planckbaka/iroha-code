# Audit: A2-tools

## Files audited

- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_file.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_file_batch.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_file_search.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_shell.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_web.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_web_safety.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_mcp.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_memory.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_schedule.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_subagent.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_task.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_team.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_todo.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tools_worktree.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/auto_review.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/lsp_tools.go
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/lsp_utils.go (registerLSPTools)
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/ci_watcher.go (registerCITools)
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/mcp.go (DynamicMCPTool)
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/runner_exec.go (dispatch)
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/runner_edit.go (snapshot/rollback)
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/pool.go (WorkdirKey)
- /Users/akiwayne/Documents/Project2026/go-project/go-claude/pkg/agent/tokenizer.go (safePrefixes)
- /Users/akiwayne/go/pkg/mod/google.golang.org/adk@v1.2.1-0.20260519122726-f2aee5301649/tool/tool.go (tool.Tool/tool.Context)
- /Users/akiwayne/go/pkg/mod/google.golang.org/adk@v1.2.1-0.20260519122726-f2aee5301649/tool/functiontool/function.go (Func/New)

## Capabilities
- **[implemented] Tool registration framework (table-driven, generic)** — ToolRegistry + generic register[TArgs,TResults]() in tools.go:24 wraps functiontool.New(Config{Name,Description}, handler). 40 tools registered across 14 register* funcs in GetSWETools() (tools.go:359). Table-driven, append-only, first-error-wins. Real, works.
- **[implemented] file_read** — tools_file.go:25-71. 10MB cap, rejects dirs, supports 1-based start/end line slicing with 'N\t<line>' formatting (mimics Read tool cat -n). Sandbox-validated (validateSandboxPath). Matches Claude Code Read semantics closely.
- **[implemented] file_edit (exact + whitespace-tolerant)** — tools_file.go:88-159. Exact-match first, then whitespace-tolerant line-based fallback (normalizeLine collapses runs). Enforces uniqueness unless replace_all. Generates unified diff. Dry-run support. snapshotFile() for rollback. No 'Read before edit' hard requirement like real CC.
- **[implemented] file_edit_batch (atomic multi-edit)** — tools_file_batch.go:22-123. Two-phase (validate-all then apply-all) with rollbackPendingEdits() on any failure. Max 50 edits. Reuses whitespaceTolerantEdit fallback. Diff per edit.
- **[implemented] file_write** — tools_file.go:391-410. MkdirAll parents, snapshot+overwrite. No diff display, no line-numbering. Diverges from CC Write (which enforces Read-before-overwrite).
- **[implemented] shell_run (streaming, sandboxed)** — tools_shell.go:43-136. exec.CommandContext via 'sh -c', WrapSandboxCommand applied, StdoutPipe+StderrPipe merged, line streaming via ToolBridge.Send(ToolStatus{StreamLines}), 500-line stream cap, 30s timeout. Exit code reported. checkShellCommandSandbox enforces cwd containment.
- **[implemented] Shell command sandbox (path/static analysis)** — tools.go:151-202 + tokenizeCommand/splitShellPipeline/tokenizeAllowedReadOnlyPipeline. Blocks relative '../' escape, out-of-cwd absolute paths (except safePrefixes from tokenizer.go), env-var expansion ($VAR/${VAR}). Allows find|grep|git|ls|rg ... | head readonly pipelines. Real but heuristic-only (tokenized, not a real shell parser).
- **[implemented] background_run / check_background** — tools_shell.go:147-179. Delegates to GlobalBackgroundManager.RunContext/Check. checkShellCommandSandbox applied. Emits task_id; results drained via drain_notifications.
- **[implemented] web_fetch** — tools_web.go:31-114. SSRF guard (checkSSRF + ssrfSafeTransport DNS-rebinding-safe DialContext, privateNets incl. fc00::/7), 5MB cap, htmlToText conversion, rate-limit 10/min. http/https only.
- **[partial] web_search (DuckDuckGo scrape / SearXNG)** — tools_web.go:135-330. HTML scraping of html.duckduckgo.com (parseDDGResults/extractDDGResult decoding uddg redirect) OR SearXNG JSON backend from config.WebSearchSearXNGURL. 10/min rate limit. No real search-API integration (CC uses hosted search).
- **[implemented] search_grep** — tools_file_search.go:104-152. regexp.Compile, filepath.Walk, skips grepExcludedDirs (.git/node_modules/etc), 1MB file cap, 50 match cap. NOT ripgrep-backed (pure Go walk). No -i/-g/file filters like CC Grep.
- **[implemented] find_files (glob)** — tools_file_search.go:165-255. Custom matchGlob with ** support (recursive), 100-file cap, skips excluded dirs. Bubble-sort (O(n^2)) — diverges from CC Glob.
- **[implemented] list_directory** — tools_file_search.go:24-85. filepath.Walk, depth cap 4, grepExcludedDirs skip, 200-entry cap. dirs get '/' suffix.
- **[implemented] memory_save/list/search/update/delete/dream** — tools_memory.go. CRUD over GlobalMemoryManager + memory_dream (4-phase DreamConsolidator). Persisted to disk. Roughly maps to CC memory/save_search semantics but types (user/feedback/project/reference) differ.
- **[implemented] task_create/update/list/get + todo** — tools_task.go + tools_todo.go over GlobalTaskManager (DAG with DFS cycle validation) and GlobalTodoManager. Mirrors CC TaskCreate/TaskUpdate/TaskList/TaskGet + TodoWrite (single in_progress rule encoded in description only).
- **[implemented] schedule_create/list/delete** — tools_schedule.go over GlobalCronScheduler. One-shot/recurring + durable persistence. Real local cron. Maps loosely to CC scheduled-task MCP, not native.
- **[implemented] spawn_teammate + team comms + protocol + autonomy** — tools_team.go. Spawn/list/message/inbox/broadcast + protocol_shutdown/plan_approval request/response + agent_claim_task/agent_set_state. Over GlobalTeamManager/GlobalProtocolManager/GlobalAutonomyManager. Parallel to CC TeamCreate/TaskUpdate/SendMessage but bespoke protocol set.
- **[partial] spawn_subagent** — tools_subagent.go:8-19. Thin wrapper calling GlobalSubagentManager.RunSubagent(ctx, args). Synchronous. No parallel/non-blocking option (CC Task supports background).
- **[implemented] worktree_create/list/status/enter/closeout** — tools_worktree.go over GlobalWorktreeManager (Create/List/Status/Enter/Closeout with keep|remove). Real git worktree-backed isolation.
- **[implemented] MCP plugin discovery + dynamic tool registration** — tools_mcp.go + mcp.go. GlobalMCPRouter.LoadAndStartPlugins + DiscoverTools returns []tool.Tool. DynamicMCPTool implements tool.Tool + Declaration()/ProcessRequest injecting genai.FunctionDeclaration with ParametersJsonSchema. Real MCP-protocol client integration.
- **[implemented] LSP tools (5)** — lsp_utils.go:105 + lsp_tools.go. LSPGotoDefinition/FindReferences/DocumentSymbols/Hover/Diagnostics via getLSPClient per-language (Go/TS/Python/Rust from config). json.RawMessage fallback parsing. Uses textDocument/diagnostic (pull, 3.17+). Rough analog of CC LSP MCP server but native.
- **[implemented] CI watcher** — ci_watcher.go:91. agent_watch_ci starts background GitHub Actions monitor -> inbox notifications on failure.
- **[implemented] Auto-review (4-tier risk + LLM judge)** — auto_review.go. RiskTier enum + ClassifyTool/classifyShellCommand (trusted/low/medium/high) and ReviewCommand/ReviewFileOperation with LLM fallback. SetAutoReviewConfig(model.LLM). Dangerous-pattern hard-filter re-checks LLM approval. callLLMForReview via llm.CollectNonStreaming. Heuristic-only fallback when no model.
- **[implemented] Edit snapshot/rollback** — runner_edit.go snapshotFile/rollbackPendingEdits + per-run commitEditedFiles. On tool failure or ctx cancel, restores originals. CC has no equivalent (uses git).
- **[implemented] Tool pool hot-reload** — tools.go:401-451. RebuildToolPool (re-discover, bump version) + CheckPluginsFileChanged (mtime of .iroha/plugins.json). Enables /mcp reload.
- **[missing] Notebook tools (NotebookEdit)** — Not in registry. CC has NotebookEdit. Absent.
- **[missing] Grep tool flag parity (output_mode/-i/-g/context)** — Grep has no -i/--include/--exclude/-A/-B/-C flags; no JSON/structured output; 50-line cap. CC Grep is ripgrep-backed with rich flags.
- **[missing] Task (background agent) tool** — CC Task supports run_in_background / TaskStop / non-blocking spawn. spawn_subagent here is strictly synchronous via RunSubagent.
- **[missing] Large output auto-compression / headroom** — web_fetch truncates at 5MB and htmlToText is naive (no readability/JS rendering). No URL-context extraction.
- **[missing] Tool description schema validation** — register functions set description strings but there is no CC-style 'dict' arg schema with required fields. functiontool derives schema from json tags; no explicit required/enum validation at registration.

## External deps
- google.golang.org/adk v1.2.1-... — tool.Tool, tool.Context, tool/functiontool (registration+schema reflection). Load-bearing across every tools_*.go.
- google.golang.org/genai v1.57.0 — genai.FunctionDeclaration/Tool/Content/Part/GenerateContentConfig used by DynamicMCPTool (mcp.go), runner_exec.go message building, and indirectly functiontool. NOT ADK but is the wire schema.
- github.com/firebase/genkit/go v1.8.0 — used ONLY in pkg/llm/adapter.go to build model.LLM; reaches A2 solely via SetAutoReviewConfig(model.LLM) consumed by auto_review.go.
- google.golang.org/adk/model — model.LLM + model.LLMRequest used by auto_review.go for the LLM safety judge.
- google.golang.org/adk/agent + adk/session — referenced by tool.Context (CallbackContext, EventActions) and by the runner (adkRunner.Run). Tools do not import these directly except in tests (tools_shell_test.go imports adk/agent, adk/memory, adk/session, adk/tool/toolconfirmation, genai).
- golang.org/x/net/html — HTML parsing for web_fetch/web_search (tools_web.go, tools_web_safety.go).
- iroha/pkg/config — WebSearchSearXNGURL + LSPServers config (tools_web.go:150, lsp_utils.go:108).
- iroha/pkg/llm — CollectNonStreaming helper used by auto_review.go (auto_review.go:298,443).

## Coupling notes

This area is HEAVILY coupled to google.golang.org/adk and is the single hardest decoupling point for a native rewrite. Concrete load-bearing dependencies:

1. tool.Tool interface (adk/tool/tool.go:42) — every registered tool must implement Name()/Description()/IsLongRunning(). GetSWETools returns []tool.Tool. A native replacement needs an equivalent interface (Name/Description/IsLongRunning/Declaration/Run).

2. tool.Context (adk/tool/tool.go:55) — NOT a context.Context alias. It embeds agent.CallbackContext and exposes FunctionCallID()/Actions()/*session.EventActions/SearchMemory() (returns *memory.SearchResponse)/ToolConfirmation()/*toolconfirmation.ToolConfirmation/RequestConfirmation(hint,payload). CRITICAL: iroha's handlers declare `ctx tool.Context` but ONLY use it as a bare context.Context via ctx.Value(WorkdirKey) (tools.go:70, pool.go:25). The rich ADK Context surface (confirmation, actions, memory search) is UNUSED by the handlers — confirmation is instead implemented ad-hoc via runner_confirmation*.go + ToolBridge + ReviewCommand. This means the handlers are 'decoupling-ready': replacing `tool.Context` with a plain `context.Context` (or a tiny native ToolCtx{context.Context; Workdir string}) requires changing only the handler signatures, not their bodies.

3. functiontool.New + functiontool.Func[TArgs,TResults] (adk/tool/functiontool/function.go:71,78) — the generic register[TArgs,TResults] in tools.go:24 depends on functiontool.New(Config{Name,Description}, handler). This auto-derives the JSON schema from struct field tags (`json:\"x\" description:\"...\"`) and auto-marshals args/results to map[string]any. A native rewrite must replicate this schema-from-struct-tags reflection (iroha already relies on the `description:` struct tag everywhere — e.g. tools_file.go FileReadArgs). This is the largest mechanical port: write a generic `register[TArgs,TResults]` that reflect-walks TArgs to produce a genai.FunctionDeclaration-style schema and a JSON-(un)marshal dispatcher.

4. genai.FunctionDeclaration / genai.Tool / genai.Part / genai.Content (google.golang.org/genai v1.57.0) — used by DynamicMCPTool.Declaration/ProcessRequest (mcp.go:267-283), by runner_exec.go building *genai.Content user messages, and indirectly by functiontool. NOTE: genai is the Google GenAI SDK, not ADK itself — it is the wire format for tool declarations and messages. Decoupling from ADK does NOT remove the genai dependency unless the native loop also replaces genai with Anthropic-native message/tool-use types.

5. model.LLM + model.LLMRequest (adk/model) + agent.Runner/agent.RunConfig/agent.StreamingModeSSE (adk/agent) — auto_review.go uses model.LLM/model.LLMRequest/llm.CollectNonStreaming (auto_review.go:12,166-168,278-298) and the runner dispatches via cr.adkRunner.Run(...) (runner_exec.go:139). Tool execution itself does NOT call model.LLM, but the auto-review subsystem does, and tools are ultimately driven by the ADK runner's event stream. Decoupling tools from ADK therefore also requires replacing the runner (A1/A3 area).

6. Indirect via Genkit: tools themselves do NOT import firebase/genkit. The only Genkit coupling is in pkg/llm/adapter.go (NewAdapter(*genkit.Genkit,...)) which produces the model.LLM that SetAutoReviewConfig consumes. So Genkit reaches A2 only through the LLM handle handed to auto-review — replacing the LLM adapter removes it.

NATIVE REPLACEMENT REQUIREMENTS (what a CC-style no-framework port needs):
- A native `Tool` interface: { Name, Description, IsLongRunning, Declaration()*Schema, Run(ctx, args any)(map[string]any,error) }.
- A native `ToolCtx` carrying workdir + function_call_id + a confirmation channel (replacing tool.Context's RequestConfirmation/ToolConfirmation), OR keep confirmation outside tools entirely (iroha already does this via ReviewCommand in runner_confirmation — the cleaner path).
- A generic schema-from-struct-tags reflector to replace functiontool.New (iroha's struct tags already encode everything needed).
- Replace genai.FunctionDeclaration with an Anthropic-tool-use schema type (or keep a thin genai-compatible shim if the wire layer stays genai).
- auto_review.go must call the native LLM client, not model.LLM/llm.CollectNonStreaming.

BOTTOM LINE: The tool HANDLERS are ~90% decoupling-ready (they only need context.Context + WorkdirKey). The coupling is concentrated in (a) the registration/reflection layer (functiontool) and (b) the types tool.Tool/tool.Context/genai.FunctionDeclaration/model.LLM. A native port is feasible and mostly mechanical for handlers, but requires building a small schema-reflection + Tool-interface + dispatch layer to replace functiontool + tool.Tool.

## Divergences from Claude Code
- file_write has NO Read-before-overwrite enforcement — real CC refuses to overwrite a file you haven't Read in this session; iroha just overwrites (tools_file.go:391).
- file_edit does NOT require a prior file_read; CC's Edit requires the file to have been Read first. iroha allows blind edits (tools_file.go:88).
- search_grep is a pure-Go filepath.Walk regex matcher, NOT ripgrep. No -i/--include/--exclude/-A/-B/-C/output_mode flags, hard 50-match cap, 1MB-per-file skip. Semantics and ergonomics differ materially from CC Grep (tools_file_search.go:104).
- find_files uses an O(n^2) bubble sort and a hand-rolled ** glob matcher, not doublestar/fsnotify; 100-result cap (tools_file_search.go:247).
- web_search scrapes DuckDuckGo HTML or hits a self-hosted SearXNG; CC uses a hosted search backend with structured results. Rate-limited to 10/min (tools_web.go:135).
- web_fetch truncates at 5MB and uses a naive htmlToText (no readability extraction, no JS rendering); CC WebFetch has richer extraction + URL-context modes.
- shell_run always uses 'sh -c' with a 30s timeout and 500-line stream cap; CC Bash supports configurable timeout up to 600000ms, run_in_background, and richer sandboxing (iroha's sandbox is static token analysis, not a true seccomp/seatbelt sandbox).
- spawn_subagent is SYNCHRONOUS only (RunSubagent blocks). CC Task supports background dispatch + TaskStop + multiple agents (tools_subagent.go:8).
- todo enforces 'exactly one in_progress' only via description text, not structurally; CC TodoWrite enforces it at the tool layer.
- snapshotFile/rollbackPendingEdits (runner_edit.go) provide a per-run undo that CC does NOT have — CC relies on git. This is an iroha-specific divergence.
- Confirmation model differs: iroha uses ReviewCommand (heuristic+LLM) + 4-tier RiskTier + ToolBridge status bridge, whereas real CC uses permission rules in settings.json + explicit per-tool allow/deny + can_use_tool hooks. ADK's native tool.Context.RequestConfirmation/ToolConfirmation is NOT used by the handlers.
- Auto-review LLM judge (callLLMForReview) re-checks LLM 'safe' verdicts against hardcoded dangerous-pattern lists to resist prompt injection — CC has no equivalent LLM-judge layer (it uses deterministic rules + hooks).
- LSP tools are first-class native tools (lsp_*) rather than an MCP server as in CC; pull-diagnostics-only (LSP 3.17+), no workspace diagnostics fallback.
- mcp_server_list is the only MCP-meta tool; CC exposes richer MCP resource/prompt tooling. Dynamic MCP tool discovery IS implemented (mcp.go DiscoverTools) but plugin lifecycle is bespoke (.iroha/plugins.json), not the standard MCP config.
- All struct-tag-based arg schemas have no 'required' field tracking (CC uses explicit required arrays in JSON schema).

## Quality notes

The tool layer is broad (40 tools) and mostly functionally complete, with genuinely thoughtful security work: SSRF protection includes DNS-rebinding-safe DialContext (tools_web_safety.go:117), symlink-resolving sandbox (validatePathForSandbox, tools.go:124), env-var-expansion blocking, and an LLM-judge with anti-injection re-checking (auto_review.go:229-272). However several rough edges: (1) sortFiles is O(n^2) bubble sort (tools_file_search.go:247); (2) shell sandbox is static tokenization, not a real sandbox (no seatbelt/seccomp) — WrapSandboxCommand exists but its strength wasn't verified here; (3) findLineMatches caps at 100 matches silently (tools_file.go:223); (4) GrepHandler ignores binary files only by size (1MB), not by content sniff — will feed binaries through regexp; (5) web_search DuckDuckGo scraping is brittle to DDG HTML changes; (6) snapshotFile reads the file again even though FileEditHandler already read it (double read); (7) no per-tool 'required args' validation — relies entirely on LLM correctness; (8) memory_dream and schedule durable persistence are real but their storage formats weren't audited here (in memory.go / schedule.go, A2-adjacent). Test coverage is strong for handlers (tools_*_test.go present for most). The codebase is internally consistent but the divergence from CC's exact tool semantics (Read-before-edit, Grep flags, Task backgrounding, NotebookEdit) is the main parity gap, not capability gaps per se.
