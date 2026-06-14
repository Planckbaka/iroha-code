# Claude Code Architecture — Master Spec & 1:1 Replica Plan

> Produced 2026-06-14. Research: 16-dimension deep-dive into real Claude Code (v2.1.x, mid-2026), sourced via anysearch against `docs.claude.com`, `code.claude.com`, `anthropic.com`, and `github.com/anthropics/*`. Audit: 6-area read-through of the current `iroha` (go-claude) codebase. Adversarial verification of 38 load-bearing claims (13/16 dimensions covered). Method: ultracode multi-agent workflow.

## How to read this

| Doc | What it is |
|-----|------------|
| **[gap-analysis.md](gap-analysis.md)** | iroha-current-state vs Claude Code, per cluster. The delta. |
| **[refactor-plan.md](refactor-plan.md)** | The phased plan to reach 1:1 fidelity. The decisions + roadmap. |
| **[research/](research/)** | 16 detailed Claude Code architecture specs (the reference implementers copy from). |
| **[audit/](audit/)** | 6 honest capability inventories of the current iroha code (+ ADK-coupling maps). |
| **[verify-verdicts.md](verify-verdicts.md)** | Adversarial fact-check results — confirmed / refuted / uncertain, with corrections. |

---

## Executive summary — the one finding that decides everything

**iroha has no native agent loop.** Its `Execute()` is a thin event-forwarder around Google ADK's internal `Flow.Run` (`for { runOneStep }`). Real Claude Code owns its loop — a single async generator `query()` → `queryLoop()` (~1,730 lines, one code path) that every caller (REPL, SDK, sub-agents, headless `-p`, compaction) funnels through.

The audit is unambiguous: **decoupling is not incremental** — *"the agent loop itself is outsourced to ADK, so a native refactor means replacing the loop driver, not just swapping types."*

This single fact reframes the whole project. The peripheral managers (task / todo / cron / background / worktree / skills / plugin / team-inbox / memory / session-JSON) are **~85% already framework-free** and port almost verbatim. The tool **handlers** are ~90% decoupling-ready (they only consume `context.Context` + a workdir key). The ADK coupling is concentrated in a small, well-defined core: `runner.go`, `tools.go` (registry), `mcp.go` (the `DynamicMCPTool` wrapper), `subagent.go`/`pool.go`, the 3 `pkg/llm` adapters, and the `genai` wire types.

**Therefore the project is a native engine rewrite with a large reusable periphery — not a greenfield rewrite, and not a patch.**

## Claude Code architecture at a glance (verified)

- **Agent loop** — one iteration = one model call: assemble context (system prompt + tool defs + history, **prompt-cached**) → stream response → if any `tool_use` blocks, execute and feed `tool_result` back as a `user` message → repeat. Yields to caller **only** on a tool-free response (`end_turn`) with no stop-hook continuation and no budget continuation. `max_turns` counts **only tool-use turns**. Read-only tools (Read/Glob/Grep/MCP `readOnlyHint`) run in parallel; stateful tools (Edit/Write/Bash) run sequentially.
- **5 SDK message types** — `SystemMessage` (`init` / `compact_boundary`), `AssistantMessage`, `UserMessage` (carries tool_result), `StreamEvent` (raw SSE, opt-in), `ResultMessage` (terminal; `success` / `error_max_turns` / `error_max_budget_usd`; carries `total_cost_usd`, `usage`, `num_turns`, `session_id`, `stop_reason`).
- **Streaming** — layered: Messages-API SSE (`message_start` → `content_block_*` → `message_delta` → `message_stop`) wrapped in SDK `StreamEvent`s; headless `--output-format stream-json` terminates with a top-level `type:"result"` event (**not** `message_stop`).
- **Session transcript** — append-only JSONL at `$CLAUDE_CONFIG_DIR/projects/<encoded-cwd>/<session-uuid>.jsonl`; each line has `uuid` + `parentUuid` (DAG/linked-list); compaction writes a `compact_boundary` (`parentUuid:null`, logicalParentUuid) followed by a user message with `isCompactSummary:true`.
- **Context/compaction** — API microcompact (`clear_tool_uses_20250919`: trigger 180k input tokens, target 40k) + `clear_thinking_20251015`; token-budget auto-continue (`COMPLETION_THRESHOLD=0.9`, `DIMINISHING_THRESHOLD=500`); real Anthropic token counting.
- **System prompt** — per-turn assembled array of blocks. **CLAUDE.md is NOT in the system prompt** — it is read and injected as a **user message** (project context); only the base agent prompt, tool descriptions, and env-info live in the system prompt (prompt-cached via `cache_control` breakpoints). *(Verified — this is the most commonly mis-stated fact.)*
- **Memory/CLAUDE.md** — `CLAUDE.md` cascade: managed (highest) → CLI args → local → project → user; `@import` expansion; the `#` memory quick-add; the `memory` tool writes typed `.md` files with an index.
- **Permissions** — 6 modes (default/acceptEdits/plan/bypassPermissions + auto/dontAsk); rules in `settings.json` `permissions.{allow,deny,ask}` evaluated **deny → ask → allow** (first match wins); Bash word-boundary glob gotcha (`Bash(ls *)` vs `Bash(ls*)`); path anchors differ per tool.
- **Hooks** — events: `PreToolUse` (uses `hookSpecificOutput.permissionDecision`, fires **before** permission-mode checks, can deny even in `bypassPermissions`), `PostToolUse`, `UserPromptSubmit`, `Stop`, `SubagentStop`, `SessionStart`, `SessionEnd`, `PreCompact`; command-hook stdin-JSON / stdout-JSON / exit-code protocol.
- **MCP** — 4 transports (stdio / SSE / streamable-HTTP / WebSocket); protocol **2025-06-18** (not iroha's pinned `2024-11-05`); tools namespaced `mcp__server__tool`; OAuth; `MAX_MCP_OUTPUT_TOKENS` default 25000 (warning at 10000); oversized results persist to disk with a file reference.
- **Subagents/Task** — single model-facing `Agent` tool (legacy alias `Task`); `.claude/agents/*.md` frontmatter (`name/description/tools/model`); parent receives **only the subagent's final message** as the tool_result (no intermediate calls); built-in `Explore`/`Plan` are one-shot (no `agentId`).
- **Skills** — `SKILL.md` with frontmatter; **progressive disclosure** (model decides when to expand the body); plugin namespace `plugin-name:skill-name`.
- **Slash commands + plan mode** — built-in + custom `.claude/commands/*.md` (`$ARGUMENTS`, `$1`, `!` bash, `@file`); `ExitPlanMode` presents 5 options (auto / acceptEdits / default / keep-planning / refine).
- **TUI** — **TypeScript React (Concurrent/Ink), not a Model/Update/View loop**; settings hierarchy: enterprise managed → user `~/.claude/settings.json` → project `.claude/settings.json` → local `settings.local.json`; IDE integration (VS Code/JetBrains).
- **Sandbox/security** — defense-in-depth: Bash sandbox (network/filesys/command deny), allow/deny patterns, macOS Seatbelt + Linux landlock/namespaces via a dedicated binary.

Full detail per dimension: see [`research/`](research/). Corrections from the verify pass: see [`verify-verdicts.md`](verify-verdicts.md).

## Current iroha state at a glance (audited)

~24,900 lines of non-test Go, 40+ tools, 7 LLM providers, 6 permission modes, 12 hook events, real OS-level sandbox (mac `sandbox-exec` / linux `bwrap`), durable task/cron/background/worktree/skills/memory stores, a hand-rolled TUI with differential renderer + glamour markdown. **Functionally broad; architecturally mis-aligned at the core.**

Capability status across audited areas: 91 implemented / 11 partial / 2 stub / 11 missing. The single `[missing]` that matters most: **the agent loop driver itself**.

## The decision (see refactor-plan.md for detail)

**Build a native `AgentLoop` that owns the model→tool→model iteration, with Anthropic-native content-block messages + a real tokenizer, decoupling from Google ADK/Genkit.** Reuse the ~85% framework-free periphery. Fix the behavioral divergences (auto-commit, fixed persona, global circuit breaker, orphaned HTTP/OAuth MCP, stale MCP protocol, forced-cheap subagents, etc.).

The plan is phased (Phase 0 foundation → Phase 4 verify) so the system stays buildable at each step.
