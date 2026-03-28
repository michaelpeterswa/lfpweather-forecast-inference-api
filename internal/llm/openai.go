package llm

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

var thinkTagRegexp = regexp.MustCompile(`(?s)<think>.*?</think>`)

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs
type OpenAIProvider struct {
	client  *openai.Client
	model   string
	noThink bool
}

// NewOpenAIProvider creates a new OpenAI-compatible provider
func NewOpenAIProvider(apiKey string, model string, baseURL string, noThink bool) *OpenAIProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}

	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(opts...)

	return &OpenAIProvider{
		client:  &client,
		model:   model,
		noThink: noThink,
	}
}

// Complete sends a completion request to an OpenAI-compatible API
func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(req.SystemPrompt),
		openai.UserMessage(req.UserPrompt),
	}

	opts := []option.RequestOption{}
	if p.noThink {
		opts = append(opts, option.WithJSONSet("chat_template_kwargs", map[string]any{
			"enable_thinking": false,
		}))
	}

	completion, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     p.model,
		Messages:  messages,
		MaxTokens: openai.Int(req.MaxTokens),
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("openai completion failed: %w", err)
	}

	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("openai returned empty response")
	}

	content := completion.Choices[0].Message.Content
	content = stripThinkTags(content)

	return &CompletionResponse{
		Content: content,
	}, nil
}

// stripThinkTags removes <think>...</think> blocks from model output
func stripThinkTags(s string) string {
	return strings.TrimSpace(thinkTagRegexp.ReplaceAllString(s, ""))
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}
