# pkg/llm — LLM Provider Adapters

Parent: [../../AGENTS.md](../../AGENTS.md)

## Purpose

Provides adapter implementations of the `model.LLM` interface (Google ADK) for multiple LLM providers. Each adapter translates the ADK's `LLMRequest`/`LLMResponse` types into provider-specific HTTP/SSE wire formats and back. The package also exposes a factory function (`NewAdapter`) that routes to the correct adapter based on provider type and optional API format override.

## Key Files

| File | Description |
|------|-------------|
| `adapter.go` | Factory function `NewAdapter`, provider enums (`ProviderType`, `APIFormat`), interfaces (`AdapterHooks`, `TokenTracker`, `SystemPromptUpdater`). Routes provider+format to concrete adapter. |
| `anthropic.go` | `AnthropicAdapter` — direct HTTP/SSE client for the Anthropic Messages API. Handles streaming text, tool use (function calls), prompt caching, and retry with budget. |
| `openai.go` | `OpenAICompatibleAdapter` — HTTP/SSE client for OpenAI-compatible APIs (GLM-4, DeepSeek, Kimi, SiliconFlow). Supports streaming text, multi-tool-call accumulation by index, and exponential backoff with jitter. |
| `genkit_adapter.go` | `GenkitModelAdapter` — bridges Firebase Genkit Go SDK into ADK `model.LLM`. Used for Claude and Gemini when a Genkit instance is available. Supports both streaming and non-streaming generation. |
| `helpers.go` | `CollectNonStreaming` helper that drains a streaming `model.LLM` iterator into a single concatenated string. |
| `retry.go` | Session-level retry budget (10 retries/session). Provides `ConsumeRetry`, `ResetRetryBudget`, `RetryBudgetStatus`, `parseRetryAfter`, and `budgetExhaustedError`. |
| `debuglog.go` | Timestamped debug log file at `/tmp/iroha-debug.log`. `InitDebugLog` opens the file; `DebugLog` appends formatted lines; `DumpDebugFile` writes raw byte dumps. |
| `anthropic_test.go` | Tests for Anthropic adapter: text streaming, tool use, error handling, API key validation, HTTP errors, message conversion (genai to Anthropic format). Uses `httptest` mock servers. |
| `openai_test.go` | Tests for OpenAI adapter: text streaming, multi-tool-call, transient failure retry (429 to success), missing API key, fatal HTTP errors, message JSON conversion. |
| `glm_test.go` | Placeholder test file (empty). |
| `retry_test.go` | Tests for retry budget: consume, exhaust, reset, status queries. |

## For AI Agents

- **Adding a new provider**: Create a new adapter struct implementing `model.LLM` (with `GenerateContent` returning `iter.Seq2[*model.LLMResponse, error]`), add a new `ProviderType` constant in `adapter.go`, and add a routing case in `NewAdapter`.
- **All adapters share a pattern**: constructor accepting `(modelName, apiKey, baseURL, systemPrompt, hooks)`, thread-safe `SetSystemPrompt` via `sync.RWMutex`, cumulative token tracking via `AddTokens`/`CumulativeTokens`, and `AdapterHooks` integration (`NoteRound`, `NagReminder`).
- **Retry logic**: Both HTTP adapters use the shared `retry.go` session budget. Call `ConsumeRetry()` before each retry attempt. Transient errors (429, 5xx) trigger retry; non-transient errors surface immediately.
- **Testing pattern**: Use `net/http/httptest` servers returning SSE streams. Iterate `adapter.GenerateContent()` and collect text/tool-call results.
- **Genkit vs direct**: Gemini always goes through Genkit. Claude routes through Genkit if a non-nil `*genkit.Genkit` is provided, otherwise falls back to the direct Anthropic HTTP adapter. All other providers use direct HTTP adapters.

## Dependencies

- `google.golang.org/adk/model` — `model.LLM`, `model.LLMRequest`, `model.LLMResponse` interfaces
- `google.golang.org/genai` — `genai.Content`, `genai.Part`, `genai.FunctionCall`, `genai.FunctionResponse`, `genai.GenerateContentConfig`
- `github.com/firebase/genkit` — Genkit SDK (only for `GenkitModelAdapter`)
- Standard library: `net/http`, `encoding/json`, `bufio`, `iter`, `sync`, `time`, `math`, `math/rand`

_Updated: 2026-06-05_
