package llm

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs
type OpenAIProvider struct {
	client *openai.Client
	model  string
}

// NewOpenAIProvider creates a new OpenAI-compatible provider
func NewOpenAIProvider(apiKey string, model string, baseURL string) *OpenAIProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}

	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(opts...)

	return &OpenAIProvider{
		client: &client,
		model:  model,
	}
}

// Complete sends a completion request to an OpenAI-compatible API
func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(req.SystemPrompt),
		openai.UserMessage(req.UserPrompt),
	}

	completion, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     p.model,
		Messages:  messages,
		MaxTokens: openai.Int(req.MaxTokens),
	})
	if err != nil {
		return nil, fmt.Errorf("openai completion failed: %w", err)
	}

	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("openai returned empty response")
	}

	return &CompletionResponse{
		Content: completion.Choices[0].Message.Content,
	}, nil
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}
