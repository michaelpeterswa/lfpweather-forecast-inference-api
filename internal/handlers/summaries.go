package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"alpineworks.io/rfc9457"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/generate"
)

// GetCurrentConditions serves GET /api/v1/current: a point-in-time summary of
// the latest station observations.
func (lh *LLMHandler) GetCurrentConditions(w http.ResponseWriter, r *http.Request) {
	lh.serveSummary(w, r, "current conditions", lh.Generator.GetCurrentConditions)
}

// GetSmokeOutlook serves GET /api/v1/smoke: the air quality and wildfire smoke
// outlook.
func (lh *LLMHandler) GetSmokeOutlook(w http.ResponseWriter, r *http.Request) {
	lh.serveSummary(w, r, "smoke outlook", lh.Generator.GetSmokeOutlook)
}

// GetFireWeather serves GET /api/v1/fire_weather: a plain-language read of
// today's fire danger.
func (lh *LLMHandler) GetFireWeather(w http.ResponseWriter, r *http.Request) {
	lh.serveSummary(w, r, "fire weather", lh.Generator.GetFireWeather)
}

// serveSummary runs a generator, writes the JSON result, and returns an RFC
// 9457 problem on error.
func (lh *LLMHandler) serveSummary(w http.ResponseWriter, r *http.Request, name string, fn func(context.Context) (*generate.Result, error)) {
	timeoutCtx, cancel := context.WithTimeout(r.Context(), lh.Timeout)
	defer cancel()

	result, err := fn(timeoutCtx)
	if err != nil {
		slog.Error("failed to get summary", slog.String("summary", name), slog.String("error", err.Error()))
		rfc9457.NewRFC9457(
			rfc9457.WithTitle(fmt.Sprintf("failed to get %s summary", name)),
			rfc9457.WithDetail(fmt.Sprintf("failed to get %s summary: %s", name, err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		slog.Error("failed to marshal summary", slog.String("summary", name), slog.String("error", err.Error()))
		rfc9457.NewRFC9457(
			rfc9457.WithTitle(fmt.Sprintf("failed to marshal %s summary", name)),
			rfc9457.WithDetail(fmt.Sprintf("failed to marshal %s summary: %s", name, err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resultJSON)
}
