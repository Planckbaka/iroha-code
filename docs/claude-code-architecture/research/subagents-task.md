# Research: subagents-task

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code's subagent system is orchestrated by a single model-facing meta-tool: the "Agent" tool (legacy alias "Task", renamed in v2.1.63). When the parent model calls Agent with {subagent_type, prompt, description, model, run_in_background}, it spawns a child agent that runs its own full conversation loop in an isolated context window with its own system prompt, tool pool, permission boundary, and abort controller. The child does its work and returns ONLY its final message verbatim as the tool result — the parent never sees intermediate tool calls or reasoning. Subagents are defined as Markdown files with YAML frontmatter at .claude/agents/ (project), ~/.claude/agents/ (user), via --agents CLI JSON, in plugins, or via managed settings, with a fixed 5-level precedence. Each subagent's "description" field drives automatic delegation, but users can force invocation via natural-language naming, @-mention, or --agent (run whole session as that agent). Parallel spawning happens naturally when the model emits multiple Agent tool calls in one turn; background subagents (run_in_background:true or background:true frontmatter or Ctrl+B) run concurrently and auto-deny any prompt. As of v2.1.172, subagents can spawn nested subagents (foreground at any depth, background capped at depth 5). Communication beyond prompt/result uses the "SendMessage" tool (only with CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1), which routes by recipient name/ID/UDS-socket/bridge-session and auto-resumes dead agents from their disk transcript.

## Components
### AgentTool (a.k.a. Task tool)
**Purpose:** The model-facing meta-tool that spawns a child subagent. The ONLY tool the parent model calls to delegate work; everything below flows from it.

**Mechanism:** Registered via buildTool() factory under name "Agent" with legacy alias "Task". call() runs a 10-step decision tree BEFORE runAgent(): (1) teammate? (team_name+name set) -> spawnTeammate(); (2) resolve effective agent type: subagent_type provided -> use it; omitted+fork enabled -> undefined (fork path); omitted+fork disabled -> "general-purpose" default; (3) fork guard check; (4) resolve definition from activeAgents, filtering by permission deny rules + allowedAgentTypes, throw if not found/denied; (5) wait up to 30s for required MCP servers; (6) resolve isolation (param overrides def): remote->teleportToRemote(), worktree->createAgentWorktree(), null->normal; (7) sync-vs-async decision: shouldRunAsync = run_in_background || selectedAgent.background || isCoordinator || forceAsync || isProactiveActive; (8) assemble worker tool pool; (9) build system prompt + prompt messages; (10) execute (async -> registerAsyncAgent + void lifecycle; sync -> iterate runAgent inline). The dynamic prompt from getPrompt() is context-sensitive (lists available agents as an attachment message to avoid busting prompt cache, NOT inline in tool description).

**Data model:** TaskInput (zod, feature-gated):
Base (always present): description (string, required, 3-5 word summary), prompt (string, required, full task instructions), subagent_type (string, optional), model (enum sonnet|opus|haiku, optional), run_in_background (boolean, optional).
Full schema additions (when swarm/isolation features active): name (string, makes agent addressable via SendMessage({to:name})), team_name (string), mode (PermissionMode), isolation (enum worktree|remote), cwd (string, absolute path override).
Feature-gated omissions: when fork active OR CLAUDE_CODE_DISABLE_BACKGROUND_TASKS set, run_in_background is stripped; when KAIROS flag off, cwd is omitted. The model never sees fields it cannot use.

**Config:** type: Agent; name 'Agent'; legacy alias 'Task' for backward compat with older transcripts/permission rules/hook configs.

### AgentDefinition file format (.claude/agents/*.md)
**Purpose:** Declarative definition of a subagent: identity, capabilities, system prompt, and lifecycle config. Single source reused across subagent invocation, @-mention, --agent main-thread mode, and agent-team teammates.

**Mechanism:** Loaded at session START only (restart required for disk edits; /agents UI edits take effect immediately). Five scope locations with priority: (1) Managed settings org-wide [highest], (2) --agents CLI flag JSON [session], (3) .claude/agents/ [project], (4) ~/.claude/agents/ [user], (5) plugin agents/ dir [lowest]. Project & user scanned RECURSIVELY (subfolders OK, identity from name field only — keep names unique within a scope or one is silently discarded). Plugin subfolders BECOME part of the scoped id (agents/review/security.md in plugin my-plugin -> my-plugin:review:security). --agents JSON uses same fields, with `prompt` field = markdown body. Programmatic SDK agents take precedence over filesystem agents with the same name.

**Data model:** ---
name: <lowercase-hyphens>      # REQUIRED
<description>                   # REQUIRED (when to delegate)
tools: Read, Glob, Grep         # optional comma-list or YAML array; '*' = all
disallowedTools: Write, Edit    # denylist; applied BEFORE tools allowlist resolves
model: sonnet|opus|haiku|fable|<full-id>|inherit   # default: inherit
permissionMode: default|acceptEdits|auto|dontAsk|bypassPermissions|plan
maxTurns: <number>
skills: [skill-name, ...]       # full content injected, not just description
mcpServers: [{<name>: {type,command,args}}, "<ref-name>"]
hooks: {PreToolUse|PostToolUse|Stop: [{matcher, hooks:[{type:command,command}]}]}
memory: user|project|local      # dir at ~/.claude/agent-memory/<name>/ etc.
background: true|false          # default false
effort: low|medium|high|xhigh|max|<number>
isolation: worktree              # temp git worktree branched from default branch
color: red|blue|green|yellow|purple|orange|pink|cyan
initialPrompt: <string>          # auto-submitted as first user turn when agent runs as MAIN session (--agent)
---
<markdown body becomes system prompt>

**Config:** name format: lowercase + hyphens (filename need not match name). model resolution precedence: CLAUDE_CODE_SUBAGENT_MODEL env -> per-invocation model param -> frontmatter model -> main model. plugins IGNORE hooks, mcpServers, permissionMode fields (security).

### Built-in subagent registry (6 types)
**Purpose:** The always-available agents Claude delegates to automatically. Cover exploration, planning, general work, verification, and UI helpers.

**Mechanism:** General-purpose: full tools (minus Agent), no CLAUDE.md omission, model=getDefaultSubagentModel(). Explore: Haiku, read-only (FileEdit/FileWrite/NotebookEdit/Agent removed), CRITICAL: READ-ONLY MODE in prompt, one-shot — most spawned (~34M/week). Plan: 'inherit' model, read-only, 4-step structured process ending with Critical Files list, one-shot. Verification: read-only, 'inherit', background:true always, red, ~130-line anti-avoidance prompt, criticalSystemReminder_EXPERIMENTAL guardrail. statusline-setup: Sonnet, Read+Edit only, orange. claude-code-guide: Haiku, dontAsk mode, excluded when entrypoint=SDK. Disable all built-ins via CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1; deny specific via permissions.deny=["Agent(Explore)"] or --disallowedTools.

**Data model:** Type registry built dynamically by getBuiltInAgents() gated by feature flags + GrowthBook experiments (BUILTIN_EXPLORE_PLAN_AGENTS + tengu_amber_stoat for Explore/Plan; VERIFICATION_AGENT + tengu_hive_evidence for Verification).

**Config:** Explore & Plan have omitClaudeMd:true (strip CLAUDE.md + git status, saves tokens; only these two skip them, NO frontmatter field to change). Explore/Plan are ONE_SHOT (no agentId returned, no SendMessage instructions, no usage trailer). Agent tool is in default disallowedTools for general-purpose to prevent exponential fan-out.

### runAgent() 15-step lifecycle
**Purpose:** The single async-generator function that creates and drives a subagent's entire execution context. Every subagent type (fork/built-in/custom/coordinator-worker) flows through it.

**Mechanism:** 15 steps: (1) Model resolution chain caller-override > agent-def > parent-model > default (getAgentModel handles 'inherit'); (2) agentId creation (override.agentId or createAgentId() -> agent-<hex>); (3) context prep — fork clones parent history via filterIncompleteToolCalls() (strips tool_use blocks lacking matching tool_result, else API rejects); fresh agents start empty; file-state cache fork=clone, fresh=createWithSizeLimit; (4) CLAUDE.md stripping for read-only agents; (5) permission isolation — custom getAppState() overlays agent mode unless parent is bypassPermissions/acceptEdits/auto (parent wins); async agents get shouldAvoidPermissionPrompts:true; allowedTools replaces session allow rules but preserves SDK --allowedTools; (6) tool resolution (fork: useExactTools passthrough for byte-identical cache prefix; else resolveAgentTools applies tools/disallowedTools/ASYNC_AGENT_ALLOWED_TOOLS); (7) system prompt (fork uses override.systemPrompt = parent's exact rendered bytes; else getAgentSystemPrompt + env details); (8) abort controller isolation (async=new unlinked controller; sync=parent's shared controller); (9) register frontmatter hooks scoped to agentId, Stop->SubagentStop conversion, strictPluginOnlyCustomization skips user agent hooks; (10) preload skills (3-strategy name resolution) as user messages; (11) MCP init (name refs shared/memoized, inline created+cleaned up); (12) createSubagentContext (sync shares setAppState, async isolates it; both share setAppStateForTasks + setResponseLength; messages own array); (13) onCacheSafeParams callback for background summarization; (14) query() loop drives child conversation, yields Messages, each recorded to sidechain transcript JSONL O(1); (15) finally{} cleanup: mcpCleanup, clearSessionHooks, cleanupAgentTracking, readFileState.clear(), initialMessages.length=0, unregisterPerfettoAgent, clearAgentTranscriptSubdir, remove agent's todos, killShellTasksForAgent.

**Data model:** runAgent signature: {agentDefinition, promptMessages, toolUseContext, canUseTool, isAsync, canShowPermissionPrompts, forkContextMessages, querySource, override, model, maxTurns, availableTools, allowedTools, onCacheSafeParams, useExactTools, worktreePath, description}. agentId branded type AgentId = `agent-<crypto.randomUUID()-hex>`.

**Config:** Thinking disabled for normal agents ({type:'disabled'}) to control cost; fork agents inherit thinkingConfig for cache identity. Explore/Plan skip CLAUDE.md & git status (gate tengu_slim_subagent_claudemd defaults true).

### Task state machine + async communication
**Purpose:** Unified state model for all background operations (shell, subagent, teammate, remote, workflow, mcp-monitor, dream). Backbone of background agent tracking, progress, and result delivery.

**Mechanism:** Three comms channels: (1) Disk output files (outputFile symlink to JSONL transcript, read incrementally via outputOffset; TaskOutputTool polls, block:true polls until terminal/timeout); (2) Task notifications (<task-notification> XML injected as user-role message in parent conversation, deduped via notified flag); (3) Command queue pendingMessages[] drained at tool-round boundaries by drainPendingMessages() (messages arrive BETWEEN tool rounds, never mid-execution). ProgressTracker tracks toolUseCount, latestInputTokens (cumulative-latest), cumulativeOutputTokens (summed), recentActivities (cap 5). Backgrounding mid-execution: Promise.race between next-message and background-signal; foreground iterator.return() triggers cleanup, re-spawn as async with same ID, flip isBackgrounded.

**Data model:** TaskStateBase: {id (prefixed random, ~2.8T combos), type, status, description, toolUseId, startTime, endTime?, totalPausedMs?, outputFile (disk path), outputOffset (read cursor), notified (dedup flag)}. LocalAgentTaskState adds: agentId, prompt, selectedAgent, agentType, model?, abortController?, pendingMessages[], isBackgrounded, retain, diskLoaded, evictAfter?, progress?, lastReportedToolCount, lastReportedTokenCount. AppState.tasks is flat Record<string,TaskState> (no parent-child tree).

**Config:** 7 types: local_bash(b), local_agent(a), remote_agent(r), in_process_teammate(t), local_workflow(w), monitor_mcp(m), dream(d). 5 statuses: pending->running->{completed|failed|killed}. isTerminalTaskStatus() guards message injection.

### SendMessage + agent teams (inter-agent messaging)
**Purpose:** Universal communication primitive across subagents, coordinator workers, swarm teammates, and remote/UDS peers. Single tool, 4 routing modes by shape of `to` field.

**Mechanism:** Leader spawns teammates (in-process via AsyncLocalStorage, or split-pane via tmux/iTerm2). SendMessage routes by `to`: bridge:<session-id> (remote relay, needs consent) > uds:<socket> (local IPC) > agentNameRegistry lookup (running->queuePendingMessage; terminal->resumeAgentBackground; not in AppState->resume from disk transcript) > team mailbox fallback. Mailbox = writeToMailbox() file per recipient; to:"*" broadcasts to all members except sender (no fan-out opt). Structured protocols: shutdown_request/response (cooperative, teammate may reject), plan_approval_response (only lead approves). Auto-resume: SendMessage to dead agent reads sidechain JSONL, filters orphaned thinking/tool blocks, rebuilds content-replacement state, re-registers as background task, runs runAgent() with restored history + new message. Workers cannot spawn sub-teams (INTERNAL_WORKER_TOOLS deny set). Known bug: SendMessage by agent NAME for completed/resumed agents may silently fail — agent ID is reliable (GitHub issue #42999).

**Data model:** InProcessTeammateTaskState: type 'in_process_teammate', identity, prompt, messages? (UI cap 50), pendingUserMessages[], isIdle, shutdownRequested, awaitingPlanApproval, permissionMode, onIdleCallbacks?, currentWorkAbortController (distinct from main kill controller — cancels current turn only, redirect pattern). TeamContext: {teamName, teammates:{[id]:{name,color}}}. agentNameRegistry: Map<string,AgentId>.

**Config:** Requires CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1 (experimental). Stored on disk: team config ~/.claude/teams/{team-name}/config.json (members array with name, agentId, agentType), task list ~/.claude/tasks/{team-name}/. Both removed on cleanup. NO project-level teams.json recognized.

### Termination & resume contract
**Purpose:** How subagents end, how their result returns to parent, and how they can be continued.

**Mechanism:** When subagent completes, Agent tool result includes text block 'agentId: <id>'. Explore/Plan are one-shot (no agentId, cannot resume). To resume: parent uses SendMessage({to: agentId}) (only available with CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1) OR SDK resumes by passing resume:<sessionId> + naming agentId in prompt. Transcripts at ~/.claude/projects/{project}/{sessionId}/subagents/agent-{agentId}.jsonl — persist independently of main conversation (main compaction doesn't touch them); cleaned up via cleanupPeriodDays (default 30). Stopped subagent receiving SendMessage auto-resumes in background without new Agent invocation.

**Data model:** Agent tool output discriminated union: {status:'completed', prompt, ...AgentToolResult} | {status:'async_launched', agentId, description, prompt, outputFile}. (Internal-only TeammateSpawnedOutput & RemoteLaunchedOutput excluded from exported schema for dead-code-elimination.)

**Config:** builtIn always registered in interactive sessions; disable specific via permissions.deny=["Agent(<name>)"] or --disallowedTools. Resume requires non-one-shot agent (general-purpose/custom); Explore/Plan cannot resume. CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1 disables all background; CLAUDE_CODE_FORK_SUBAGENT=1 forces all spawns to background.

## Key behaviors
- The Task->Agent rename (v2.1.63) is a BREAKING CHANGE for hook scripts: PreToolUse/PostToolUse hooks that string-match the tool name must now check BOTH 'Task' and 'Agent' for cross-version compatibility. The SDK still emits 'Agent' in tool_use blocks but 'Task' in system:init tools list and result.permission_denials[].tool_name.
- Model resolution order is FIXED and non-obvious: CLAUDE_CODE_SUBAGENT_MODEL env > per-invocation model param > frontmatter model > main conversation model. 'inherit' resolves to parent's model. Explore defaults to Haiku for external users via GrowthBook gating.
- Subagent receives ONLY: its own system prompt + Agent tool prompt + project CLAUDE.md (except Explore/Plan) + git status snapshot (except Explore/Plan) + preloaded skills. It does NOT receive parent conversation history, parent system prompt, or preloaded skill content unless in AgentDefinition.skills. The parent->child channel is ONLY the prompt string.
- The parent receives the subagent's FINAL message VERBATIM as the Agent tool_result (may be summarized by parent in its own response). To preserve verbatim subagent output in user-facing response, instruct the main query() to do so — the contract is not automatic.
- Foreground subagents share the parent's abort controller (Escape kills both); background subagents get an independent controller (Escape on parent does NOT kill them). Backgrounding mid-execution re-spawns with same ID and flips isBackgrounded.
- Background subagents auto-deny ANY tool call that would prompt (no terminal attached); foreground passes prompts through to user. Named/background subagents auto-deny prompting tools; 'bubble' mode is the exception that surfaces prompts to parent terminal.
- If 'Agent' is omitted from a subagent's tools list, it CANNOT spawn nested subagents. 'Agent(worker, researcher)' allowlist syntax ONLY applies when running as main thread via --agent; in a subagent definition, any type list in parens is IGNORED (bare Agent enables nesting).
- Nested subagent depth limit (v2.1.172): foreground can spawn at any depth (self-limiting via blocking); background subagent at depth 5 gets NO Agent tool and cannot spawn further. The limit is fixed and NOT configurable. Fork still cannot spawn another fork (querySource==='agent:builtin:fork' guard + isInForkChild scan for <fork-boilerplate>).
- Permission mode cascade: if parent is bypassPermissions, acceptEdits, or auto mode, the PARENT'S mode always wins — the subagent's permissionMode frontmatter is IGNORED. Otherwise the agent's mode applies. This prevents a custom agent from downgrading security the user explicitly set.
- Auto-resume via SendMessage: sending a message to a completed/killed agent transparently resurrects it from its disk JSONL transcript (filters orphaned thinking/tool blocks, rebuilds content-replacement state for cache stability). Coordinators do not need to track agent liveness. CAVEAT: GitHub issue #42999 reports SendMessage by agent NAME silently fails for some resume paths — agent ID is the reliable target.
- transcripts persist separately from main conversation: main-conversation compaction does NOT touch subagent transcripts. They survive session restart and are cleaned up via cleanupPeriodDays (default 30 days). Sidechain recording is O(1) per message (append-only, previous-UUID reference).
- Plugin subagents CANNOT use hooks, mcpServers, or permissionMode frontmatter fields (silently ignored for security). Copy into .claude/agents/ if you need them. As of v2.1.153, main-session MCP restrictions (--strict-mcp-config, --bare, managed MCP, allowedMcpServers/deniedMcpServers) also cover servers declared in subagent frontmatter (but --strict-mcp-config does NOT filter inline --agents/SDK agents servers — those are explicit caller input).
- Filesystem-based agents load at SESSION START only. Editing a .claude/agents/*.md on disk requires a session restart. /agents UI edits take effect immediately. Windows: very long subagent prompts may fail (>8191 char command-line limit) — use filesystem agents.
- Explore/Plan are the ONLY agents that skip CLAUDE.md and git status, and there is NO frontmatter field to change which agents skip them. If a rule must reach Explore/Plan, restate it in the delegation prompt.
- In agent teams: subagent definitions used as teammates apply ONLY tools + model; the body is APPENDED to teammate system prompt (not replacing). skills and mcpServers fields are NOT applied on the teammate path (teammates load those from project/user settings like a regular session). Team coordination tools (SendMessage, task tools) are ALWAYS available even when tools restricts others.

## External interfaces
- Tool name: 'Agent' (primary), 'Task' (legacy alias) — emitted in tool_use blocks; system:init tools list & result.permission_denials[].tool_name still use 'Task' in some SDK versions
- Agent tool input: {description, prompt, subagent_type?, model?, run_in_background?, name?, team_name?, mode?, isolation?, cwd?}
- Agent tool output: {status:'completed', prompt, ...result} | {status:'async_launched', agentId, description, prompt, outputFile}
- SendMessage tool input: {to: name|'*'|'uds:<socket>'|'bridge:<session-id>'|agentId, summary?, message: string | {type:'shutdown_request'|'shutdown_response'|'plan_approval_response', ...}}
- TaskStop tool input: {task_id?, shell_id? (deprecated)} — legacy alias 'KillShell'
- TaskOutput tool input: {task_id, block=true, timeout=30000}
- File formats: .claude/agents/*.md & ~/.claude/agents/*.md (YAML frontmatter + markdown body); --agents JSON (prompt field = body); subagent transcripts ~/.claude/projects/{project}/{sessionId}/subagents/agent-{agentId}.jsonl
- CLI flags: --agent <name>, --agents '<json>', --disallowedTools 'Agent(Explore)', --teammate-mode in-process|tmux|auto, settings 'agent' & 'teammateMode'
- Env vars: CLAUDE_CODE_SUBAGENT_MODEL, CLAUDE_CODE_DISABLE_BACKGROUND_TASKS, CLAUDE_CODE_FORK_SUBAGENT, CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS, CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS, CLAUDE_CODE_COORDINATOR_MODE
- Permission rule forms: 'Agent', 'Agent(worker, researcher)' (allowlist only when main --agent), 'Agent(Explore)' in permissions.deny

## Open questions
- Exact content/wording of the Explore agent's 'CRITICAL: READ-ONLY MODE' system prompt section and the general-purpose system prompt (described but not quoted verbatim in sources)
- Full list and exact gating conditions of the ~12 feature flags + GrowthBook experiments (FORK_SUBAGENT, BUILTIN_EXPLORE_PLAN_AGENTS, VERIFICATION_AGENT, KAIROS, TRANSCRIPT_CLASSIFIER, PROACTIVE, tengu_amber_stoat, tengu_hive_evidence, tengu_slim_subagent_claudemd, tengu_scratch) — which are compile-time vs runtime A/B
- Exact AgentProgress type fields and the ASYNC_AGENT_ALLOWED_TOOLS allowlist contents
- Whether the 'dream' task type (speculative background thinking) and 'local_workflow' Workflow tool are GA or still feature-gated as of v2.1.175
- Whether coordinator mode (CLAUDE_CODE_COORDINATOR_MODE) is GA or still behind COORDINATOR_MODE feature flag for general users

## Sources
- [Create custom subagents — Claude Code Docs (official)](https://code.claude.com/docs/en/sub-agents) — PRIMARY source. Full frontmatter field table, 5 scope priorities, built-in subagent details (Explore/Plan/general-purpose), isolation:worktree, what-loads-at-startup matrix, resume contract, nested depth rules.
- [Subagents in the SDK — Claude Code Docs (official)](https://code.claude.com/docs/en/agent-sdk/subagents) — AgentDefinition field table (description/prompt/tools/disallowedTools/model/skills/memory/mcpServers/initialPrompt/maxTurns/background/effort/permissionMode), what-subagents-inherit matrix, v2.1.63 Task->Agent rename + dual-name detection guidance, resume via agentId, v2.1.172 nested depth rule.
- [Orchestrate teams of Claude Code sessions — Claude Code Docs (official)](https://code.claude.com/docs/en/agent-teams) — Agent teams architecture (lead/teammates/task list/mailbox), team+task disk paths, subagent-definitions-for-teammates (tools+model honored, body appended, skills/mcpServers ignored), mailbox messaging, plan approval protocol, v2.1.32 minimum.
- [Ch 8. Spawning Sub-Agents — Claude Code from Source](https://claude-code-from-source.com/ch08-sub-agents/) — Authoritative internals: AgentTool base+full input schema with feature-gated field omissions, 10-step call() decision tree, full 15-step runAgent() lifecycle, 6 built-in agent types with feature gates, fork guard mechanics, output schema discriminated union.
- [Ch 10. Tasks, Coordination, and Swarms — Claude Code from Source](https://claude-code-from-source.com/ch10-coordination/) — Task state machine (7 types, 5 statuses, TaskStateBase/LocalAgentTaskState fields), 3 background comms channels (disk/notifications/queue), SendMessage 4-mode routing + auto-resume, TaskStop kill switch, coordinator mode internals, swarm mailbox.
- [Claude Code changelog — Claude Code Docs (official)](https://code.claude.com/docs/en/changelog) — Confirms version-specific facts: v2.1.172 'Sub-agents can now spawn sub-agents up to 5 levels deep'; Workflow tool agent() attribution.
- [v2.1.63 Task->Agent tool rename breaking hooks — GitHub Issue #29677](https://github.com/anthropics/claude-code/issues/29677) — Confirms the v2.1.63 Task->Agent rename is a breaking change for PreToolUse/PostToolUse hook scripts that check the tool name.
- [SendMessage silently fails when using agent name — GitHub Issue #42999](https://github.com/anthropics/claude-code/issues/42999) — Documents the gotcha that SendMessage with agent NAME may silently fail for resuming completed agents; only agent ID works reliably.
- [Claude Code v2.1.172 Release Notes — claudeupdates.dev](https://www.claudeupdates.dev/version/2.1.172) — Independent corroboration of v2.1.172 nested subagent (5-level) release and the agent-lifecycle stability fixes (stuck-active panel, fixed background agent project-settings isolation).
- [Task tool input schema (TaskArgs) — letta-ai/letta-code Task.ts](https://github.com/letta-ai/letta-code/blob/32e042d5/src/tools/impl/Task.ts) — Third-party reimplementation confirming exact Task tool args: command/subagent_type/prompt/description/model/agent_id/conversation_id/run_in_background, validating the schema shape from primary sources.
