<!-- Generated: 2026-06-05 | Updated: 2026-06-05 -->

# iroha (go-claude)

## Purpose
An interactive, terminal-native AI coding agent CLI (binary: `iroha`). Bridges Google Genkit / Google ADK for multi-provider LLM orchestration (Gemini, Claude, OpenAI, DeepSeek, GLM) with Charm's Bubble Tea TUI framework for the user interface. Features human-in-the-loop permission approvals, hook pipelines, cross-session persistent memory, structured task tracking, team coordination, MCP plugin routing, and autonomous execution modes. Designed as a Claude Code-inspired agent for the terminal.

## Key Files
| File | Description |
|------|-------------|
| `go.mod` | Module `iroha`, Go 1.26.1, direct deps: Charm stack, Firebase Genkit, Google ADK/GenAI, UUID, yaml |
| `system_prompt.md` | Default system prompt template loaded by the agent at runtime |
| `.golangci.yml` | Linter config (errcheck, govet, revive, staticcheck) |
| `.goreleaser.yml` | GoReleaser build and release configuration |
| `install.sh` | One-line installer script (curl pipe sh) |
| `DESIGN.md` | Product design doc: brand, visual language, component inventory, interaction states |
| `README.md` | User-facing docs: features, quick start, slash commands, permission modes |
| `CONTRIBUTING.md` | Contribution guide and dev environment setup |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `cmd/` | CLI entry points (see `cmd/AGENTS.md`) |
| `pkg/` | Core library packages (see `pkg/AGENTS.md`) |
| `docs/` | Project documentation and roadmap (see `docs/AGENTS.md`) |
| `scratch/` | Debug scripts and experimental throwaway code (gitignored) |
| `test/` | Integration test suites (see `test/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Build: `go build -o iroha ./cmd/agent-cli`
- Test all: `go test ./...`
- Test specific packages: `go test ./pkg/tui/ ./pkg/llm/ ./pkg/agent/`
- Tidy modules: `go mod tidy`
- Binary output is `./iroha` at repo root
- User config stored at `~/.iroha.json` (outside repo)
- Project-local state in `./.iroha/` (gitignored)

### Testing Requirements
- Unit tests live alongside source (`*_test.go`)
- Key tested packages: `pkg/tui`, `pkg/llm`, `pkg/agent`
- Test gaps: `tools.go` / `tools_*.go` lack dedicated tests

### Common Patterns
- Standard Go layout: `cmd/` for binaries, `pkg/` for libraries
- Google ADK (`google.golang.org/adk`) as agent framework
- Firebase Genkit for Gemini/Claude SDK bridging and OpenTelemetry tracing
- Charm stack (Bubble Tea, Lipgloss, Glamour) for TUI
- Three-level permission model: Default (confirm), Plan (read-only), Auto (silent)
- Hook system: `PreToolUse`, `PostToolUse`, `SessionStart` lifecycle hooks

## Dependencies

### External
- `github.com/charmbracelet/lipgloss` v1.1.1 — Terminal styling
- `github.com/charmbracelet/glamour` v1.0.0 — Markdown rendering
- `github.com/charmbracelet/x/ansi` v0.11.6 — ANSI utilities
- `github.com/firebase/genkit/go` v1.8.0 — Firebase Genkit Go SDK (LLM orchestration)
- `github.com/google/uuid` v1.6.0 — UUID generation
- `google.golang.org/adk` v1.2.1 — Google Agent Development Kit
- `google.golang.org/genai` v1.57.0 — Generative AI types
- `golang.org/x/term` v0.43.0 — Terminal control
- `gopkg.in/yaml.v3` v3.0.1 — YAML parsing
