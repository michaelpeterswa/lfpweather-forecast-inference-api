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
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
	"github.com/redis/go-redis/v9"
)

type ForecastHandler struct {
	AnthropicHandler *AnthropicHandler
	GridPoints       string
}

type ForecastSummaryResponse struct {
	Summary     string    `json:"summary"`
	Icon        string    `json:"icon"`
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

	systemPrompt := `You are a tool that can provide concise summaries of weather forecasts.
	You have access to the following list of icons:
	"""
	cloud
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
	wind
	"""
	`

	prompt :=
		`
		Input is a JSON array with one entry per forecast period.
		Output is a JSON object with the key "summary" containing the overall forecast in at most four sentences and "icon" containing the icon that best fits the soonest weather for this summary.
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
			Output: `{"summary": "Tonight, mostly cloudy with a low around 54. Sunday, mostly sunny with a high near 74, temperatures falling to around 72 in the afternoon. Sunday night, mostly cloudy with a low around 51. Winds light and variable.", "icon": "cloud-moon"}`,
		},
	}

	message, err := ah.AnthropicClient.Messages.New(timeoutCtx, anthropic.MessageNewParams{
		Model:     anthropic.F(anthropic.ModelClaude3_5SonnetLatest),
		MaxTokens: anthropic.F(int64(1024)),
		System:    anthropic.F([]anthropic.TextBlockParam{anthropic.NewTextBlock(systemPrompt)}),
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

type GetForecastPeriodsInformation struct {
	Name      string `json:"name"`
	TimeOfDay string `json:"time_of_day"`
	Icon      string `json:"icon"`
	Beaufort  string `json:"beaufort"`
}

type JoinedForecastPeriodsInformation struct {
	Name             string    `json:"name"`
	TimeOfDay        string    `json:"time_of_day"`
	Icon             string    `json:"icon"`
	Beaufort         string    `json:"beaufort"`
	DetailedForecast string    `json:"detailed_forecast"`
	ShortForecast    string    `json:"short_forecast"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	Temperature      int       `json:"temperature"`
	WindSpeed        string    `json:"wind_speed"`
	WindDirection    string    `json:"wind_direction"`
}

func JoinForecastPeriodsInformation(fpi GetForecastPeriodsInformation, period nws.SimplifiedForecastPeriods) JoinedForecastPeriodsInformation {
	return JoinedForecastPeriodsInformation{
		Name:             fpi.Name,
		TimeOfDay:        fpi.TimeOfDay,
		Icon:             fpi.Icon,
		Beaufort:         fpi.Beaufort,
		DetailedForecast: period.DetailedForecast,
		ShortForecast:    period.ShortForecast,
		StartTime:        period.StartTime,
		EndTime:          period.EndTime,
		Temperature:      period.Temperature,
		WindSpeed:        period.WindSpeed,
		WindDirection:    period.WindDirection,
	}
}

type GetForecastPeriodsInformationResponse struct {
	Periods     []JoinedForecastPeriodsInformation `json:"periods"`
	LastUpdated time.Time                          `json:"last_updated"`
}

func (ah *AnthropicHandler) GetForcastPeriodsInformation(w http.ResponseWriter, r *http.Request) {
	timeoutCtx, cancel := context.WithTimeout(r.Context(), ah.Timeout)
	defer cancel()

	res, err := ah.DragonflyClient.Client.Get(timeoutCtx, fmt.Sprintf("%s-%s", ah.DragonflyClient.KeyPrefix, "forecast-periods-information")).Result()
	if err != nil && err != redis.Nil {
		slog.Error("could not get forecast periods information from cache", slog.String("error", err.Error()))
	} else if err == nil && res != "" {
		var fpi GetForecastPeriodsInformationResponse
		err := json.Unmarshal([]byte(res), &fpi)
		if err != nil {
			slog.Error("could not unmarshal forecast periods information from cache", slog.String("error", err.Error()))
		}

		fpiJson, err := json.Marshal(fpi)
		if err != nil {
			rfc9457.NewRFC9457(
				rfc9457.WithTitle("failed to marshal forecast periods information from cache"),
				rfc9457.WithDetail(fmt.Sprintf("failed to marshal forecast periods information from cache: %s", err.Error())),
				rfc9457.WithInstance(r.URL.Path),
				rfc9457.WithStatus(http.StatusInternalServerError),
			).ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write([]byte(fpiJson))
		return
	}

	periods, err := ah.NWSClient.GetSimplifiedForecastNPeriods("SEW/127,75", -1)
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

	systemPrompt := `You are a tool that can provide concise weather forecast breakdowns.
		You have access to the following list of icons:
		"""
		cloud
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
		wind
		"""
		`

	prompt :=
		`Input is a JSON array with one entry per forecast period.
		Output is a JSON array with the following key-value pairs:
		"name": the "name" field on the given forecast period,
		"time_of_day": either day or night based upon the given forecast period,
		"icon": the icon that best fits the "detailed_forecast" for this forecast period,
		"beaufort": the beaufort scale string that best fits the "wind_speed" for this period,

		Do not include any information that is not present in the input.
		Only include the JSON, do not include outside text.


		Structure the output exactly like this, but remove all whitespace:

		"""
		[
		{
			"name": "",
			"time_of_day": "",
			"icon": "",
			"beaufort": "",
		},
		...
		]
		"""
		`

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
			Output: `[{"name":"Tonight","time_of_day":"night","icon":"cloud-moon","beaufort":"Light air"},{"name":"Sunday","time_of_day":"day","icon":"cloud-sun","beaufort":"Light breeze"},{"name":"Sunday Night","time_of_day":"night","icon":"cloud-moon","beaufort":"Light breeze"}]`,
		},
	}

	message, err := ah.AnthropicClient.Messages.New(timeoutCtx, anthropic.MessageNewParams{
		Model:     anthropic.F(anthropic.ModelClaude3_5SonnetLatest),
		MaxTokens: anthropic.F(int64(1024)),
		System:    anthropic.F([]anthropic.TextBlockParam{anthropic.NewTextBlock(systemPrompt)}),
		Messages: anthropic.F([]anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(buildFinalPrompt(prompt, fewShotTraining, string(periodsJSON)))),
		}),
	})
	if err != nil {
		rfc9457.NewRFC9457(
			rfc9457.WithTitle("failed to get forecast periods information"),
			rfc9457.WithDetail(fmt.Sprintf("failed to get forecast periods information: %s", err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	var fpi []GetForecastPeriodsInformation
	err = json.Unmarshal([]byte(message.Content[0].Text), &fpi)
	if err != nil {
		rfc9457.NewRFC9457(
			rfc9457.WithTitle("failed to unmarshal forecast periods information"),
			rfc9457.WithDetail(fmt.Sprintf("failed to unmarshal forecast periods information: %s", err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	joinedPeriods := make([]JoinedForecastPeriodsInformation, 0)
	for _, period := range periods {
		for _, fpiPeriod := range fpi {
			if period.Name == fpiPeriod.Name {
				joinedPeriods = append(joinedPeriods, JoinForecastPeriodsInformation(fpiPeriod, period))
			}
		}
	}

	fpiResponse := GetForecastPeriodsInformationResponse{
		Periods:     joinedPeriods,
		LastUpdated: time.Now(),
	}

	fpiJson, err := json.Marshal(fpiResponse)
	if err != nil {
		rfc9457.NewRFC9457(
			rfc9457.WithTitle("failed to marshal forecast periods information"),
			rfc9457.WithDetail(fmt.Sprintf("failed to marshal forecast periods information: %s", err.Error())),
			rfc9457.WithInstance(r.URL.Path),
			rfc9457.WithStatus(http.StatusInternalServerError),
		).ServeHTTP(w, r)
		return
	}

	err = ah.DragonflyClient.Client.Set(timeoutCtx, fmt.Sprintf("%s-%s", ah.DragonflyClient.KeyPrefix, "forecast-periods-information"), fpiJson, ah.DragonflyClient.CacheResultsDuration).Err()
	if err != nil {
		slog.Error("could not set forecast periods information in cache", slog.String("error", err.Error()))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fpiJson))
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
