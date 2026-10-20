package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/generate"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/lfpweather"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/llm"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
	"github.com/redis/go-redis/v9"
)

type fakeProvider struct{ content string }

func (f *fakeProvider) Complete(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: f.content}, nil
}

func (f *fakeProvider) Name() string { return "fake" }

const testNWSForecast = `{"properties":{"periods":[
{"name":"Tonight","startTime":"2026-10-19T18:00:00-07:00","endTime":"2026-10-20T06:00:00-07:00","temperature":45,"detailedForecast":"Mostly cloudy, with a low around 45."},
{"name":"Tuesday","startTime":"2026-10-20T06:00:00-07:00","endTime":"2026-10-20T18:00:00-07:00","temperature":61,"detailedForecast":"Partly sunny, with a high near 61."},
{"name":"Tuesday Night","startTime":"2026-10-20T18:00:00-07:00","endTime":"2026-10-21T06:00:00-07:00","temperature":47,"detailedForecast":"Cloudy, with a low around 47."}
]}}`

// TestGetForecastSummaryUsesGenerator checks that the handler serves the
// generator's result in the {summary, icon, last_updated} shape. The Dragonfly
// client points at a closed port, so the cache read and write fail (and are
// only logged) and the handler falls back to generating, as it does when the
// worker is disabled.
func TestGetForecastSummaryUsesGenerator(t *testing.T) {
	nwsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(testNWSForecast))
	}))
	defer nwsSrv.Close()
	lfpSrv := httptest.NewServer(http.NotFoundHandler())
	defer lfpSrv.Close()

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	defer func() { _ = rdb.Close() }()
	df := &dragonfly.DragonflyClient{Client: rdb, KeyPrefix: "lfia", CacheResultsDuration: time.Minute}

	provider := &fakeProvider{content: `{"summary": "Cloudy tonight, partly sunny Tuesday.", "icon": "cloud-moon"}`}
	gen := generate.New(
		provider,
		lfpweather.NewClient(&http.Client{Timeout: time.Second}, lfpSrv.URL, ""),
		nws.NewNWSClientWithBaseURL(&http.Client{Timeout: time.Second}, nwsSrv.URL),
		df,
		"SEW/127,75",
		5*time.Second,
	)
	lh := NewLLMHandler(provider, nil, df, gen, 5*time.Second)

	rec := httptest.NewRecorder()
	lh.GetForecastSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forecast/summary", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not a JSON object: %v; body: %s", err, rec.Body.String())
	}
	if len(raw) != 3 {
		t.Errorf("body keys = %v, want exactly summary, icon, last_updated", raw)
	}

	var got generate.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body does not decode as generate.Result: %v", err)
	}
	if got.Summary != "Cloudy tonight, partly sunny Tuesday." || got.Icon != "cloud-moon" {
		t.Errorf("result = %+v, want the generator's summary and icon", got)
	}
	if got.LastUpdated.IsZero() {
		t.Error("last_updated is zero")
	}
}
