<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-23 | Updated: 2026-06-03 -->

# tui

## Purpose
Terminal UI with retained-mode component architecture: prompt input, streaming output, human-in-the-loop confirmation, slash commands, session picker, permission selection, viewport scrolling, markdown rendering, and diagnostic dashboard.

## Architecture
Component-based retained-mode TUI (Pi-tui inspired):
- **Component interface**: `Render(width) []string`, `HandleInput(Key) bool`, `Active(state) bool`, `OnStateChange(old, new)`
- **App orchestrator**: Wires components via callbacks, dispatches events, collects renders
- **Callback communication**: Components declare callback fields (`OnSubmit`, `OnRespond`), App wires in constructor
- **FocusModel**: Explicit input buffer ownership (FocusNone/FocusPrompt/FocusConfirmEdit)
- **HistoryStore**: Structured `[]HistoryEntry` with viewport rendering, caching, scrolling, search
- **RawRenderer**: Differential ANSI redraw (unchanged)

## Key Files
| File | Description |
|------|-------------|
| `app.go` | `App` — orchestrator with all components, event dispatch, render collection, `RunApp` entry point |
| `component.go` | `Component` interface, `BaseComponent`, `TuiState` enum |
| `component_chat.go` | `ChatComponent` — history + streaming + thinking + tool activity |
| `component_input.go` | `InputComponent` — buffer, cursor, history nav, slash menu integration |
| `component_confirm.go` | `ConfirmComponent` — own edit buffer, Y/N/Always/Edit/Explain, diff toggle |
| `component_status.go` | `StatusBarComponent` — mode, tokens, cost, active tool |
| `component_slash_menu.go` | `SlashMenuComponent` — filters and renders slash commands |
| `component_screens.go` | `ScreenComponent` — permission and session selection |
| `focus.go` | `FocusModel` — input ownership tracking |
| `history.go` | `HistoryStore` — structured entries with viewport render, cache, scroll, search |
| `interfaces.go` | `AgentRunner`, `BridgeResponder` interfaces |
| `wrap.go` | `WordWrap` — ANSI-aware word wrapping using visual width |
| `model.go` | `Model` — **DEPRECATED**: legacy monolithic state machine, replaced by App |
| `update_keys.go` | Legacy key handler for Model |
| `update_msgs.go` | Legacy `RunRawTUI` event loop (superseded by `RunApp`) |
| `view.go` | Rendering functions: `RenderMarkdown`, `RenderConfirmCard`, `RenderWelcomeCard`, dashboards |
| `styles.go` | Lipgloss color palette (cyber-holographic) and style definitions |
| `raw_input.go` | Raw terminal keyboard reader with UTF-8 support |
| `renderer.go` | `RawRenderer` — differential ANSI redraw |
| `component_test.go` | Component interface compliance and behavior tests |
| `focus_test.go` | FocusModel unit tests |
| `history_test.go` | HistoryStore unit tests (add, scroll, search, viewport) |

## For AI Agents

### Working In This Directory
- State machine: `statePrompt` -> `stateThinking` -> `stateStreaming` -> back to `statePrompt`
- `stateConfirming` interrupts streaming for tool approval (y/n/always/edit/explain)
- `statePermissionSelect` / `stateSessionSelect` for full-screen selection overlays
- Slash commands (20): `/permission`, `/rules`, `/hooks`, `/memory`, `/prompt`, `/sections`, `/task`, `/team`, `/worktree`, `/mcp`, `/bg`, `/skill`, `/trace`, `/stats`, `/sessions`, `/resume`, `/help`, `/commands`, `/doctor`, `/exit`
- `RunApp` is the new entry point (uses App); `RunRawTUI` is the legacy path (uses Model)
- `ConfirmationRequiredMsg` received from `agent.Bridge.PromptChan`
- Viewport scrolling via PageUp/PageDown (HistoryStore.PageUp/PageDown)

### Testing Requirements
- `go test ./pkg/tui/...`
- Tests for FocusModel, HistoryStore, Component interface compliance
- New component code targets >=80% coverage

### Common Patterns
- Custom message types: `StreamTextMsg`, `ConfirmationRequiredMsg`, `ToolStatusMsg`, `AgentErrorMsg`, `AgentDoneMsg`, `StartupPromptMsg`
- Bridge channels: `agent.Bridge.PromptChan` (confirmation), `agent.ToolBridge.StatusChan` (tool status)
- ANSI codes replaced with Lipgloss styles (no raw `\x1b[` escapes)

## Dependencies

### Internal
- `pkg/agent` — `CustomRunner`, `Bridge`, `ToolBridge`, `GlobalPermissionManager`, `GlobalSessionService`

### External
- `github.com/charmbracelet/lipgloss` — Terminal styling
- `github.com/charmbracelet/glamour` — ANSI markdown rendering
- `google.golang.org/adk/session` — Event type

<!-- MANUAL: -->
