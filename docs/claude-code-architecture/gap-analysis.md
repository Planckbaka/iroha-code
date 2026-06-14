# Gap Analysis — iroha (go-claude) vs Claude Code

Organized by the three architectural clusters. Status legend: ✅ present & faithful · 🟡 partial · 🔴 missing · ⚠️ divergent (present but wrong).

The recurring structural blocker is called out once here and applies throughout: **the agent loop, tool registry, MCP-tool wrapper, subagent execution, and LLM streaming all speak Google ADK / `genai` types — Claude Code owns native equivalents.** See [refactor-plan.md](refactor-plan.md) for the decoupling strategy.

---

## Cluster A — Core Engine & Runtime
*(agent loop · tool set · tool-exec engine · streaming · session/transcript · compaction · system-prompt assembly)*

### The loop itself
| Capability | CC | iroha | Status |
|---|---|---|---|
| Native model→tool→model iteration (`queryLoop`) | owned, ~1,730 lines, one path | **outsourced to ADK `Flow.Run`**; `Execute()` only forwards events | 🔴 **THE critical gap** |
| `max_turns` counts only tool-use turns | yes | n/a (no native loop) | 🔴 |
| Parallel read-only / sequential stateful tool dispatch | yes (StreamingToolExecutor) | ADK decides | ⚠️ (uncontrolled) |
| `yieldMissingToolResultBlocks` safety net (orphan tool_use) | yes | ADK internal | ⚠️ |
| Stop-hook forced continuation (`stopHookActive`) | yes | no | 🔴 |
| Token-budget auto-continue (`0.9` / `500` thresholds) | yes | no | 🔴 |

### Tool set
| Tool | CC | iroha | Status |
|---|---|---|---|
| Read (cat -n, slices, 10MB) | yes | `file_read` | ✅ |
| Write (**Read-before-overwrite** enforced) | yes | `file_write` overwrites blindly | ⚠️ |
| Edit (**requires prior Read**, unique-match) | yes | `file_edit` allows blind edits | ⚠️ |
| MultiEdit | yes | `file_edit_batch` (atomic, rollback) | ✅ (parity+) |
| **NotebookEdit** | yes | — | 🔴 |
| Bash (timeout to 600000ms, `run_in_background`, real sandbox) | yes | `shell_run` (30s, 500-line cap, heuristic sandbox) | 🟡 |
| Glob (doublestar/fsnotify) | yes | hand-rolled `**`, **O(n²) bubble sort**, 100-cap | ⚠️ |
| Grep (**ripgrep-backed**, `-i/-g/-A/-B/-C/output_mode`) | yes | pure-Go regex walk, 50-match cap, no flags | ⚠️ |
| Task/Agent (`run_in_background`, `TaskStop`, structured handoff) | yes | `spawn_subagent` **synchronous only** | 🟡 |
| TodoWrite (structural single-in_progress) | yes | text-only enforcement | 🟡 |
| WebSearch / WebFetch (hosted backend, readability, URL-context) | yes | DDG-scrape / SearXNG, naive htmlToText, 5MB | 🟡 |

iroha **extras** (not in CC): LSP tools (native, not MCP), CI watcher, worktree manager, memory dream consolidator, auto-review LLM judge, AGENTS.md↔memory sync.

### Tool execution engine
- ✅ Permission gating + hook pipeline + snapshot/rollback — implemented, but via the **`blockingConfirmationTool` ADK-wrapper hack** (overwrites `req.Tools` map to force dispatch). ⚠️ Structural divergence; a native registry calls permission inline before dispatch.
- ⚠️ **No `required`-field schema validation** at registration (relies on LLM correctness); CC uses explicit JSON-schema `required` arrays.

### Streaming protocol
- ✅ Direct Anthropic + OpenAI adapters parse SSE and emit `model.LLMResponse` chunks.
- 🔴 **No SDK message taxonomy** (`SystemMessage`/`AssistantMessage`/`UserMessage`/`StreamEvent`/`ResultMessage`) — iroha consumes opaque `session.Event`. Headless `stream-json` mode absent.
- ⚠️ Anthropic adapter **hardcodes `MaxTokens:8192`** and ignores configured max_output_tokens.

### Session/transcript
- ✅ `PersistentSessionService` JSON-per-session, resume/last/fork, session picker.
- ⚠️ **Not the CC transcript format**: iroha serializes `[]*session.Event` + state map. CC is append-only JSONL with `uuid`+`parentUuid` DAG, `compact_boundary` records, `isCompactSummary` user messages, `toolUseResult` fields. Resume loses tool-card fidelity (replay concatenates text parts).
- ⚠️ Token/cost = `bytes/4` and `$2/M` placeholder (not per-model, no real tokenizer).

### Compaction
- ✅ Microcompact (archives >1000B tool results to transcript JSONL) + round-based summarization (>12 rounds) + circuit breaker + sticky-block preservation.
- ⚠️ **Divergent strategy**: round/byte-based, not CC's token-threshold API microcompact (`clear_tool_uses_20250919`: 180k→40k tokens; `clear_thinking_20251015`).
- 🔴 **No restore path** — archives are append-only, never read back into context; CC restores on edit.
- ⚠️ Sticky mechanism is a bespoke `[STICKY]` text marker capped at 20% of a hardcoded 200000-byte estimate; CC uses prompt-cache breakpoints + file/snapshot references.

### System prompt assembly
- ✅ `SystemPromptBuilder` assembles identity/persona/memories/CLAUDE.md/AGENTS.md/skills/dynamic sections with SHA-256 `cached:` hints.
- 🔴 **CLAUDE.md placement wrong**: iroha puts CLAUDE.md **in the system prompt**; CC injects it as a **user message** (verified). This breaks prompt-cache semantics.
- 🔴 **No real `cache_control` breakpoints** — only a string-hash comment; CC uses provider-side cache breakpoints.
- ⚠️ Rebuilt inside the model delegator (keyed off `GlobalMessageCount`), not at the turn boundary.
- ⚠️ `sanitizeADKStatePlaceholders` is an ADK-template-injection guard — dead weight under a native loop.

---

## Cluster B — Trust Boundary (permissions · hooks · sandbox · MCP)

### Permissions
- ✅ All 6 modes + rule engine (allow/deny/ask) + `BashSecurityValidator` (14 regex) + 4-tier risk classifier.
- 🔴 **Not CC's config format**: hardcoded built-in rules + `AddRule` API, not `settings.json` `permissions.{allow,deny,ask}` arrays. No enterprise managed-settings / `settings.local.json` merge, no `additionalDirectories`, no gitignore-style matching.
- ⚠️ `matchesPattern` uses substring fallback (looser than CC).
- ⚠️ Permission order not guaranteed deny→ask→allow with correct precedence.

### Hooks
- ✅ 12 events (covers CC's 8 + extras), command/http/llm-prompt types, matchers, stdin-JSON/stdout-JSON/exit-code protocol, project-hook trust gate (`IROHA_TRUST_PROJECT_HOOKS`), env whitelisting (good secret hygiene).
- 🔴 **`PreToolUse` does not use `hookSpecificOutput.permissionDecision`** semantics (allow/deny/ask/defer + `updatedInput`); does not fire before permission-mode checks.
- ⚠️ Config at `.iroha/hooks.json`, not `.claude/settings.json` hooks block (shape close but not identical).
- ⚠️ `llm-prompt` hook type is an **iroha extension** (CC has no native LLM hook).

### Sandbox
- ✅ Real OS-level sandbox: mac `sandbox-exec` (generated Seatbelt profile) + linux `bwrap`, graceful fallback.
- ⚠️ Seatbelt profile is **allow-by-default** (`(allow default)` then denies specific paths) — **weaker** than CC's deny-by-default; network implicitly allowed.
- ⚠️ Two **overlapping** path-escape checkers (`checkShellCommandSandbox` + `isPathDangerous`) with divergent whitelists.
- ⚠️ CC ships its own sandboxing binary (seatbelt helper / landlock+namespaces) with granular workspace allowlisting + network policy.

### MCP
- ✅ Stdio JSON-RPC 2.0 client, `tools/list` → `DynamicMCPTool` (`mcp__server__tool`), plugin discovery, per-skill `plugins.json`, `/mcp` reload.
- 🔴 **HTTP transport + OAuth are implemented but NOT wired** — `LoadAndStartPlugins` always constructs stdio `NewMCPClient`, ignoring `config.URL`; OAuth tokens never attached. URL/OAuth MCP servers silently fall back and fail.
- 🔴 **Protocol version pinned to `2024-11-05`**; CC uses **`2025-06-18`** (elicitation, structured tool output, resource links).
- 🔴 No resource/prompt subscriptions, no sampling, no cancellation, no logging notifications. MCP stderr silently discarded.
- ⚠️ MCP tool result parsed as `map[string]any` (non-object JSON errors); CC normalizes content blocks / `is_error` / structured output.

---

## Cluster C — Human Interface & Orchestration (memory · subagents · skills · slash/plan · TUI/config)

### Memory / CLAUDE.md
- ✅ File-based memory (YAML frontmatter, global+project layers, 100-cap), trigger-aware injection, AGENTS.md↔memory sync, dream consolidator.
- 🔴 **Wrong layer**: memory + CLAUDE.md injected into system prompt; CC injects CLAUDE.md as a **user message**.
- ⚠️ Memory model (user/feedback/project/reference `.md`) is iroha-specific, not CC's CLAUDE.md-only convention + `memory` tool.
- ⚠️ Dream consolidator (dedup + LLM merge + PID lock + 7 gates) has no CC equivalent; `ConsolidateSemantically` deletes originals before validating LLM JSON (not transactional).

### Subagents / Task
- ✅ 6 typed agents (explore/planner/reviewer/researcher/executor/work), curated toolsets, worktree isolation for executor/work, JSONL logs, file-diff derivation.
- 🔴 **Forced cheap model** (haiku/flash/4o-mini) unless overridden; CC spawns with parent's model.
- 🔴 **No parent↔child context handoff**; isolated in-memory session; parent gets only text summary + git file lists, not a structured handoff or visible tool transcript.
- 🔴 Synchronous only (no `run_in_background` / `TaskStop`).

### Skills
- ✅ Discovery (~/.iroha/skills + project), `skill.json` manifest, 3 types (model/user/always), path-escape guard.
- 🔴 **Naive substring trigger matching** + eager body load; CC uses **model-driven progressive disclosure** (model decides when to expand SKILL.md body).
- ⚠️ Plugin namespace not CC's `plugin-name:skill-name`.

### Slash commands + plan mode
- ✅ ~22 commands + autocomplete, `/compact`, `/context`, `/sessions`, `/resume`, `/team`, `/worktree`, `/bg`, `/skill`, `/mcp`, permission/session screens.
- 🔴 **No live `/model` hot-swap** (startup-only); CC has `/model`.
- 🔴 `/trace` is a stub; CC surfaces a live tool-call timeline.
- 🔴 **No plan mode tool pair** (`EnterPlanMode`/`ExitPlanMode`) with the 5-option approval flow.
- ⚠️ Custom-command `.claude/commands/*.md` support with `$ARGUMENTS`/`$1`/`!`/`@file` — verify parity (audit didn't confirm full).

### TUI / IDE / config
- ✅ Hand-rolled retained-mode event loop + differential renderer + glamour (width-keyed cache + stream memoization), component model, multiline input, history viewport, confirmation card, status bar. **Genuinely good TUI engineering.**
- ⚠️ **Not Bubble Tea** (re-implements viewport/scroll/cursor) — and CC is React/Ink anyway, so this is a style choice, not a fidelity bug. Keep it.
- ⚠️ `/context` uses chars/4 heuristic, not real tokens.
- 🔴 **Ctrl+Y copy-last-response advertised but unwired**; `/trace` stub.
- 🔴 **No IDE integration** (VS Code/JetBrains bridge).
- ⚠️ Config at `~/.iroha.json`, not CC's `settings.json` 4-tier hierarchy (managed → user → project → local).
- ⚠️ Retry budget is a **process-global** package var, not reset per session.

### LLM adapters
- ✅ Direct Anthropic + OpenAI-compatible (7 providers), SSE, cumulative tokens, nag-reminder injection, retry (budget/backoff/Retry-After/classification), truncation surfacing.
- ⚠️ All implement ADK `model.LLM.GenerateContent` over `genai` types — the load-bearing coupling. Genkit only for Gemini/Claude-via-Genkit; direct adapters bypass it.
- 🔴 **Genkit dependency** for Gemini; dropping Genkit leaves ProviderGemini broken (reimplement via google generative-ai SDK directly).

---

## Cross-cutting: behavioral divergences baked into the current loop tail

These are not capability gaps — they are **wrong behaviors** the refactor must remove:

1. **Auto-commit on every turn** (`runner_exec.go:189-242`) — CC never auto-commits; commits are explicit user actions.
2. **Fixed "iroha" persona** + `GlobalMessageCount` seeded at 10 (`autonomous.go:135-146`) — CC has no fixed persona, no synthetic count.
3. **Global, exact-arg-only circuit breaker**, reset every `Execute` (`runner_confirmation.go:219-256`) — breaks teammate isolation; CC is per-tool, typed, time-windowed.
4. **go-build self-heal hardcoded to `./pkg/agent/...`** (`runner_confirmation.go:157`) — misreports outside this repo.
5. **Confirmation explain/edit flows spawn extra model calls** — CC permission is rule-based + user prompt only.
6. **Auto-review LLM judge pre-approves** medium-risk ops in `ModeAuto` — more permissive than CC's ask-human default (iroha extension; keep as opt-in, not default).

## Coupling summary (what must change for a native loop)

The ADK/Genkit coupling is concentrated in **8 files** (out of ~100 Go files):

| File | Coupling | Native replacement |
|---|---|---|
| `runner.go` | `llmagent.New` + `runner.New` + `session.InMemoryService` + `DynamicLLMDelegator` | native `AgentLoop` + `Session` |
| `runner_exec.go` | `adkRunner.Run` event iteration | native loop driver |
| `runner_confirmation.go` | `tool.Tool`/`tool.Context`/`req.Tools` map hijack | inline permission call before dispatch |
| `tools.go` | `functiontool.New` + `tool.Tool` | native `Tool` interface + struct-tag schema reflector |
| `mcp.go` | `DynamicMCPTool` impl of `adkRunnableTool` | native `Tool` adapter (transport stays) |
| `subagent.go` + `pool.go` | per-subagent `llmagent`+`runner`+`session` | native `AgentLoop` recursion |
| `pkg/llm/*` (3 adapters) | `model.LLM` + `genai` types | native `Model` interface + content-block types |
| `compaction.go` + `session_store.go` | `[]*genai.Content` + `session.Event` | native `Message`/`Event` |

**Everything else** (task/todo/cron/background/worktree/skills/plugin/team-inbox/memory/frontmatter/migrate/prompt-builder/permission-rules/hook-config/sandbox/MCP-client/MCP-transport/OAuth/config) is **framework-free** and ports with signature changes only.
