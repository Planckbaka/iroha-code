# Refactor Plan — Native Engine for 1:1 Claude Code Fidelity

## Architecture self-assessment (per CLAUDE.md directive)

**Verdict: underengineered at the core, well-engineered at the periphery.**

The current `iroha` is functionally broad (~24.9k lines, 40+ tools, 7 providers, real sandbox) but **architecturally hollow at the single most important seam**: the agent loop is outsourced to Google ADK. Everything that makes Claude Code *Claude Code* — the `query()`/`queryLoop()` generator, the SDK message taxonomy, Anthropic content-block messages, real token budgeting, stop-hook continuation, prompt-cache breakpoints, the CC transcript format — is either absent or approximated through a framework that was never designed to mirror it.

A 1:1 replica cannot reach fidelity by patching ADK's `Flow.Run`. The audit confirms decoupling is non-incremental. So the plan is a **native engine rewrite**, reusing the ~85% framework-free periphery, executed in phases so the binary stays green at each step.

This is *not* overengineering — overengineering would be greenfield-rewriting the framework-free managers (which already work) or inventing a second abstraction layer on top of ADK. This plan touches exactly the 8 coupled files + adds a small native core, and leaves the periphery intact.

## Core decisions

### Decision 1 — Native `AgentLoop`, decouple from Google ADK + Genkit
Replace the ADK-mediated loop with a native Go `AgentLoop` that owns the model→tool→model iteration. This is the load-bearing change; everything else follows.

**Native loop contract (from verified research):**
```go
// One iteration = one model call. Loop continues while the response contains
// any tool_use block; yields when tool-free (end_turn) and no stop-hook/budget
// continuation. max_turns counts ONLY tool-use turns.
type AgentLoop struct { session *Session; tools *Registry; model Model; perms *PermissionManager; hooks *HookManager; budget *Budget }

func (l *AgentLoop) Run(ctx, userInput) iter.Seq2[Event, error]  // yields the 5 SDK message types
```
- Read-only tools run concurrently; stateful tools sequentially.
- `yieldMissingToolResults` safety net on abort/fallback/error.
- Stop-hook continuation (`stopHookActive`).
- Token-budget auto-continue (`COMPLETION_THRESHOLD=0.9`, `DIMINISHING_THRESHOLD=500`); subagents always stop.

### Decision 2 — Anthropic-native message types + real tokenizer
The audit (A4) is explicit: the single biggest 1:1 blocker is `genai.Content` + `bytes/4` token heuristic vs CC's Anthropic content-blocks + real counting. Define native types:
```go
type Content struct { Role string; Blocks []Block }
type Block interface{ blockType() string }
type TextBlock struct { Text string }
type ToolUseBlock struct { ID, Name string; Input json.RawMessage }
type ToolResultBlock struct { ToolUseID string; Content []Block; IsError bool }
type ThinkingBlock struct { Text, Signature string }
```
- Provider adapters translate native ↔ wire (Anthropic direct, OpenAI-compatible).
- **Tokenizer**: add `tiktoken-go` (or Anthropic count_tokens endpoint) for real budgeting + the 180k/40k microcompact thresholds + the 92/95% auto-compact thresholds.

**Genai stays as an adapter-internal detail only** (or is dropped entirely once adapters speak native). Decision: drop `genai` from the loop; adapters own translation.

### Decision 3 — Native `Tool` interface + struct-tag schema reflector
Replace `tool.Tool`/`tool.Context`/`functiontool.New` with:
```go
type Tool interface {
    Name() string
    Description() string
    IsLongRunning() bool
    Declaration() *ToolSchema          // built from struct tags (iroha already uses `description:` tags everywhere)
    Run(ctx context.Context, args any) (Result, error)
}
type Registry struct{ ... }  // register/unregister, dispatch with permission+hooks inline
```
- Permission check becomes an inline call in `Registry.dispatch` **before** `Tool.Run` — removes the `req.Tools`-map hijack entirely.
- A generic `register[TArgs, TResults]` reflect-walks `TArgs` struct tags → `ToolSchema` with explicit `required` arrays (CC fidelity).
- `DynamicMCPTool` becomes a native `Tool` adapter; the MCP transport/client layer is already framework-free.

### Decision 4 — Remove the behavioral divergences (not optional for 1:1)
Delete/fix: auto-commit-on-turn, fixed persona + synthetic count, global circuit breaker (→ per-tool typed time-windowed), hardcoded `./pkg/agent/...` go-build, explain/edit extra model calls. Make the auto-review LLM judge **opt-in**, not the `ModeAuto` default.

## Phased roadmap

Each phase ends with `go build` + `go test` green. Phases are ordered so later phases depend on earlier primitives.

### Phase 0 — Foundation: native types + tokenizer (no behavior change yet)
**Goal:** introduce native message/LLM types alongside ADK, with a bridge so the existing loop still runs.
- `pkg/engine/message.go` — `Content`, `Block` union, `Event` union (5 SDK message types + stream deltas).
- `pkg/engine/tokenizer.go` — real tokenizer wrapper.
- `pkg/engine/llm.go` — native `Model` interface; port the 3 adapters' internals to native types (keep `model.LLM` shim delegating to native so the old loop still compiles).
- Provider adapters translate native ↔ Anthropic/OpenAI wire.
- **Exit criteria:** `go build` green; adapters round-trip native↔wire; tokenizer counts match a known fixture.

### Phase 1 — Core: native `AgentLoop` + `Tool` registry (the big one)
**Goal:** the loop is owned in-process; ADK runner retired for the main path.
- `pkg/engine/loop.go` — `AgentLoop.Run` (the `queryLoop` equivalent): assemble request → stream model → detect tool_use → dispatch via registry (permission + hooks + per-tool circuit breaker inline) → append tool_result → repeat; stop conditions + budget.
- `pkg/engine/tool.go` — native `Tool` interface + `Registry` + struct-tag schema reflector + `register[TArgs,TResults]`.
- Migrate `GetSWETools()` registrations to native `Tool` (handlers need only `context.Context` + workdir — already decoupling-ready).
- Replace `blockingConfirmationTool` hijack with inline permission in `Registry.dispatch`.
- Retire `runner.go`'s `adkRunner`/`llmagent`/`DynamicLLMDelegator`; `runner_exec.go` becomes a thin caller of `AgentLoop.Run` that forwards native `Event`s to the TUI bridge.
- **Exit criteria:** a real multi-turn tool-using session runs end-to-end on the native loop; `go test ./pkg/engine/...` + existing agent tests green; no `google.golang.org/adk/runner|llmagent|model|session` imports remain in non-shim code.

### Phase 2 — Trust boundary parity
**Goal:** CC-faithful permissions, hooks, MCP.
- Permissions: switch to `settings.json` 4-tier merge (managed→user→project→local); `permissions.{allow,deny,ask}` arrays; deny→ask→allow eval; gitignore-style matching; Bash word-boundary glob; path anchors per tool.
- Hooks: implement `PreToolUse` `hookSpecificOutput.permissionDecision` (allow/deny/ask/defer + `updatedInput`); fire before permission-mode checks (deny even in bypass); confirm `.claude/settings.json` hooks-block shape; keep `llm-prompt` as opt-in extension.
- MCP: **wire HTTP transport + OAuth** into the router (currently orphaned); bump protocol to **2025-06-18**; normalize tool results (content blocks / `is_error` / structured output); persist oversized results to disk (25k token default, 500k char ceiling); capture stderr.
- Sandbox: flip Seatbelt to **deny-by-default**; collapse the two overlapping path-escape checkers into one; add network policy.
- **Exit criteria:** permission/hooks/MCP parity spot-checks pass against CC docs examples.

### Phase 3 — Interface & orchestration parity
**Goal:** CC-faithful UX + extensibility.
- Memory/CLAUDE.md: inject CLAUDE.md as a **user message** (not system prompt); real `cache_control` breakpoints; `#` quick-add + `memory` tool; drop dream-consolidator to opt-in.
- Compaction: CC token-threshold API microcompact (`clear_tool_uses_20250919` 180k→40k, `clear_thinking_20251015`); restore-on-edit path; retire `[STICKY]` marker.
- Session transcript: adopt CC JSONL format (`uuid`+`parentUuid` DAG, `compact_boundary`, `isCompactSummary`, `toolUseResult`); faithful replay (tool cards).
- Subagents: parent's model (no forced downgrade); structured handoff + visible tool transcript; `run_in_background` + `TaskStop`; built-in `Explore`/`Plan` one-shot.
- Skills: **progressive disclosure** (model-driven body expansion); plugin namespace `plugin-name:skill-name`.
- Slash/plan: live `/model`; `EnterPlanMode`/`ExitPlanMode` with 5-option approval; `/trace` live timeline; wire Ctrl+Y; full custom-command parity.
- Config: move to CC `settings.json` hierarchy (keep `~/.iroha.json` as legacy migration source).
- TUI: decouple `OnEvent` from `session.Event` (native `AgentEvent`); per-session retry budget reset; respect configured `max_tokens`.
- **Exit criteria:** a resumed session round-trips with tool-card fidelity; plan mode + ExitPlanMode flow works; `/model` swaps live.

### Phase 4 — Verify
- `go build ./...` green; `go test ./...` green; `golangci-lint` 0 issues.
- Parity spot-checks: tool schemas vs CC docs; hook JSON I/O vs docs examples; transcript format vs a real CC session JSONL; permission rule precedence; headless `stream-json` end-to-end.
- Drop dead code (sanitizeADKStatePlaceholders, legacy chat render path, unused OAuth/HTTP if superseded).
- Optional: keep Gemini support by reimplementing via google generative-ai SDK (drop Genkit) — or document Gemini as unsupported.

## Suggested package layout (additive)
```
pkg/engine/        # NEW native core: message, event, tokenizer, model, loop, tool, registry, session, budget
pkg/agent/         # EXISTING — handlers/managers migrate to pkg/engine types; periphery stays
pkg/llm/           # adapters ported to native Model; genai becomes adapter-internal or removed
pkg/tui/           # OnEvent decoupled to native AgentEvent
pkg/config/        # settings.json 4-tier hierarchy
```

## Risk register
- **Risk:** Phase 1 is large; a half-migrated loop breaks everything. **Mitigation:** Phase 0 bridge keeps the old loop compiling; Phase 1 lands the native loop behind the same `Execute()`/`OnEvent` seam, swappable in one commit.
- **Risk:** Test suite assumes ADK `session.Event`/`model.LLM` shapes (68 test files). **Mitigation:** keep thin shims during migration; rewrite tests against native types as each file is touched.
- **Risk:** Genkit removal strands Gemini. **Mitigation:** Phase 0 ports Gemini to the google generative-ai SDK directly (no Genkit).
- **Risk:** Behavioral removals (auto-commit, persona) may be wanted by existing iroha users. **Mitigation:** keep them as config-gated opt-ins (`iroha.autoCommit`, `iroha.persona`), off by default for 1:1 fidelity.

## Effort signal (rough, for sequencing only)
Phase 0 ~2-3 days · Phase 1 ~5-8 days (the crux) · Phase 2 ~3-4 days · Phase 3 ~4-6 days · Phase 4 ~2 days. Ultracode mode: quality over speed; each phase gets its own implementation workflow + verification pass.

## What to do next
This plan is the blueprint. Recommended execution under ultracode: run a **Phase 0 implementation workflow** (native types + tokenizer + adapter port) as the first concrete step, verify build+tests, then proceed phase by phase — each phase its own workflow, each ending in a green build + parity check before the next begins.
