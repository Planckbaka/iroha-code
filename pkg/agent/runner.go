package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"strings"
	"sync"
	"time"

	"iroha/pkg/llm"

	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/anthropic"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// runnerHooks implements llm.AdapterHooks using an injected TodoManager.
type runnerHooks struct {
	todo *TodoManager
}

func (h runnerHooks) NagReminder() string {
	if h.todo == nil {
		return ""
	}
	if h.todo.RoundsSinceUpdate() >= 3 {
		return "📌 [System] To ensure continuity of subsequent code changes, please update your todo plan progress before executing the current step."
	}
	return ""
}

func (h runnerHooks) NoteRound() {
	if h.todo != nil {
		h.todo.NoteRoundWithoutUpdate()
	}
}

func buildSystemPrompt() string {
	builder := NewSystemPromptBuilder()
	return builder.Build()
}

// GlobalSessionService is the persistent session store wrapper singleton.
var GlobalSessionService *PersistentSessionService

// globalLLMModel is the current LLM model adapter for dynamic prompts/explanations.
var globalLLMModel model.LLM

// DynamicLLMDelegator is a thread-safe delegator that allows changing the active model at runtime.
type DynamicLLMDelegator struct {
	mu           sync.RWMutex
	currentModel model.LLM
}

func (d *DynamicLLMDelegator) Name() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentModel.Name()
}

// compactionTriggerTokens is the estimated-token threshold above which the
// delegator runs CompactContents before delegating to the underlying model.
// Mirrors the s06 "auto-compact" lever (~50k tokens of active context).
const compactionTriggerTokens = 50000

// estimateContentsTokens returns a rough token estimate for a slice of Contents
// by summing text/JSON-arg byte length and dividing by 4.
func estimateContentsTokens(contents []*genai.Content) int {
	total := 0
	for _, c := range contents {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			total += len(p.Text)
			if p.FunctionCall != nil {
				if b, err := json.Marshal(p.FunctionCall.Args); err == nil {
					total += len(b)
				}
			}
			if p.FunctionResponse != nil {
				if b, err := json.Marshal(p.FunctionResponse.Response); err == nil {
					total += len(b)
				}
			}
		}
	}
	return estimateTokens(total)
}

func (d *DynamicLLMDelegator) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	d.mu.RLock()
	m := d.currentModel
	d.mu.RUnlock()

	// s10 System Prompt: the dynamic pipeline only takes effect if the prompt is
	// rebuilt each turn and re-pushed to the adapter. We update the live message
	// count first (drives <identity> re-injection) then assemble a fresh prompt
	// so time/tasks/teammates/inbox/safety/memory all reflect current state.
	if req != nil {
		GlobalMessageCount = len(req.Contents)
		if updater, ok := m.(llm.SystemPromptUpdater); ok {
			builder := NewSystemPromptBuilder()
			userPrompt := latestUserText(req.Contents)
			updater.SetSystemPrompt(builder.BuildWithPrompt(userPrompt))
		}
	}

	// s06 Context Compact: relocate detail out of the active window before it
	// overflows. Gated on a token estimate so small turns skip the deep copy.
	// The underlying model `m` (not the delegator) is passed for summarization
	// to avoid re-entering compaction recursively.
	if req != nil && len(req.Contents) > 0 {
		if len(req.Contents) > 12 || estimateContentsTokens(req.Contents) > compactionTriggerTokens {
			sessionID := GlobalLogger.CurrentSessionID()
			req.Contents = CompactContents(req.Contents, sessionID, m)
		}
	}

	// s11 Error Recovery: a "prompt too long" / context-length-exceeded error
	// surfaces from the provider BEFORE any content streams, so it is safe to
	// react by force-compacting the window and retrying once. Mid-stream errors
	// are NOT retried here — replaying would duplicate already-emitted text.
	return d.generateWithRetryRecovery(ctx, req, stream, m)
}

// generateWithRetryRecovery wraps the underlying model's response stream and
// retries safe pre-output failures. Context-length errors still get one
// force-compaction attempt; transient direct-HTTP errors use the shared API
// retry budget and Claude Code-like user-visible retry notices. Any error after
// output is passed through to avoid duplicating streamed text or tool calls.
func (d *DynamicLLMDelegator) generateWithRetryRecovery(ctx context.Context, req *model.LLMRequest, stream bool, m model.LLM) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		retryableDirectHTTP := false
		if _, ok := m.(llm.DirectHTTPAdapter); ok {
			retryableDirectHTTP = true
		}
		if !retryableDirectHTTP {
			for resp, err := range d.generateWithContextRecovery(ctx, req, stream, m) {
				if !yield(resp, err) {
					return
				}
			}
			return
		}

		maxRetries := llm.MaxRetries()
		for attempt := 0; ; attempt++ {
			emitted := false
			retried := false
			for resp, err := range d.generateWithContextRecovery(ctx, req, stream, m) {
				if err != nil {
					if !emitted && llm.IsRetryableTemporaryError(err) && attempt < maxRetries {
						if !llm.ConsumeRetry() {
							yield(nil, llm.BudgetExhaustedError(m.Name(), err))
							return
						}
						nextAttempt := attempt + 1
						delay := llm.RetryDelay(nextAttempt, nil)
						if !yield(llm.RetryNotice(err.Error(), nextAttempt, maxRetries, delay), nil) {
							return
						}
						select {
						case <-ctx.Done():
							yield(nil, ctx.Err())
							return
						case <-time.After(delay):
						}
						retried = true
						break
					}
					yield(resp, err)
					return
				}
				if responseHasOutput(resp) {
					emitted = true
				}
				if !yield(resp, nil) {
					return
				}
			}
			if !retried {
				return
			}
		}
	}
}

// generateWithContextRecovery wraps the underlying model's response stream and,
// if the very first item is a context-length error (no content emitted yet),
// force-compacts the request once and retries. Any later error is passed
// through untouched to avoid duplicating streamed output.
func (d *DynamicLLMDelegator) generateWithContextRecovery(ctx context.Context, req *model.LLMRequest, stream bool, m model.LLM) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		emitted := false
		for resp, err := range m.GenerateContent(ctx, req, stream) {
			if err != nil && !emitted && req != nil && isContextLengthError(err) {
				// Force-compact regardless of size gate, then retry once.
				sessionID := GlobalLogger.CurrentSessionID()
				req.Contents = CompactContents(req.Contents, sessionID, m)
				for resp2, err2 := range m.GenerateContent(ctx, req, stream) {
					if !yield(resp2, err2) {
						return
					}
				}
				return
			}
			if responseHasOutput(resp) {
				emitted = true
			}
			if !yield(resp, err) {
				return
			}
		}
	}
}

func responseHasOutput(resp *model.LLMResponse) bool {
	if resp == nil || resp.Content == nil {
		return false
	}
	for _, p := range resp.Content.Parts {
		if p == nil {
			continue
		}
		if p.Text != "" || p.FunctionCall != nil {
			return true
		}
	}
	return false
}

// isContextLengthError reports whether an error from a provider indicates the
// request exceeded the model's context window (vs. a transient/auth error).
func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "prompt is too long"),
		strings.Contains(msg, "context length"),
		strings.Contains(msg, "context_length_exceeded"),
		strings.Contains(msg, "maximum context"),
		strings.Contains(msg, "too many tokens"),
		strings.Contains(msg, "reduce the length"):
		return true
	}
	return false
}

// latestUserText returns the text of the most recent user message, used for
// skill trigger-matching when rebuilding the system prompt.
func latestUserText(contents []*genai.Content) string {
	for i := len(contents) - 1; i >= 0; i-- {
		c := contents[i]
		if c == nil || c.Role != "user" {
			continue
		}
		for _, p := range c.Parts {
			if p != nil && p.Text != "" {
				return p.Text
			}
		}
	}
	return ""
}

func (d *DynamicLLMDelegator) SetModel(m model.LLM) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentModel = m
}

func (d *DynamicLLMDelegator) CumulativeTokens() int {
	d.mu.RLock()
	m := d.currentModel
	d.mu.RUnlock()
	if tt, ok := m.(llm.TokenTracker); ok {
		return tt.CumulativeTokens()
	}
	return 0
}

func (d *DynamicLLMDelegator) AddTokens(n int) {
	d.mu.Lock()
	m := d.currentModel
	d.mu.Unlock()
	if tt, ok := m.(llm.TokenTracker); ok {
		tt.AddTokens(n)
	}
}

// RunnerDeps holds all manager dependencies injected into CustomRunner.
// Replaces direct global access within runner methods.
type RunnerDeps struct {
	TodoManager        *TodoManager
	MemoryManager      *MemoryManager
	SessionService     *PersistentSessionService
	BackgroundManager  *BackgroundManager
	CronScheduler      *CronScheduler
	HookManager        *HookManager
	Logger             *LoggerManager
	ToolCircuitBreaker *ToolCircuitBreaker
	AgentPool          *AgentPool
	TeamManager        *TeamManager
	DreamConsolidator  *DreamConsolidator
	AutoReviewConfig   *autoReviewConfig
	PermissionManager  *PermissionManager
	TaskManager        *TaskManager
	MCPRouter          *MCPToolRouter
	Bridge             *ConfirmationBridge
}

// CustomRunner wraps ADK runner and manages background execution
type CustomRunner struct {
	mu              sync.RWMutex
	adkRunner       *runner.Runner
	llmModel        model.LLM
	delegator       *DynamicLLMDelegator
	Provider        llm.ProviderType
	ActiveModelName string
	APIKey          string
	BaseURL         string
	APIFormat       llm.APIFormat
	GenkitRegistry  *genkit.Genkit
	deps            RunnerDeps
}

// initGenkit creates a Genkit registry with the appropriate provider plugin.
// Returns nil for providers that use the direct adapter (e.g. OpenAI-compatible).
func initGenkit(provider llm.ProviderType, apiKey, baseURL string) *genkit.Genkit {
	switch provider {
	case llm.ProviderGemini, llm.ProviderClaude:
		ctx := context.Background()
		var plugins []api.Plugin
		switch provider {
		case llm.ProviderGemini:
			plugins = append(plugins, &googlegenai.GoogleAI{APIKey: apiKey})
		case llm.ProviderClaude:
			plugins = append(plugins, &anthropic.Anthropic{APIKey: apiKey, BaseURL: baseURL})
		}
		return genkit.Init(ctx, genkit.WithPlugins(plugins...))
	}
	return nil
}

func NewCustomRunner(provider llm.ProviderType, modelName string, apiKey string, baseURL string, apiFormat llm.APIFormat) (*CustomRunner, error) {
	// 1. Initialize Genkit registry (nil for OpenAI-compatible providers)
	g := initGenkit(provider, apiKey, baseURL)

	// 2. Create our abstract model adapter
	systemPrompt := buildSystemPrompt()
	modelAdapter, err := llm.NewAdapter(g, provider, modelName, apiKey, baseURL, systemPrompt, apiFormat, runnerHooks{todo: GlobalTodoManager})
	if err != nil {
		return nil, fmt.Errorf("failed to create model adapter: %w", err)
	}

	// 2. Load classic SWE tools
	tools, err := GetSWETools()
	if err != nil {
		return nil, fmt.Errorf("failed to load tool set: %w", err)
	}

	// 3. Setup tool with custom confirmation provider that blocks on the Bridge
	wrappedTools := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		// Wrap all tools to run through the permission checking pipeline
		wrappedTools = append(wrappedTools, &blockingConfirmationTool{
			Tool: t,
		})
	}

	// 4. Create llmagent — inject persistent memories into the system instruction
	baseInstruction := "" // now built dynamically by SystemPromptBuilder in prompt.go

	// s09: Append any durable memories that survived from previous sessions.
	// "Memory gives direction; current observation gives truth."
	instruction := baseInstruction
	if memSection := GlobalMemoryManager.BuildSystemPromptSection(); memSection != "" {
		instruction = baseInstruction + "\n\n" + memSection
	}

	delegator := &DynamicLLMDelegator{currentModel: modelAdapter}

	rootAgent, err := llmagent.New(llmagent.Config{
		Name:        "iroha-agent",
		Instruction: instruction,
		Model:       delegator,
		Tools:       wrappedTools,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// 5. Create persistent session service
	inMem := session.InMemoryService()
	GlobalSessionService = NewPersistentSessionService(inMem, GetSessionsDir())

	// Pre-load all sessions from disk
	if err := GlobalSessionService.LoadSessions(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to restore historical sessions: %v\n", err)
	}

	// 6. Create ADK Runner
	adkRunner, err := runner.New(runner.Config{
		AppName:           "iroha",
		Agent:             rootAgent,
		SessionService:    GlobalSessionService,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	// 7. Fire SessionStart hooks — runs external scripts once at startup
	GlobalHookManager.RunHooks(HookSessionStart, HookContext{})

	// Initialize debug logging for LLM adapter
	llm.InitDebugLog()

	// 8. Configure auto-review with the model adapter
	SetAutoReviewConfig(modelAdapter)

	// Start background CronScheduler
	GlobalCronScheduler.Start()

	// Initialize GlobalAgentPool active parameters
	GlobalAgentPool.mu.Lock()
	GlobalAgentPool.Provider = provider
	GlobalAgentPool.ModelName = modelName
	GlobalAgentPool.APIKey = apiKey
	GlobalAgentPool.BaseURL = baseURL
	GlobalAgentPool.APIFormat = apiFormat
	GlobalAgentPool.GenkitRegistry = g
	GlobalAgentPool.mu.Unlock()

	// Override team ProcessMessage callback to use our agent pool
	GlobalTeamManager.ProcessMessage = func(teammate *Teammate, msg TeamMessage) (string, error) {
		return GlobalAgentPool.ExecuteMessage(teammate, msg)
	}

	globalLLMModel = modelAdapter

	// Trigger non-blocking automatic memory consolidation pass ("Dream Pass") in background
	if GlobalDreamConsolidator != nil {
		go func() {
			if _, err := GlobalDreamConsolidator.Consolidate(GlobalMemoryManager, false); err != nil {
				LogError(CatSession, "dream_consolidation_failed", "dream consolidation failed", err, nil)
			}
		}()
	}

	return &CustomRunner{
		adkRunner:       adkRunner,
		llmModel:        modelAdapter,
		delegator:       delegator,
		Provider:        provider,
		ActiveModelName: modelName,
		APIKey:          apiKey,
		BaseURL:         baseURL,
		APIFormat:       apiFormat,
		GenkitRegistry:  g,
		deps: RunnerDeps{
			TodoManager:        GlobalTodoManager,
			MemoryManager:      GlobalMemoryManager,
			SessionService:     GlobalSessionService,
			BackgroundManager:  GlobalBackgroundManager,
			CronScheduler:      GlobalCronScheduler,
			HookManager:        GlobalHookManager,
			Logger:             GlobalLogger,
			ToolCircuitBreaker: GlobalToolCircuitBreaker,
			AgentPool:          GlobalAgentPool,
			TeamManager:        GlobalTeamManager,
			DreamConsolidator:  GlobalDreamConsolidator,
			AutoReviewConfig:   GlobalAutoReviewConfig,
			PermissionManager:  GlobalPermissionManager,
			TaskManager:        GlobalTaskManager,
			MCPRouter:          GlobalMCPRouter,
			Bridge:             Bridge,
		},
	}, nil
}

func (cr *CustomRunner) SwitchModel(provider llm.ProviderType, modelName string, apiKey string, baseURL string, apiFormat llm.APIFormat) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	g := initGenkit(provider, apiKey, baseURL)

	systemPrompt := buildSystemPrompt()
	newAdapter, err := llm.NewAdapter(g, provider, modelName, apiKey, baseURL, systemPrompt, apiFormat, runnerHooks{})
	if err != nil {
		return fmt.Errorf("failed to create model adapter: %w", err)
	}

	cr.delegator.SetModel(newAdapter)
	cr.llmModel = newAdapter
	cr.Provider = provider
	cr.ActiveModelName = modelName
	cr.APIKey = apiKey
	cr.BaseURL = baseURL
	cr.APIFormat = apiFormat
	cr.GenkitRegistry = g

	GlobalAgentPool.mu.Lock()
	GlobalAgentPool.Provider = provider
	GlobalAgentPool.ModelName = modelName
	GlobalAgentPool.APIKey = apiKey
	GlobalAgentPool.BaseURL = baseURL
	GlobalAgentPool.APIFormat = apiFormat
	GlobalAgentPool.GenkitRegistry = g
	GlobalAgentPool.mu.Unlock()
	cr.deps.AgentPool = GlobalAgentPool

	SetAutoReviewConfig(newAdapter)
	globalLLMModel = newAdapter

	return nil
}

func (cr *CustomRunner) ModelName() string {
	if cr.llmModel == nil {
		return "Unknown"
	}
	return cr.llmModel.Name()
}

func (cr *CustomRunner) GetTokenUsage() int {
	if cr.llmModel == nil {
		return 0
	}
	if adapter, ok := cr.llmModel.(llm.TokenTracker); ok {
		tokens := adapter.CumulativeTokens()
		if tokens > 0 {
			return tokens
		}
	}
	return 0
}
