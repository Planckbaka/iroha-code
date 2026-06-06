<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-06-05 | Updated: 2026-06-05 -->

# pkg

## Purpose
Core packages for the go-claude application, providing the agent runtime, LLM adapters, terminal UI, and configuration layer.

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `agent/` | Agent runtime: execution loop, 30+ tool dispatchers, git/LLM/MCP/subagent integration, session management, hooks, permissions, memory, task DAG, team orchestration, and background tasks (see `agent/AGENTS.md`) |
| `config/` | Application configuration loading, provider defaults, cost estimation, and interactive setup wizard (see `config/AGENTS.md`) |
| `llm/` | LLM provider adapters (OpenAI-compatible, Anthropic, Genkit/GLM) with retry logic and debug logging (see `llm/AGENTS.md`) |
| `tui/` | Bubble Tea terminal UI: chat view, input handling, status display, slash commands, theming, rendering, and doctor diagnostics (see `tui/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- All packages follow standard Go conventions with `package` declarations matching directory names
- Packages communicate via exported interfaces and global singletons (`Global*`)
- Test files are colocated with source (`*_test.go`); run per-package with `go test ./pkg/<dir>/...`
- Config path: `~/.iroha/` (auto-migrates from legacy `~/.go-claude/`)

### Testing Requirements
- `go test ./pkg/...` runs all tests
- Each package is independently testable
- ~80+ source files and 23+ test files across the four packages

### Common Patterns
- Global singletons: `GlobalPermissionManager`, `GlobalHookManager`, `GlobalMemoryManager`, `GlobalTodoManager`, `GlobalTaskManager`, `GlobalBackgroundManager`, `GlobalCronScheduler`, `GlobalTeamManager`, `GlobalProtocolManager`, `GlobalAutonomyManager`, `GlobalWorktreeManager`, `GlobalMCPRouter`
- Channel-based bridges for async TUI ↔ Agent communication (`ConfirmationBridge`, `ToolStatusBridge`)
- Mutex-protected concurrent access (`sync.RWMutex`)

## Dependencies

### Internal
- Dependency flow: `tui/` → `agent/` → `llm/` + `config/`
- `agent` is the central orchestrator, importing all other packages
- `cmd/agent-cli` → all `pkg/` packages
