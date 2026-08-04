// Package generate builds the headline summaries that combine live station
// data from the lfpweather-api with the NWS forecast: current conditions, a
// smoke outlook, and a fire weather read. Both the HTTP handlers and the
// background worker use it, so the prompts and cache logic live in one place.
package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/lfpweather"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/llm"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
	"github.com/redis/go-redis/v9"
)

// Cache key suffixes for each generated summary. The Dragonfly key prefix is
// prepended at write time.
const (
	KeyCurrentConditions = "current-conditions"
	KeySmokeOutlook      = "smoke-outlook"
	KeyFireWeather       = "fire-weather"
)

// iconList is the shared icon vocabulary. Every summary picks one icon from
// this set so the frontend can render it the same way as the forecast summary.
const iconList = `cloud
cloud-drizzle
cloud-fog
cloud-hail
cloud-lightning
cloud-moon
cloud-moon-rain
cloud-rain
cloud-rain-wind
cloud-snow
cloud-sun
cloud-sun-rain
cloudy
snowflake
sun
sun-snow
thermometer-snowflake
thermometer-sun
wind`

// Result is the cached payload and the HTTP response body for a summary. It
// matches the {summary, icon, last_updated} shape of the forecast summary.
type Result struct {
	Summary     string    `json:"summary"`
	Icon        string    `json:"icon"`
	LastUpdated time.Time `json:"last_updated"`
}

// Generator produces the summaries and caches them in Dragonfly.
type Generator struct {
	llm       llm.Provider
	lfp       *lfpweather.Client
	nws       *nws.NWSClient
	dragonfly *dragonfly.DragonflyClient
	gridPoint string
	timeout   time.Duration
}

// New returns a Generator. The timeout bounds each summary during a worker
// refresh; the HTTP handlers pass their own request context.
func New(
	provider llm.Provider,
	lfp *lfpweather.Client,
	nwsClient *nws.NWSClient,
	dragonflyClient *dragonfly.DragonflyClient,
	gridPoint string,
	timeout time.Duration,
) *Generator {
	return &Generator{
		llm:       provider,
		lfp:       lfp,
		nws:       nwsClient,
		dragonfly: dragonflyClient,
		gridPoint: gridPoint,
		timeout:   timeout,
	}
}

// buildFunc fetches the inputs and produces a summary. It does not set
// LastUpdated or touch the cache; produce does that.
type buildFunc func(ctx context.Context) (*Result, error)

// GetCurrentConditions returns the cached current conditions summary, or
// generates and caches it on a miss.
func (g *Generator) GetCurrentConditions(ctx context.Context) (*Result, error) {
	return g.getOrProduce(ctx, KeyCurrentConditions, g.buildCurrentConditions)
}

// GetSmokeOutlook returns the cached smoke outlook, or generates and caches it
// on a miss.
func (g *Generator) GetSmokeOutlook(ctx context.Context) (*Result, error) {
	return g.getOrProduce(ctx, KeySmokeOutlook, g.buildSmokeOutlook)
}

// GetFireWeather returns the cached fire weather summary, or generates and
// caches it on a miss.
func (g *Generator) GetFireWeather(ctx context.Context) (*Result, error) {
	return g.getOrProduce(ctx, KeyFireWeather, g.buildFireWeather)
}

// RefreshAll regenerates and caches all three summaries at once. The
// background worker calls it. Each summary runs with its own timeout, and a
// failure in one does not stop the others.
func (g *Generator) RefreshAll(ctx context.Context) {
	jobs := []struct {
		name  string
		key   string
		build buildFunc
	}{
		{"current conditions", KeyCurrentConditions, g.buildCurrentConditions},
		{"smoke outlook", KeySmokeOutlook, g.buildSmokeOutlook},
		{"fire weather", KeyFireWeather, g.buildFireWeather},
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(name, key string, build buildFunc) {
			defer wg.Done()

			jobCtx, cancel := context.WithTimeout(ctx, g.timeout)
			defer cancel()

			if _, err := g.produce(jobCtx, key, build); err != nil {
				slog.Error("worker: failed to generate summary", slog.String("summary", name), slog.String("error", err.Error()))
				return
			}
			slog.Info("worker: summary generated and cached", slog.String("summary", name))
		}(j.name, j.key, j.build)
	}
	wg.Wait()
}

// cacheKey prepends the Dragonfly key prefix to a summary key.
func (g *Generator) cacheKey(key string) string {
	return fmt.Sprintf("%s-%s", g.dragonfly.KeyPrefix, key)
}

// getOrProduce returns the cached summary, or produces a fresh one on a miss.
func (g *Generator) getOrProduce(ctx context.Context, key string, build buildFunc) (*Result, error) {
	res, err := g.dragonfly.Client.Get(ctx, g.cacheKey(key)).Result()
	if err != nil && err != redis.Nil {
		slog.Error("could not read summary from cache", slog.String("key", key), slog.String("error", err.Error()))
	} else if err == nil && res != "" {
		var cached Result
		if err := json.Unmarshal([]byte(res), &cached); err != nil {
			slog.Error("could not unmarshal cached summary", slog.String("key", key), slog.String("error", err.Error()))
		} else {
			return &cached, nil
		}
	}

	return g.produce(ctx, key, build)
}

// produce builds a summary, stamps it, caches it, and returns it.
func (g *Generator) produce(ctx context.Context, key string, build buildFunc) (*Result, error) {
	result, err := build(ctx)
	if err != nil {
		return nil, err
	}

	result.LastUpdated = time.Now()

	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal summary: %w", err)
	}

	if err := g.dragonfly.Client.Set(ctx, g.cacheKey(key), encoded, g.dragonfly.CacheResultsDuration).Err(); err != nil {
		slog.Error("could not cache summary", slog.String("key", key), slog.String("error", err.Error()))
	}

	return result, nil
}

// llmOutput is the JSON object each prompt asks the model to return.
type llmOutput struct {
	Summary string `json:"summary"`
	Icon    string `json:"icon"`
}

// complete runs one prompt through the provider and parses the {summary, icon}
// output.
func (g *Generator) complete(ctx context.Context, systemPrompt, task string, shots []shot, input string) (*Result, error) {
	response, err := g.llm.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   buildFinalPrompt(task, shots, input),
		MaxTokens:    1024,
	})
	if err != nil {
		return nil, err
	}

	cleaned := stripMarkdownCodeBlock(response.Content)

	var out llmOutput
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal summary output: %w (raw: %s)", err, cleaned)
	}

	return &Result{Summary: out.Summary, Icon: out.Icon}, nil
}

// shot is one few-shot example.
type shot struct {
	Input  string
	Output string
}

// multiShotWrapper renders few-shot examples as an <examples> block.
func multiShotWrapper(shots []shot) string {
	var sb strings.Builder
	sb.WriteString("<examples>")
	for _, s := range shots {
		sb.WriteString("<example>")
		sb.WriteString("input: ")
		sb.WriteString(s.Input)
		sb.WriteString("\noutput: ")
		sb.WriteString(s.Output)
		sb.WriteString("</example>")
	}
	sb.WriteString("</examples>")
	return sb.String()
}

// buildFinalPrompt joins the task, the examples, and the input data.
func buildFinalPrompt(task string, shots []shot, input string) string {
	return fmt.Sprintf("%s\n\n%s\n\n%s", task, multiShotWrapper(shots), fmt.Sprintf("input: %s", input))
}

// stripMarkdownCodeBlock removes a leading ```json or ``` fence and a trailing
// ``` fence so the JSON can be parsed.
func stripMarkdownCodeBlock(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimPrefix(text, "```json")
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
	}
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
