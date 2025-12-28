package llm

import (
	"context"
)

// CompletionRequest represents a request to an LLM provider
type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int64
}

// CompletionResponse represents a response from an LLM provider
type CompletionResponse struct {
	Content string
}

// Provider defines the interface for LLM providers
type Provider interface {
	// Complete sends a completion request and returns the response
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	// Name returns the name of the provider
	Name() string
}
