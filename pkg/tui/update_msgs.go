package tui

import (
	"context"
	"os"
	"time"

	"iroha/pkg/agent"

	"google.golang.org/adk/session"
	"golang.org/x/term"
)

type StartupPromptMsg struct {
	Prompt string
}

// RunRawTUI launches the interactive standard library raw terminal TUI loop.
// It initializes standard sync-rendering, maps thread-safe events, and blocks on input.
func RunRawTUI(runner *agent.CustomRunner, sessionID string, startInSessionPicker bool, initialMode agent.PermissionMode, startupPrompt string) error {
	m := SetupRawTui(runner, sessionID, startInSessionPicker, initialMode, startupPrompt)
	renderer := NewRawRenderer(os.Stdout)
	eventChan := make(chan any, 256)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Thread-safe callback redirects
	m.OnEvent = func(ev *session.Event) {
		if ev != nil && ev.LLMResponse.Content != nil {
			for _, part := range ev.LLMResponse.Content.Parts {
				if part.Text != "" {
					eventChan <- StreamTextMsg{Text: part.Text}
				}
			}
		}
	}
	m.OnError = func(err error) {
		eventChan <- AgentErrorMsg{Err: err}
	}
	m.OnDone = func() {
		eventChan <- AgentDoneMsg{}
	}

	// 2. Scan standard keyboard inputs in background raw goroutine
	go func() {
		_ = ReadRawKeys(ctx, func(k Key) bool {
			eventChan <- k
			return true
		})
	}()

	// 3. Channel bridge redirects
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case prompt := <-agent.Bridge.PromptChan:
				eventChan <- ConfirmationRequiredMsg{Prompt: prompt}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case status := <-agent.ToolBridge.StatusChan:
				eventChan <- ToolStatusMsg{Status: status}
			}
		}
	}()

	// Spinner ticking timer goroutine
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				eventChan <- "tick"
			}
		}
	}()

	updateWidth := func() {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			m.Width = w
		} else {
			m.Width = 80
		}
	}

	updateWidth()
	// Draw the initial welcome screen
	renderer.Draw(m.Render())

	// Handle CLI trailing prompts immediately
	if m.StartupPrompt != "" {
		eventChan <- StartupPromptMsg{Prompt: m.StartupPrompt}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-eventChan:
			shouldExit := m.HandleEvent(ev)
			if shouldExit {
				// Reset terminal renderer state before exit
				renderer.Reset()
				return nil
			}
			updateWidth()
			renderer.Draw(m.Render())
		}
	}
}
