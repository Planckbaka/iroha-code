package tui

import (
	"context"

	"google.golang.org/adk/session"
)

// AgentRunner abstracts the agent execution interface for testing.
type AgentRunner interface {
	Execute(ctx context.Context, userID, sessionID, prompt string,
		onEvent func(*session.Event), onError func(error), onDone func())
	ModelName() string
	GetTokenUsage() int
}

// BridgeResponder abstracts the confirmation bridge for testing.
type BridgeResponder interface {
	Send(response string)
}
