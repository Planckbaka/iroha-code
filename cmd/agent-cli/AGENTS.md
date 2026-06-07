<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-06-05 | Updated: 2026-06-05 -->

# agent-cli

## Purpose
Primary CLI entry point. Resolves configuration (flags > env vars > config file > wizard), initializes the agent runner with LLM adapter and tools, and launches the Bubble Tea TUI. Also supports teammate mode for multi-agent IPC.

## Key Files
| File | Description |
|------|-------------|
| `main.go` | Binary entry point — flag parsing, config resolution, session management, runner init, TUI launch (~214 lines) |

## For AI Agents

### Working In This Directory
- This is the only file that ties all `pkg/` packages together
- Config priority: CLI flags > environment variables > `~/.iroha.json` > interactive wizard > provider defaults
- Two runtime modes: (1) normal TUI mode, (2) teammate mode via `--teammate <name> --socket <path>` for child agent IPC
- Session management flags: `--resume` (picker), `--last` (auto-resume recent), `--session <id>`, `--fork <id>`
- Permission mode flags: `--yes`/`-y` (auto), `--plan`/`-p` (read-only), `--default`/`-d` (ask)
- Trailing CLI args are joined as a startup prompt sent to the agent immediately
- Supported providers: gemini, claude, openai, glm, deepseek, kimi, siliconflow

### Testing Requirements
- No unit tests here; tested via integration/manual testing

## Dependencies

### Internal
- `iroha/pkg/agent` — Runner creation, session service, teammate mode, permission modes
- `iroha/pkg/config` — Config loading and interactive wizard
- `iroha/pkg/llm` — Provider type constants, API format enum
- `iroha/pkg/tui` — TUI model and program (Bubble Tea)

### External
- `github.com/google/uuid` — Session ID generation
