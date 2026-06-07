<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-06-05 | Updated: 2026-06-05 -->

# cmd

## Purpose
Application entry points. Each subdirectory is a standalone binary with its own `main()` function.

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `agent-cli/` | Primary CLI binary — flag parsing, config resolution, runner init, TUI launch (see `agent-cli/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Each subdirectory produces one binary via `go build -o iroha ./cmd/agent-cli`
- Keep `main.go` files thin — delegate to `pkg/` packages
- The main.go in agent-cli is ~214 lines, all orchestration logic is in `pkg/`

### Testing Requirements
- No unit tests for entry points; tested via integration/manual testing
- Build verification: `go build -o /dev/null ./cmd/agent-cli`

### Common Patterns
- Flag parsing for provider, model, API key, base URL, API format, session, permission mode
- Config file resolution with CLI flag > env var > config file > wizard priority chain
- Teammate mode for multi-agent IPC via `--teammate` and `--socket` flags
