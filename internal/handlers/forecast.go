package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"alpineworks.io/rfc9457"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/redis/go-redis/v9"
)

type ForecastHandler struct {
	AnthropicHandler *AnthropicHandler
	GridPoints       string
}

type ForecastSummaryResponse struct {
	Summary     string    `json:"summary"`
	LastUpdated time.Time `json:"last_updated"`
}

func (ah *AnthropicHandler) GetForecastSummary(w http.ResponseWriter, r *http.Request) {
	timeoutCtx, cancel := context.WithTimeout(r.Context(), ah.Timeout)
	defer cancel()

	res, err := ah.DragonflyClient.Client.Get(timeoutCtx, fmt.Sprintf("%s-%s", ah.DragonflyClient.KeyPrefix, "forecast-summary")).Result()
	if err != nil && err != redis.Nil {
		slog.Error("could not get forecast summary from cache", slog.String("error", err.Error()))
	} else if err == nil && res != "" {
		var fsr ForecastSummaryResponse
		err := json.Unmarshal([]byte(res), &fsr)
		if err != nil {
			slog.Error("could not unmarshal forecast summary from cache", slog.String("error", err.Error()))
		}

		fsrJson, err := json.Marshal(fsr)
		if err != nil {
			rfc9457.NewRFC9457(
				rfc9457.WithTitle("failed to marshal forecast summary from cache"),
				rfc9457.WithDetail(fmt.Sprintf("failed to marshal forecast summary from cache: %s", err.Error())),
				rfc9457.WithInstance(r.URL.Path),
				rfc9457.WithStatus(http.StatusInternalServerError),
			).ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write([]byte(fsrJson))
		return
	}

	periods, err := ah.NWSClient.GetSimplifiedForecastNPeriods("SEW/127,75", 3)
	if err != nil {
		rfc9457.NewRFC9457(
			rfc9457.WithTitle("failed to get simplified forecast periods"),
			rfc9457.WithDetail(fmt.Sprintf("failed to get simplified forecast periods: %s", err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	periodsJSON, err := json.Marshal(periods)
	if err != nil {
		rfc9457.NewRFC9457(
			rfc9457.WithTitle("failed to marshal simplified forecast periods"),
			rfc9457.WithDetail(fmt.Sprintf("failed to marshal simplified forecast periods: %s", err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	prompt :=
		`You are a tool that can provide concise summaries of weather forecasts.
		Input is a JSON array with one entry per forecast period.
		Output is a JSON object with the key "summary" containing the overall forecast in at most four sentences.
		Each entry contains relavant weather information including a detailed text forecast.
		Do not include any information that is not present in the input.
		Do not comment twice on the same weather condition.
		Focus mainly on the daytime periods.
		Avoid editorializing or making assumptions.
		Avoid referring to "periods" in the output.
		Make the output sound like a human wrote it, with concise but friendly language and complete sentences.`

	fewShotTraining := []MultiShot{
		{
			Input: `[
					{
						"name": "Tonight",
						"start_time": "2024-06-08T20:00:00-07:00",
						"end_time": "2024-06-09T06:00:00-07:00",
						"temperature": "54F",
						"detailed_forecast": "Mostly cloudy, with a low around 54. East wind around 2 mph.",
						"relative_humidity": "80%",
						"wind_speed": "2 mph E"
						},
					}
					{
						"name": "Sunday",
						"start_time": "2024-06-09T06:00:00-07:00",
						"end_time": "2024-06-09T18:00:00-07:00",
						"temperature": "74F",
						"detailed_forecast": "Mostly sunny. High near 74, with temperatures falling to around 72 in the afternoon. Southwest wind 1 to 6 mph.",
						"relative_humidity": "79%",
						"wind_speed": "1 to 6 mph SW"
					},
					{
						"name": "Sunday Night",
						"start_time": "2024-06-09T18:00:00-07:00",
						"end_time": "2024-06-10T06:00:00-07:00",
						"temperature": "51F",
						"detailed_forecast": "Mostly cloudy, with a low around 51. West wind 2 to 6 mph.",
						"relative_humidity": "85%",
						"wind_speed": "2 to 6 mph W"
					}
				]`,
			Output: `{"summary": "Tonight, mostly cloudy with a low around 54. Sunday, mostly sunny with a high near 74, temperatures falling to around 72 in the afternoon. Sunday night, mostly cloudy with a low around 51. Winds light and variable."}`,
		},
	}

	message, err := ah.AnthropicClient.Messages.New(timeoutCtx, anthropic.MessageNewParams{
		Model:     anthropic.F(anthropic.ModelClaude3_5SonnetLatest),
		MaxTokens: anthropic.F(int64(1024)),
		Messages: anthropic.F([]anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(buildFinalPrompt(prompt, fewShotTraining, string(periodsJSON)))),
		}),
	})
	if err != nil {
		rfc9457.NewRFC9457(
			rfc9457.WithTitle("failed to get forecast summary"),
			rfc9457.WithDetail(fmt.Sprintf("failed to get forecast summary: %s", err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	var fsr ForecastSummaryResponse
	err = json.Unmarshal([]byte(message.Content[0].Text), &fsr)
	if err != nil {
		rfc9457.NewRFC9457(
			rfc9457.WithTitle("failed to unmarshal forecast summary"),
			rfc9457.WithDetail(fmt.Sprintf("failed to unmarshal forecast summary: %s", err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	fsr.LastUpdated = time.Now()

	fsrJson, err := json.Marshal(fsr)
	if err != nil {
		rfc9457.NewRFC9457(
			rfc9457.WithTitle("failed to marshal forecast summary"),
			rfc9457.WithDetail(fmt.Sprintf("failed to marshal forecast summary: %s", err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	err = ah.DragonflyClient.Client.Set(timeoutCtx, fmt.Sprintf("%s-%s", ah.DragonflyClient.KeyPrefix, "forecast-summary"), fsrJson, ah.DragonflyClient.CacheResultsDuration).Err()
	if err != nil {
		slog.Error("could not set forecast summary in cache", slog.String("error", err.Error()))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fsrJson))
}

type MultiShot struct {
	Input  string
	Output string
}

func multiShotWrapper(ms []MultiShot) string {
	var sb strings.Builder
	sb.WriteString("<examples>")

	for _, m := range ms {
		sb.WriteString("<example>")
		sb.WriteString("input: ")
		sb.WriteString(m.Input)
		sb.WriteString("\noutput: ")
		sb.WriteString(m.Output)
		sb.WriteString("</example>")
	}

	sb.WriteString("</examples>")
	return sb.String()
}

func buildFinalPrompt(prompt string, ms []MultiShot, inputData string) string {
	return fmt.Sprintf("%s\n\n%s\n\n%s", prompt, multiShotWrapper(ms), fmt.Sprintf("input: %s", inputData))
}
