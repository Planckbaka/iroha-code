# Research: tools-canonical

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code (as of v2.1.x, mid-2026) exposes a fixed canonical set of built-in tools to the model. The core file/exec/agent tools are Read, Write, Edit, Glob, Grep, Bash, NotebookEdit, Task (a.k.a. Agent), TodoWrite, WebFetch, WebSearch, AskUserQuestion, ExitPlanMode, Skill. The official docs table now lists ~50 tools including newer ones: TaskCreate/TaskGet/TaskList/TaskUpdate (which REPLACE TodoWrite as of v2.1.142), NotebookEdit, LSP, Monitor, PowerShell, EnterPlanMode/ExitPlanMode, EnterWorktree/ExitWorktree, CronCreate/CronList/CronDelete, ScheduleWakeup, SendMessage, TeamCreate/TeamDelete, Workflow, ShareOnboardingGuide, RemoteTrigger, PushNotification, ListMcpResourcesTool/ReadMcpResourceTool, WaitForMcpServers, ToolSearch, plus deprecated BashOutput/KillShell/TaskOutput. CRITICAL VERSION FACT: MultiEdit was REMOVED in Claude Code v2.0 (it existed in v1.x for batch atomic edits in a single file) and is NOT in the current tool set; the model achieves the same via multiple parallel Edit calls. TodoWrite is DISABLED BY DEFAULT as of v2.1.142 in favor of the Task* quartet (re-enable via CLAUDE_CODE_ENABLE_TASKS=0). Each tool has a strict JSON-schema parameter contract; file tools require absolute paths and enforce a read-before-edit/read-before-write session state check; permission rules use the exact tool name as the matcher string.

## Components
### Read
**Purpose:** Read file contents with line numbers; multimodal (text, images, PDFs, .ipynb).

**Mechanism:** Returns file contents with 1-indexed line numbers in `cat -n` format. Line-number prefix format: `spaces + line_number + tab + content`. Default reads first 2000 lines from the start; each line truncated at 2000 chars. If a whole-file read exceeds token limit, returns first page + a `PARTIAL view` notice telling the model how to read more with offset/limit. A read that explicitly passes offset/limit and STILL exceeds the limit returns an error. Multimodal: images (PNG/JPG) returned as visual content (resized/recompressed to model limits); PDFs read whole if <=10 pages, else paged via `pages` param like "1-5" up to 20 pages; .ipynb returns all cells with outputs. Reads files only, NOT directories (use Bash `ls`). Absolute paths enforced.

**Data model:** Params: {file_path: string (required), offset?: number, limit?: number}. additionalProperties:false. Result: tool_result with text content. For >10-page PDFs the `pages` param is required.

**Config:** Required: file_path. Optional: offset (1-indexed line number to start), limit (line count, default 2000). No path = error.

### Write
**Purpose:** Create new file or fully overwrite existing file.

**Mechanism:** Creates a new file or fully overwrites an existing one. Does NOT append or merge — atomically writes the complete content. Enforces READ-BEFORE-WRITE: if target exists, the model must have read it in the current conversation at least once or the call FAILS with an error. New files are exempt. Same Bash-read satisfaction rules as Edit (cat/head/tail/sed -n X,Yp/grep/egrep/fgrep on a single file, no pipes). For partial changes, the model is instructed to use Edit instead. Absolute paths only.

**Data model:** Params: {file_path: string (required), content: string (required)}. additionalProperties:false.

**Config:** Required: file_path, content. No optional fields.

### Edit
**Purpose:** Precise surgical string replacement in a file via exact matching.

**Mechanism:** EXACT string replacement — no regex, no fuzzy matching. Three checks run in order: (1) READ-BEFORE-EDIT (must have read file this conversation AND file unchanged on disk since) — runs FIRST before matching; (2) MATCH (old_string must appear exactly, including indentation/whitespace); (3) UNIQUENESS — old_string must appear EXACTLY ONCE, otherwise the edit fails; to disambiguate, supply more surrounding context, or set replace_all:true to replace all occurrences. Absolute paths. Read-before-edit is ALSO satisfied when Bash ran cat/head/tail/sed -n 'X,Yp'/grep/egrep/fgrep on a SINGLE file with no pipes/redirects — piped output and other commands do NOT count. NOTE: read-before-edit satisfaction set != deny-rule-checked set (egrep/fgrep count for read-before-edit but not Read deny rules).

**Data model:** Params: {file_path, old_string, new_string (all required); replace_all?: boolean (default false)}. additionalProperties:false.

**Config:** Required: file_path, old_string, new_string. Optional: replace_all (default false). new_string MUST differ from old_string.

### Glob
**Purpose:** Fast file-by-name pattern matching.

**Mechanism:** Finds files by NAME pattern using standard glob syntax: `*` (single dir level), `**` (recursive), `?`, `{a,b}` alternation, `[abc]`/`[a-z]`/`[!abc]`. Examples: `**/*.js`, `src/**/*.ts`, `*.{json,yaml}`. Results sorted by modification time (most recent first), capped at 100 files; hitting the cap returns a truncation flag so the model can narrow. Does NOT respect .gitignore by default (finds gitignored files) — DIFFERS from Grep which does respect .gitignore. Set CLAUDE_CODE_GLOB_NO_IGNORE=false to make it respect .gitignore.

**Data model:** Params: {pattern: string (required), path?: string}. additionalProperties:false. Result: list of file paths + truncation flag.

**Config:** CLAUDE_CODE_GLOB_NO_IGNORE=false makes Glob respect .gitignore (default ignores the ignore file).

### Grep
**Purpose:** Search file contents using ripgrep regex.

**Mechanism:** Searches file CONTENTS. Built on ripgrep (uses ripgrep regex, NOT POSIX grep — literal braces need escaping: `interface\{\}` to find Go `interface{}`). Three output modes: files_with_matches (paths only, DEFAULT), content (matching lines + file + line number, supports -A/-B/-C context and -n), count (per-file match count). Scope by `glob` (e.g. `**/*.tsx`) or `type` (e.g. `py`, `rust`). Default single-line match; multiline:true spans lines (rg -U --multiline-dotall). head_limit caps first N entries across all modes. Respects .gitignore (skips gitignored files); to search a gitignored file pass its path directly. The literal JSON keys `-i`, `-n`, `-A`, `-B`, `-C`, `multiline`, `head_limit` mirror rg flags.

**Data model:** Params: {pattern (required), path?, output_mode?: 'content'|'files_with_matches'|'count' (default files_with_matches), glob?, type?, '-i'?, '-n'?, '-A'?, '-B'?, '-C'?, multiline?: boolean (default false), head_limit?: number}. additionalProperties:false. Note the literal flag names -i/-n/-A/-B/-C as JSON keys.

**Config:** output_mode default files_with_matches. -A/-B/-C/-n only honored with output_mode=content. multiline default false. head_limit works in all modes.

### NotebookEdit
**Purpose:** Modify Jupyter notebook cells by cell_id.

**Mechanism:** Edits ONE cell at a time, targeted by `cell_id` (NOT string replacement across the notebook like Edit). Modes: replace (overwrite cell source, DEFAULT), insert (add new cell AFTER target; with no cell_id goes at the START; requires cell_type=code|markdown), delete (remove target cell). notebook_path must be ABSOLUTE. Permission rules use the Edit(...) path format — e.g. `Edit(notebooks/**)` covers NotebookEdit in that dir.

**Data model:** Params: {notebook_path (required, absolute), new_source (required), cell_id?, cell_type?: 'code'|'markdown', edit_mode?: 'replace'|'insert'|'delete' (default replace)}. additionalProperties:false.

**Config:** Required: notebook_path, new_source. Optional: cell_id, cell_type (required for insert), edit_mode (default replace).

### Bash
**Purpose:** Execute shell commands; general-purpose escape hatch.

**Mechanism:** Runs each command in a SEPARATE process (not one persistent shell) but emulates persistence: `cd` carries to later commands ONLY if it stays in the project dir or an added working dir (else resets to project dir + appends `Shell cwd was reset to <dir>`). Env vars do NOT persist across commands (export in one is gone in the next). Aliases/functions/options DO persist — at session start Claude Code sources ~/.zshrc/~/.bashrc/~/.profile, captures aliases/functions/options, applies to every command. Subagent sessions never carry cwd changes. Limits: default timeout 120000ms (2 min), model can request up to 600000ms (10 min) via timeout param; output truncated at 30000 chars by default — when exceeded, full output saved to a file in the session dir and the model gets the file path + short preview (raise via BASH_MAX_OUTPUT_LENGTH up to hard 150000). run_in_background:true detaches; never use it for `sleep` (returns immediately). Model is told to avoid Bash for cat/head/tail/grep/find/sed/awk/echo and to prefer Read/Grep/Glob; independent commands go as parallel Bash calls, dependent ones chained with && (not newlines). Background task output files have no size limit and are not auto-cleaned. Git safety: never update git config, never destructive git ops unless explicit, never skip hooks, never force-push main/master.

**Data model:** Params: {command: string (required), description?: string, timeout?: number (max 600000), run_in_background?: boolean (default false)}. additionalProperties:false. Result text includes stdout, stderr, and `Exit code N`.

**Config:** timeout default 120000 (BASH_DEFAULT_TIMEOUT_MS overrides default, BASH_MAX_TIMEOUT_MS overrides ceiling). Output cap 30000 (BASH_MAX_OUTPUT_LENGTH raises it, hard ceiling 150000). CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR=1 disables cwd carry-over. CLAUDE_ENV_FILE for env var persistence. Sources ~/.zshrc/~/.bashrc/~/.profile.

### Skill
**Purpose:** Execute a skill within the main conversation.

**Mechanism:** Loads a skill by name. Skill names without leading slash. Plugin-namespaced skills use `plugin:skill` form. When invoked, shows `{name} skill is loading` then expands the skill prompt. Only skills in the available list may be invoked; cannot invoke a skill already running; not for built-in CLI commands (/help, /clear). Runs through the existing Skill tool rather than adding a new tool entry. Note: the separate SlashCommand tool handles user-authored `/commands`.

**Data model:** Params: {command: string (required) — skill name only, no args}. additionalProperties:false.

**Config:** Required: command. No args passed (args go in the skill itself).

### ExitPlanMode
**Purpose:** Present a plan for approval and exit plan mode.

**Mechanism:** Called only while in plan mode, after the model has presented its plan and is ready to code. Presents the plan to the user for approval and exits plan mode. ONLY for implementation/code-writing tasks — explicitly NOT for research/exploration. If ambiguous, the model is told to resolve via AskUserQuestion first. Permission: Yes (entering/exiting plan mode is gated).

**Data model:** Params: {plan: string (required, supports markdown)}. additionalProperties:false.

**Config:** Required: plan. Use only for implementation tasks, not research.

### AskUserQuestion
**Purpose:** Ask multiple-choice clarifying questions.

**Mechanism:** Structured multiple-choice prompt. 1-4 questions per call, 2-4 options per question, header is a very short label (max 12 chars), each option has label (1-5 words) + description. Users can always select 'Other' for custom text (auto-added — model must NOT include an 'Other' option). multiSelect must be specified. Used for gathering preferences, clarifying ambiguity, deciding implementation direction.

**Data model:** Params: {questions: array (minItems 1, maxItems 4) of {question, header (max 12 chars), multiSelect: boolean (required), options: array (minItems 2, maxItems 4) of {label, description}}; answers?: object (populated by permission component)}. additionalProperties:false.

**Config:** 1-4 questions; 2-4 options each; header max 12 chars; label 1-5 words; multiSelect required field.

### WebSearch
**Purpose:** Server-side web search returning titles+URLs.

**Mechanism:** Runs query against Anthropic's server-side web search backend, returns result TITLES and URLs only (does NOT fetch pages — follow up with WebFetch). May issue up to EIGHT backend searches per call, refining internally before returning. Scope with allowed_domains (include only) or blocked_domains (exclude) — the two lists CANNOT be combined in one call. Backend not configurable (use MCP for other providers). Permission rules take NO specifier — bare `WebSearch` in allow/deny only. US-only. Availability varies by provider (works on Claude API + MS Foundry; on Vertex AI with Claude 4 models; NOT on Bedrock).

**Data model:** Params: {query: string (required, minLength 2), allowed_domains?: string[], blocked_domains?: string[]}. additionalProperties:false.

**Config:** Required: query (min 2 chars). allowed_domains XOR blocked_domains (not both). No specifier in permission rules.

### WebFetch
**Purpose:** Fetch a URL, convert to Markdown, extract per prompt via small model.

**Mechanism:** Fetches URL, converts HTML to Markdown (not configurable), runs the prompt against content using a SMALL FAST model, returns that model's answer (NOT raw page) — lossy by design. HTTP auto-upgraded to HTTPS. Large pages truncated to a fixed char limit before processing. 15-minute self-cleaning cache. On cross-host redirect, returns a text result naming original + redirect target (does NOT follow); model issues a second WebFetch. User-Agent begins with `Claude-User`; Accept header prefers Markdown over HTML. In default/acceptEdits modes, prompts on first reach of a new domain EXCEPT a built-in preapproved docs-domain set; add `WebFetch(domain:example.com)` to pre-allow. An explicit WebFetch(domain:...) in deny/ask/allow OVERRIDES the preapproved set. auto/bypassPermissions modes skip the prompt.

**Data model:** Params: {url: string (required, format: uri), prompt: string (required)}. additionalProperties:false.

**Config:** Required: url, prompt. 15-min cache. HTTP auto->HTTPS. User-Agent: Claude-User*.

### Task (a.k.a. Agent)
**Purpose:** Spawn a subagent with its own context to handle a task autonomously.

**Mechanism:** Spawns a subagent in a SEPARATE context window that works autonomously and returns ONE final text result; parent never sees intermediate tool calls/outputs. Named types: general-purpose (all tools), Explore (Glob/Grep/Read/Bash, with thoroughness quick|medium|very thorough), plus setup agents. `tools`/`disallowedTools` frontmatter on the subagent definition controls tool set: neither=inherit all; tools only=just those; disallowedTools only=all except those; both set=disallowedTools wins. Foreground subagents show live permission prompts; background subagents auto-deny any prompting call and continue. Launching itself needs no permission. maxTurns caps turn count. Fork mode: a fork inherits the full parent conversation, always runs in background, surfaces prompts in terminal. Note: docs table lists the tool as `Agent`; older schema/system-prompt name is `Task` — same tool. deprecated TaskOutput is replaced by Read on the task's output file path.

**Data model:** Params: {description: string (3-5 words, required in older schema), prompt: string (required), subagent_type: string (required), model?: 'haiku'|'sonnet'|'opus', resume?: string (agent id)}. additionalProperties:false.

**Config:** Required: prompt. Optional: description, subagent_type, model, resume.

### TodoWrite (LEGACY / disabled by default)
**Purpose:** Manage the session checklist (whole-list replace).

**Mechanism:** Replaces the ENTIRE todo list each call (not incremental). Exactly ONE item should be in_progress at a time. Item shape: {content: imperative-form string, status: 'pending'|'in_progress'|'completed', activeForm: present-continuous string}. Use for 3+ step complex tasks; skip for trivial/conversational. VERSION CHANGE: TodoWrite is DISABLED BY DEFAULT as of v2.1.142 in favor of the granular TaskCreate/TaskGet/TaskList/TaskUpdate quartet. To re-enable the legacy TodoWrite tool, set CLAUDE_CODE_ENABLE_TASKS=0. (Note: the Tasks feature itself was gated behind CLAUDE_CODE_ENABLE_TASKS=1 during its earlier opt-in rollout.) A 2026 system-prompt change swaps the hardcoded TodoWrite reference for one that resolves to TaskCreate or TodoWrite depending on whether tasks are enabled.

**Data model:** TodoWrite params: {todos: array of {content (minLength 1), status: 'pending'|'in_progress'|'completed', activeForm (minLength 1)}}. additionalProperties:false on items.

**Config:** Disabled by default since v2.1.142. Set CLAUDE_CODE_ENABLE_TASKS=0 to re-enable TodoWrite.

### TaskCreate / TaskGet / TaskList / TaskUpdate
**Purpose:** Granular ID-based task management (replaces TodoWrite).

**Mechanism:** The modern replacement (introduced ~v2.1.16, became default in v2.1.142). Granular CRUD: TaskCreate (new pending task, auto-assigned ID), TaskGet (full details by ID), TaskList (all tasks summary), TaskUpdate (status pending->in_progress->completed, owner assignment, blockedBy/blocks dependencies, or deleted). Replaces the whole-list-replace TodoWrite with ID-based per-task updates and dependency graphs. State persists in ~/.claude/tasks/<team-name>/ for team contexts.

**Data model:** TaskCreate: {subject, description, activeForm?, metadata?}. TaskUpdate: {taskId, status?, subject?, description?, activeForm?, owner?, addBlockedBy?, addBlocks?, metadata?}. TaskGet: {taskId}. TaskList: {} (returns summary).

**Config:** No permission required. New ID-based (vs old positional).

### Monitor / LSP / PowerShell / plan-mode / worktree / cron / agent-team / workflow / MCP / background-task tools
**Purpose:** Extended built-in tools beyond the core file/exec/agent set.

**Mechanism:** These are real, current tools but secondary to the core file/exec/agent set: Monitor (v2.1.98+, runs a watcher in background, reuses Bash permission rules, not on Bedrock/Vertex/Foundry); LSP (code intelligence, inactive until a code-intelligence plugin is installed; operations goToDefinition/findReferences/hover/documentSymbol/workspaceSymbol/goToImplementation/prepareCallHierarchy/incomingCalls/outgoingCalls); PowerShell (native, CLAUDE_CODE_USE_POWERSHELL_TOOL=1, spawns pwsh with -ExecutionPolicy Bypass process-scope); EnterPlanMode/ExitPlanMode (plan mode lifecycle); EnterWorktree/ExitWorktree (git worktree sessions under .claude/worktrees/); CronCreate/CronList/CronDelete (session-scoped scheduled prompts); ScheduleWakeup (reschedules a /loop iteration, 1min-1hr out); PushNotification (desktop + phone via Remote Control); SendMessage/TeamCreate/TeamDelete (agent teams, CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1); Workflow (dynamic multi-subagent orchestration); ShareOnboardingGuide; RemoteTrigger (claude.ai Routines behind /schedule); ListMcpResourcesTool/ReadMcpResourceTool/WaitForMcpServers/ToolSearch (MCP integration + deferred tool loading); TaskOutput (DEPRECATED — prefer Read on the task output file path); TaskStop (kill background task). Older/internal-only tools NOT in current v2 docs: BashOutput (read background shell output by bash_id, only NEW output since last check, optional regex filter that permanently drops non-matching lines) and KillShell (kill by shell_id) — these predate the run_in_background/task-id model.

**Data model:** Various; see docs table.

**Config:** Conditions: SendMessage/TeamCreate/TeamDelete need CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1. Monitor/RemoteTrigger/ScheduleWakeup/PushNotification unavailable on Bedrock/Vertex/Foundry. PowerShell needs CLAUDE_CODE_USE_POWERSHELL_TOOL=1 (off-C Windows). LSP needs a code-intelligence plugin. ToolSearch only when tool-search enabled.

## Key behaviors
- Read output uses `cat -n` 1-indexed line numbers with prefix `spaces + line_number + tab + content`; default first 2000 lines, each line truncated at 2000 chars; a whole-file read that exceeds the token limit returns a `PARTIAL view` notice (NOT an error), but a read that explicitly passes offset/limit and still exceeds returns an ERROR.
- Edit's THREE ordered checks: (1) read-before-edit (file read this conversation + unchanged on disk since) runs FIRST, (2) exact match, (3) uniqueness — old_string must appear EXACTLY ONCE or the edit FAILS (use replace_all:true or more context). Whitespace/indentation must match exactly.
- Read-before-edit / read-before-write is ALSO satisfied by Bash `cat`/`head`/`tail`/`sed -n 'X,Yp'`/`grep`/`egrep`/`fgrep` on a SINGLE file with NO pipes/redirects — but the deny-rule-checked command set differs (egrep/fgrep count for read-before-edit but NOT for Read deny rules). Piped output does NOT satisfy read-before-edit.
- Bash: 30,000 char output truncation default; when exceeded, FULL output is saved to a file in the session dir and the model receives the file path + a short preview from the start (raise cap via BASH_MAX_OUTPUT_LENGTH up to hard 150,000). Background task `.output` files have NO size limit and are never auto-cleaned.
- Bash `cd` carries to later commands ONLY within the project dir / added working dirs; landing outside resets to project dir and appends `Shell cwd was reset to <dir>`. Env vars do NOT persist across commands (export is gone next call); aliases/functions/options DO persist (sourced from ~/.zshrc/~/.bashrc/~/.profile at session start). CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR=1 disables carry-over; CLAUDE_ENV_FILE enables env persistence.
- Glob does NOT respect .gitignore by default (finds gitignored files) — DIFFERS from Grep which DOES respect .gitignore. Glob results sorted by mtime (recent first), capped at 100 files with a truncation flag. Set CLAUDE_CODE_GLOB_NO_IGNORE=false to make Glob respect .gitignore.
- Grep uses RIPGREP regex not POSIX grep (literal braces need escaping: `interface\{\}`); output_mode default is `files_with_matches` (paths only); -A/-B/-C/-n context flags only honored when output_mode=content; multiline default false; literal JSON keys `-i`/`-n`/`-A`/`-B`/`-C` mirror rg flags.
- TodoWrite is DISABLED BY DEFAULT as of v2.1.142 — replaced by TaskCreate/TaskGet/TaskList/TaskUpdate. Re-enable legacy TodoWrite with CLAUDE_CODE_ENABLE_TASKS=0. TodoWrite replaces the WHOLE list each call; Task* tools are ID-based and granular with dependency graphs.
- MultiEdit (batch edits, one file, `edits: [{old_string,new_string,replace_all}]`) was REMOVED in Claude Code v2.0 and is NOT in the current built-in tool set — replicas should implement parallel Edit calls instead of a MultiEdit tool.
- WebFetch is LOSSY by design: HTML->Markdown (not configurable), processed by a small fast model per the prompt (model gets the answer, not raw page), 15-min cache, HTTP auto->HTTPS, cross-host redirect returns original+target (no follow) requiring a second call. User-Agent starts with `Claude-User`.
- WebSearch returns TITLES + URLs only (no page fetch — follow up with WebFetch); may issue up to 8 backend searches per call; allowed_domains and blocked_domains CANNOT be combined in one call; permission rule takes NO specifier (bare `WebSearch` only); US-only; NOT on Bedrock.
- Agent/Task subagents: parent sees ONLY the final result, never intermediate tool calls; launching needs no permission but each subagent tool call is checked against session permission rules (background subagents auto-deny any prompting call); disallowedTools takes precedence over tools when both frontmatter fields set.
- All file tools require ABSOLUTE paths (relative rejected); NotebookEdit targets cells by cell_id not by index and not by string replacement; permission rules: Read/Grep/Glob/LSP use `Read(path)` format, Edit/Write/NotebookEdit use `Edit(path)` format (an Edit allow also grants read to same path), Bash/Monitor use `Bash(cmd pattern)`, WebFetch uses `WebFetch(domain:...)`, Agent uses `Agent(type)`, Skill uses `Skill(name)`.

## Open questions
- Exact current schema of the Task/Agent tool's optional `model` and `resume` fields and whether `description`/`subagent_type` remain strictly required in the latest v2.1.16x prompt (community schemas conflict slightly on required-ness).
- Whether TaskOutput is fully removed or merely deprecated in the very latest version (docs mark it deprecated, prefer Read on output file path).
- Exact composition of the built-in preapproved WebFetch documentation-domain set that skip the first-time domain prompt.
- Exact internal JSON result envelope shape for each tool (the model-facing text content is well documented, but the structured tool_result field names Claude Code itself emits for the API differ slightly and are not officially published.

## Sources
- [Tools reference - Claude Code Docs (official)](https://code.claude.com/docs/en/tools-reference) — PRIMARY source. Full official table of every built-in tool name + permission requirement + per-tool behavior sections (Read cat -n, Edit unique-match, Bash persistence/limits, Glob/Grep, NotebookEdit, WebFetch/WebSearch, Write, Agent, TodoWrite v2.1.142 deprecation, Task tools, Monitor/LSP/PowerShell/worktree/cron/workflow).
- [Internal claude code tools implementation (gist by bgauryy)](https://gist.github.com/bgauryy/0cdb9aa337d01ae5bd0c803943aa36bd) — Reverse-engineered EXACT JSON schemas (draft-07) and parameter interfaces for Read/Write/Edit/Glob/Grep/NotebookEdit/Bash/BashOutput/KillShell/Task/Skill/SlashCommand/TodoWrite/ExitPlanMode/AskUserQuestion/WebFetch/WebSearch/getDiagnostics/executeCode — the load-bearing field names and types for a replica.
- [Claude Code Tool Input Schemas (kaidhar/claude-code-permissions-hook)](https://github.com/kaidhar/claude-code-permissions-hook/blob/main/docs/tool-input-schemas.md) — Cross-referenced tool_input JSON shapes (verified against actual hook inputs) used by PreToolUse hooks — confirms MultiEdit schema (edits[] array), Task model/resume fields, LS tool (path+ignore), and MCP naming mcp__<server>__<tool>.
- [Claude Code 2.0 System Prompt Changes (Mikhail Shilkov)](https://mikhail.io/2025/09/sonnet-4-5-system-prompt-changes/) — Authoritative confirmation that MultiEdit was REMOVED in Claude Code v2.0 (existed as a ~70-line tool in v1.x), driving the decision NOT to reimplement a MultiEdit tool.
- [Tasks API vs TodoWrite (DeepWiki) + Reddit r/ClaudeAI](https://deepwiki.com/FlorianBruniaux/claude-code-ultimate-guide/8.1-tasks-api-vs-todowrite) — Confirms the v2.1.16 Tasks API introduction and the v2.1.142 default-disable of TodoWrite, plus the CLAUDE_CODE_ENABLE_TASKS env var semantics during rollout.
- [anthropics/claude-code Issue #19901 (Bash output limits)](https://github.com/anthropics/claude-code/issues/19901) — Official-tracked confirmation that Bash captures max 30,000 chars by default and spills full output to a session file with path+preview when exceeded.
- [Claude Code changelog (official)](https://code.claude.com/docs/en/changelog) — Version-specific Bash behavior changes (background shell stopped ~5s after result when stdin closes; $()/$VAR subshell pattern matching) and the CLAUDE_CODE_ENABLE_TASKS gating timeline.
- [Piebald-AI claude-code-system-prompts CHANGELOG](https://github.com/Piebald-AI/claude-code-system-prompts/blob/main/CHANGELOG.md) — Tracks the system-prompt swap that resolves the TodoWrite tool reference to TaskCreate or TodoWrite depending on whether tasks are enabled — confirms the dual-resolution mechanism.
