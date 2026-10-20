package generate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/lfpweather"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/llm"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
)

// fakeProvider records the last request and returns a fixed answer.
type fakeProvider struct {
	mu       sync.Mutex
	requests []llm.CompletionRequest
	content  string
}

func (f *fakeProvider) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	return &llm.CompletionResponse{Content: f.content}, nil
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) lastRequest(t *testing.T) llm.CompletionRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("provider was not called")
	}
	return f.requests[len(f.requests)-1]
}

const testNWSForecast = `{"properties":{"periods":[
{"number":1,"name":"Tonight","startTime":"2026-10-19T18:00:00-07:00","endTime":"2026-10-20T06:00:00-07:00","isDaytime":false,"temperature":45,"temperatureUnit":"F","windSpeed":"3 mph","windDirection":"S","shortForecast":"Mostly Cloudy","detailedForecast":"Mostly cloudy, with a low around 45."},
{"number":2,"name":"Tuesday","startTime":"2026-10-20T06:00:00-07:00","endTime":"2026-10-20T18:00:00-07:00","isDaytime":true,"temperature":61,"temperatureUnit":"F","windSpeed":"5 mph","windDirection":"SW","shortForecast":"Partly Sunny","detailedForecast":"Partly sunny, with a high near 61."},
{"number":3,"name":"Tuesday Night","startTime":"2026-10-20T18:00:00-07:00","endTime":"2026-10-21T06:00:00-07:00","isDaytime":false,"temperature":47,"temperatureUnit":"F","windSpeed":"4 mph","windDirection":"S","shortForecast":"Cloudy","detailedForecast":"Cloudy, with a low around 47."},
{"number":4,"name":"Wednesday","startTime":"2026-10-21T06:00:00-07:00","endTime":"2026-10-21T18:00:00-07:00","isDaytime":true,"temperature":58,"temperatureUnit":"F","windSpeed":"6 mph","windDirection":"S","shortForecast":"Rain","detailedForecast":"Rain. High near 58."}
]}}`

const testLLMAnswer = `{"summary": "Mostly cloudy tonight with a low around 45.", "icon": "cloud-moon"}`

// newNWSServer serves testNWSForecast for any gridpoint forecast request.
func newNWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/gridpoints/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/geo+json")
		_, _ = w.Write([]byte(testNWSForecast))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestGenerator wires a Generator to the fake provider, the given
// lfpweather and NWS servers, and a Dragonfly client that is never contacted
// (buildForecastSummary does not touch the cache).
func newTestGenerator(t *testing.T, provider llm.Provider, lfpURL string, lfpTimeout time.Duration, nwsURL string) *Generator {
	t.Helper()
	lfp := lfpweather.NewClient(&http.Client{Timeout: lfpTimeout}, lfpURL, "")
	nwsClient := nws.NewNWSClientWithBaseURL(&http.Client{Timeout: 5 * time.Second}, nwsURL)
	return New(provider, lfp, nwsClient, &dragonfly.DragonflyClient{KeyPrefix: "lfia"}, "SEW/127,75", 10*time.Second)
}

// userInput returns the text after the final "input: " in the user prompt.
func userInput(t *testing.T, req llm.CompletionRequest) string {
	t.Helper()
	i := strings.LastIndex(req.UserPrompt, "input: ")
	if i < 0 {
		t.Fatalf("user prompt has no input: %s", req.UserPrompt)
	}
	return req.UserPrompt[i+len("input: "):]
}

func TestForecastSummaryCacheKey(t *testing.T) {
	g := New(nil, nil, nil, &dragonfly.DragonflyClient{KeyPrefix: "lfia"}, "", time.Second)
	if got := g.cacheKey(KeyForecastSummary); got != "lfia-forecast-summary" {
		t.Errorf("cacheKey(KeyForecastSummary) = %q, want %q", got, "lfia-forecast-summary")
	}
}

func TestForecastSummaryUsesThreeNWSPeriods(t *testing.T) {
	provider := &fakeProvider{content: testLLMAnswer}
	lfpSrv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(lfpSrv.Close)
	g := newTestGenerator(t, provider, lfpSrv.URL, time.Second, newNWSServer(t).URL)

	res, err := g.buildForecastSummary(context.Background())
	if err != nil {
		t.Fatalf("buildForecastSummary returned error: %v", err)
	}
	if res.Summary != "Mostly cloudy tonight with a low around 45." || res.Icon != "cloud-moon" {
		t.Errorf("result = %+v, want the provider's summary and icon", res)
	}

	req := provider.lastRequest(t)
	input := userInput(t, req)
	for _, want := range []string{`"Tonight"`, `"Tuesday"`, `"Tuesday Night"`} {
		if !strings.Contains(input, want) {
			t.Errorf("input missing period %s\ninput: %s", want, input)
		}
	}
	if strings.Contains(input, `"Wednesday"`) {
		t.Errorf("input includes a fourth period\ninput: %s", input)
	}
	if !strings.Contains(req.SystemPrompt, "thermometer-snowflake") {
		t.Errorf("system prompt is missing the icon list:\n%s", req.SystemPrompt)
	}
}
