package generate

import (
	"context"
	"encoding/json"
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

// testStationForecast has its low (40.6 -> 41) at 13:00Z (6 AM PDT) and its
// high at 22:00Z (3 PM PDT); NWS over the same hours runs 45..60.
const testStationForecast = `{"issued_at":"2026-10-20T04:00:00Z","source":"timestral","forecast":[
{"valid_at":"2026-10-20T05:00:00Z","lead_hours":1,"median":47.2},
{"valid_at":"2026-10-20T13:00:00Z","lead_hours":9,"median":40.6},
{"valid_at":"2026-10-20T22:00:00Z","lead_hours":18,"median":58.9}
],"nws":[
{"time":"2026-10-20T05:00:00Z","temperature":49},
{"time":"2026-10-20T13:00:00Z","temperature":45},
{"time":"2026-10-20T22:00:00Z","temperature":60}
]}`

// summaryInput decodes the forecast summary input from the user prompt.
func summaryInput(t *testing.T, req llm.CompletionRequest) map[string]json.RawMessage {
	t.Helper()
	var in map[string]json.RawMessage
	if err := json.Unmarshal([]byte(userInput(t, req)), &in); err != nil {
		t.Fatalf("prompt input is not a JSON object: %v\ninput: %s", err, userInput(t, req))
	}
	if _, ok := in["periods"]; !ok {
		t.Errorf("prompt input has no periods: %s", userInput(t, req))
	}
	return in
}

func TestForecastSummaryIncludesStationForecast(t *testing.T) {
	var gotPath string
	lfpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testStationForecast))
	}))
	t.Cleanup(lfpSrv.Close)
	provider := &fakeProvider{content: testLLMAnswer}
	g := newTestGenerator(t, provider, lfpSrv.URL, time.Second, newNWSServer(t).URL)
	// 30 minutes after the run was issued, so every point is still ahead.
	g.now = func() time.Time { return time.Date(2026, 10, 20, 4, 30, 0, 0, time.UTC) }

	if _, err := g.buildForecastSummary(context.Background()); err != nil {
		t.Fatalf("buildForecastSummary returned error: %v", err)
	}
	if gotPath != "/api/v1/temperature_forecast/latest" {
		t.Errorf("lfpweather path = %q, want /api/v1/temperature_forecast/latest", gotPath)
	}

	in := summaryInput(t, provider.lastRequest(t))
	raw, ok := in["station_forecast"]
	if !ok {
		t.Fatalf("prompt input has no station_forecast: %v", in)
	}
	var sf stationForecastInput
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatalf("station_forecast does not decode: %v", err)
	}
	if sf.LowF != 41 {
		t.Errorf("station_forecast.low_f = %d, want 41", sf.LowF)
	}
	if sf.LowAt != "Tue 6 AM" {
		t.Errorf("station_forecast.low_at = %q, want %q (America/Los_Angeles)", sf.LowAt, "Tue 6 AM")
	}
	if sf.NWSLowF == nil || *sf.NWSLowF != 45 {
		t.Errorf("station_forecast.nws_low_f = %v, want 45", sf.NWSLowF)
	}
}

func TestForecastSummaryWithoutStationForecastOn404(t *testing.T) {
	lfpSrv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(lfpSrv.Close)
	provider := &fakeProvider{content: testLLMAnswer}
	g := newTestGenerator(t, provider, lfpSrv.URL, time.Second, newNWSServer(t).URL)

	res, err := g.buildForecastSummary(context.Background())
	if err != nil {
		t.Fatalf("buildForecastSummary returned error: %v", err)
	}
	if res == nil || res.Summary == "" {
		t.Fatalf("result = %+v, want a summary", res)
	}
	if in := summaryInput(t, provider.lastRequest(t)); in["station_forecast"] != nil {
		t.Errorf("prompt input has station_forecast on 404: %s", in["station_forecast"])
	}
}

func TestForecastSummaryStationTimeout(t *testing.T) {
	lfpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		_, _ = w.Write([]byte(testStationForecast))
	}))
	t.Cleanup(lfpSrv.Close)
	provider := &fakeProvider{content: testLLMAnswer}
	g := newTestGenerator(t, provider, lfpSrv.URL, 50*time.Millisecond, newNWSServer(t).URL)

	res, err := g.buildForecastSummary(context.Background())
	if err != nil {
		t.Fatalf("buildForecastSummary returned error: %v", err)
	}
	if res == nil || res.Summary == "" {
		t.Fatalf("result = %+v, want a summary", res)
	}
	if in := summaryInput(t, provider.lastRequest(t)); in["station_forecast"] != nil {
		t.Errorf("prompt input has station_forecast after a timeout: %s", in["station_forecast"])
	}
}

func TestForecastSummaryPromptExplainsStationForecast(t *testing.T) {
	lfpSrv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(lfpSrv.Close)
	provider := &fakeProvider{content: testLLMAnswer}
	g := newTestGenerator(t, provider, lfpSrv.URL, time.Second, newNWSServer(t).URL)

	if _, err := g.buildForecastSummary(context.Background()); err != nil {
		t.Fatalf("buildForecastSummary returned error: %v", err)
	}
	prompt := provider.lastRequest(t).UserPrompt
	for _, want := range []string{"station_forecast", "2°F", "never mention a station forecast", `"low_f": 41`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
	if n := strings.Count(prompt, "<example>"); n != 3 {
		t.Errorf("prompt has %d few-shot examples, want 3", n)
	}
}

func TestForecastSummaryMaxTokens(t *testing.T) {
	lfpSrv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(lfpSrv.Close)
	provider := &fakeProvider{content: testLLMAnswer}
	g := newTestGenerator(t, provider, lfpSrv.URL, time.Second, newNWSServer(t).URL)

	if _, err := g.buildForecastSummary(context.Background()); err != nil {
		t.Fatalf("buildForecastSummary returned error: %v", err)
	}
	if got := provider.lastRequest(t).MaxTokens; got != 4096 {
		t.Errorf("forecast summary MaxTokens = %d, want 4096", got)
	}
}

func TestCurrentConditionsMaxTokens(t *testing.T) {
	lfpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"time":"2026-10-19T20:00:00Z","last":50}`))
	}))
	t.Cleanup(lfpSrv.Close)
	provider := &fakeProvider{content: testLLMAnswer}
	g := newTestGenerator(t, provider, lfpSrv.URL, time.Second, newNWSServer(t).URL)

	if _, err := g.buildCurrentConditions(context.Background()); err != nil {
		t.Fatalf("buildCurrentConditions returned error: %v", err)
	}
	if got := provider.lastRequest(t).MaxTokens; got != 1024 {
		t.Errorf("current conditions MaxTokens = %d, want 1024", got)
	}
}

func TestForecastSummaryPromptAgreeCase(t *testing.T) {
	lfpSrv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(lfpSrv.Close)
	provider := &fakeProvider{content: testLLMAnswer}
	g := newTestGenerator(t, provider, lfpSrv.URL, time.Second, newNWSServer(t).URL)

	if _, err := g.buildForecastSummary(context.Background()); err != nil {
		t.Fatalf("buildForecastSummary returned error: %v", err)
	}
	prompt := provider.lastRequest(t).UserPrompt
	if !strings.Contains(prompt, "less than 2°F, do not add a station sentence") {
		t.Error("prompt does not state the agree case")
	}
	if strings.Contains(prompt, "backyard") {
		t.Error(`prompt uses "backyard"; use "home weather station" throughout`)
	}

	// The third example is the agree case: its output has no station sentence.
	examples := strings.Split(prompt, "<example>")
	if len(examples) != 4 {
		t.Fatalf("got %d examples, want 3", len(examples)-1)
	}
	agree := examples[3]
	out := agree[strings.Index(agree, "\noutput: "):]
	if strings.Contains(strings.ToLower(out), "station") {
		t.Errorf("agree example output mentions the station: %s", out)
	}
}
