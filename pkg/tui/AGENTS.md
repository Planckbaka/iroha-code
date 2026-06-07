# pkg/tui — Terminal User Interface

Parent: [../../AGENTS.md](../../AGENTS.md)

## Purpose

Implements the full terminal UI for the Iroha Code agent. Built on a custom retained-mode component architecture (not bubbletea's Elm-style update/model). The `App` struct orchestrates all components, dispatches key events, and composes their rendered output into a single terminal frame. Rendering uses `lipgloss` for styling, `glamour` for Markdown rendering, and a custom `RawRenderer` for flicker-free differential screen updates.

## Key Files

| File | Description |
|------|-------------|
| `app.go` | `App` — the top-level orchestrator. Creates and wires all components, runs the main event loop in `RunApp`, handles event dispatch (`HandleEvent`), composes the full screen layout in `Render`, and manages agent execution lifecycle (`executePrompt`, `finalizeTurn`, `handleToolStatus`). |
| `component.go` | `Component` interface (`Render`, `HandleInput`, `Active`, `OnStateChange`) and `BaseComponent` struct. Foundation for all UI components. |
| `component_chat.go` | `ChatComponent` — renders conversation history, streaming text, thinking indicator, and tool activity. Delegates history to `HistoryStore`. |
| `component_confirm.go` | `ConfirmComponent` — human-in-the-loop confirmation flow with 5 options (Allow/Deny/Always/Edit/Explain). Has its own edit buffer for argument modification. Supports diff viewing toggle. |
| `component_input.go` | `InputComponent` — manages the user input buffer, cursor movement, text editing, history navigation, and slash menu integration. Fires `OnSubmit` and `OnSlashCmd` callbacks. |
| `component_status.go` | `StatusBarComponent` — renders the bottom status bar with permission mode, token count, cost estimate, active tool, elapsed time, and goal mode indicator. |
| `component_slash_menu.go` | `SlashMenuComponent` — filters and renders slash command autocomplete popup. Manages command list filtering by input prefix, navigation (up/down), and selection. Embeds `BaseComponent`. |
| `component_screens.go` | `ScreenComponent` — full-screen overlay for permission mode selection and session history picker. Renders permission modes with descriptions and session list with metadata. Uses callbacks (`OnPermSelect`, `OnSessionSelect`, `OnNewSession`). |
| `model.go` | `SlashMenuItem` struct and `AllSlashCommands` — master list of all slash commands with descriptions. |
| `update_msgs.go` | `TuiState` enum (6 states: Prompt, Thinking, Streaming, Confirming, PermissionSelect, SessionSelect). Custom message types: `StreamTextMsg`, `ConfirmationRequiredMsg`, `ToolStatusMsg`, `AgentErrorMsg`, `AgentDoneMsg`. |
| `view.go` | Rendering helpers: `RenderMarkdownWithWidth` (Glamour-based Markdown to ANSI), `RenderConfirmCard`/`RenderConfirmCardWithDiff`, `RenderWelcomeCard`, `RenderSlashMenu`, `FormatToolActivity` (maps tool name+args to human-readable description), `RenderToolSuccessCard`/`RenderToolErrorCard`, `RenderHelpDashboard`, `RenderErrorCard`, dashboards (todo, task, team, worktree, MCP, background), `RenderShellStreamArea`, `RenderFrustrationPauseCard`. |
| `styles.go` | Color palette (6 named colors) and `lipgloss.Style` variables for all UI elements (Prompt, UserMsg, AgentMsg, ToolSuccess, ToolError, Thinking, ConfirmCard, StatusBar, etc.). Braille spinner animation. |
| `history.go` | `HistoryStore` — structured conversation history with viewport rendering, scroll support (PageUp/PageDown, mouse wheel), render caching, and search. `HistoryEntry` has Role/Content/TS/Tokens/Metadata. |
| `input.go` | `HistoryManager` — simple input history for up/down arrow navigation in the prompt. |
| `focus.go` | `FocusModel` — input buffer ownership between components (FocusNone, FocusPrompt, FocusConfirmEdit). Manages shared `Buffer []rune` and `CursorIndex`. Provides `Take`, `Release`, and `Is` methods. |
| `raw_input.go` | `ReadRawKeys` — raw terminal input loop. Parses ANSI escape sequences into `Key` structs (arrows, Tab, Shift+Tab, PgUp/PgDn, mouse wheel, Alt+Enter, Ctrl+C/D/Y). UTF-8 aware. |
| `renderer.go` | `RawRenderer` — flicker-free differential terminal redraw. Uses Synchronized Output (`\x1b[?2026h`) to prevent tearing. Finds first differing line and overwrites only changed content. Positions hardware cursor for IME alignment. Manages `oldLines` buffer for diff comparison. |
| `interfaces.go` | `AgentRunner` interface (`Execute`, `ModelName`, `GetTokenUsage`) and `BridgeResponder` interface (`Send`) for testability and decoupling from concrete agent implementation. |
| `doctor.go` | `RunDiagnostics` — environmental health check (config audit, network latency, git status, toolchain validation, system metrics). Returns styled dashboard. |
| `wrap.go` | `WordWrap` (ANSI-aware, uses `xansi.Hardwrap`) and `WrapInput` (wraps input with prompt prefix offset). |
| `component_test.go` | Interface compliance tests for all components. Unit tests for ChatComponent, InputComponent, ConfirmComponent (including edit mode), SlashMenuComponent, StatusBarComponent, ScreenComponent, and word wrap. |
| `tui_test.go` | Integration-level tests: confirm card rendering, Markdown rendering, welcome card, tool error/success cards, help/cancel cards, slash command stats, renderer flicker-free, tool stream accumulation, finalize turn, Ctrl+C, viewport height clipping, slash menu clipping. |
| `history_test.go` | Tests for HistoryStore: add, timestamp, scroll preservation, render, raw markdown storage, scroll up/down/clamp, tail anchoring, search, cache invalidation, entry bounds. |
| `focus_test.go` | Tests for FocusModel: Take, Release, Is, buffer management. |
| `raw_input_test.go` | Tests for SGR mouse wheel parsing and non-wheel mouse event consumption. |

## For AI Agents

### Working In This Directory

- **Component pattern**: All components implement the `Component` interface (`Render`, `HandleInput`, `Active`, `OnStateChange`). Components communicate with `App` through callback fields (e.g., `OnSubmit`, `OnRespond`), not direct references. State transitions are propagated via `OnStateChange(oldState, newState)`.
- **Focus management**: `FocusModel` in `focus.go` manages input buffer ownership. Only one component (Prompt or ConfirmEdit) owns the buffer at a time. `Take`/`Release` methods ensure clean transitions.
- **State machine**: The TUI has 6 states. Input dispatch follows priority order: Confirm, Input, Slash, Screens. Chat and StatusBar are always visible but never handle input.
- **Event loop**: `RunApp` in `app.go` is the main entry point. Events flow through a buffered channel: keyboard input, bridge channels (prompts, tool status), ticker (spinner), and startup prompt.
- **Rendering pipeline**: `App.Render()` composes: [dashboards] [history+tail] [separator] [slash menu] [input] [status bar]. `RawRenderer.Draw()` performs differential redraw by finding the first changed line and rewriting from there. Viewport clipping uses `HistoryStore.RenderWithTail` to fit within `height - chrome`.
- **Cursor tracking**: `cursorRow`/`cursorCol` are computed during `Render()` for hardware cursor positioning. The renderer uses these for IME candidate window alignment.
- **Testing interfaces**: `AgentRunner` and `BridgeResponder` in `interfaces.go` decouple the TUI from concrete agent implementations, enabling nil runners in tests.

### Testing Requirements

- Component interface compliance is verified with `var _ Component = (*XxxComponent)(nil)`.
- Use `httptest` or mock adapters; the `AgentRunner` interface in `interfaces.go` allows nil runners in tests.
- Test files cover: component behavior, rendering output, key handling, state transitions, focus management, history scroll, and differential rendering.
- All tests run with `go test ./pkg/tui/...`.

### Common Patterns

- **Adding a new slash command**: Add entry to `AllSlashCommands` in `model.go`, add a `case` in `handleRawSlashCommand` in `app.go`.
- **Adding a new UI state**: Add constant to `TuiState` enum in `update_msgs.go`, update `String()`, add state checks in relevant component `Active()` methods.
- **Adding a new dashboard**: Create a `RenderXxxDashboard()` function in `view.go`, call it from `App.Render()` in `app.go`.
- **Adding a new component**: Implement the `Component` interface, embed `BaseComponent`, wire callbacks in `App` constructor, add to the render composition in `App.Render()`.

### Dependencies

- `github.com/charmbracelet/lipgloss` — terminal styling and layout
- `github.com/charmbracelet/glamour` — Markdown to ANSI rendering
- `github.com/charmbracelet/x/ansi` — ANSI string width/stripping utilities
- `github.com/muesli/termenv` — terminal color profile detection
- `golang.org/x/term` — raw terminal mode
- `iroha/pkg/agent` — agent runner, permission manager, tool status, session service, bridge channels
- `iroha/pkg/config` — cost estimation, config loading
- `google.golang.org/adk/session` — session event types

_Updated: 2026-06-05_
