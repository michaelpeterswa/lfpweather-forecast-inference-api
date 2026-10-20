package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/generate"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/llm"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
)

// ForecastWorker handles background generation of forecast data
type ForecastWorker struct {
	LLMProvider     llm.Provider
	NWSClient       *nws.NWSClient
	DragonflyClient *dragonfly.DragonflyClient
	Generator       *generate.Generator
	Interval        time.Duration
	Timeout         time.Duration
	GridPoint       string
}

// NewForecastWorker creates a new forecast worker
func NewForecastWorker(
	provider llm.Provider,
	nwsClient *nws.NWSClient,
	dragonflyClient *dragonfly.DragonflyClient,
	generator *generate.Generator,
	interval time.Duration,
	timeout time.Duration,
	gridPoint string,
) *ForecastWorker {
	return &ForecastWorker{
		LLMProvider:     provider,
		NWSClient:       nwsClient,
		DragonflyClient: dragonflyClient,
		Generator:       generator,
		Interval:        interval,
		Timeout:         timeout,
		GridPoint:       gridPoint,
	}
}

// Start begins the background worker loop
func (w *ForecastWorker) Start(ctx context.Context) {
	slog.Info("starting forecast worker", slog.Duration("interval", w.Interval))

	// Run immediately on start
	w.runGeneration(ctx)

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("stopping forecast worker")
			return
		case <-ticker.C:
			w.runGeneration(ctx)
		}
	}
}

// runGeneration regenerates the detailed forecast periods and, through the
// generator, every headline summary.
func (w *ForecastWorker) runGeneration(ctx context.Context) {
	slog.Info("running forecast generation")

	done := make(chan struct{}, 2)

	go func() {
		w.generateForecastPeriodsInformation(ctx)
		done <- struct{}{}
	}()

	go func() {
		// RefreshAll regenerates the forecast summary, current conditions,
		// smoke outlook, and fire weather summaries.
		w.Generator.RefreshAll(ctx)
		done <- struct{}{}
	}()

	<-done
	<-done

	slog.Info("forecast generation complete")
}

// GetForecastPeriodsInformation matches the handler structure
type GetForecastPeriodsInformation struct {
	Name      string `json:"name"`
	TimeOfDay string `json:"time_of_day"`
	Icon      string `json:"icon"`
	Beaufort  string `json:"beaufort"`
}

// JoinedForecastPeriodsInformation matches the handler structure
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

// GetForecastPeriodsInformationResponse matches the handler structure
type GetForecastPeriodsInformationResponse struct {
	Periods     []JoinedForecastPeriodsInformation `json:"periods"`
	LastUpdated time.Time                          `json:"last_updated"`
}

func (w *ForecastWorker) generateForecastPeriodsInformation(ctx context.Context) {
	timeoutCtx, cancel := context.WithTimeout(ctx, w.Timeout)
	defer cancel()

	periods, err := w.NWSClient.GetSimplifiedForecastNPeriods(w.GridPoint, -1)
	if err != nil {
		slog.Error("worker: failed to get simplified forecast periods", slog.String("error", err.Error()))
		return
	}

	periodsJSON, err := json.Marshal(periods)
	if err != nil {
		slog.Error("worker: failed to marshal simplified forecast periods", slog.String("error", err.Error()))
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

	prompt := `Input is a JSON array with one entry per forecast period.
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

	fewShotTraining := []multiShot{
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

	response, err := w.LLMProvider.Complete(timeoutCtx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   buildFinalPrompt(prompt, fewShotTraining, string(periodsJSON)),
		MaxTokens:    4096,
	})
	if err != nil {
		slog.Error("worker: failed to get forecast periods information", slog.String("error", err.Error()))
		return
	}

	var fpi []GetForecastPeriodsInformation
	cleanedText := stripMarkdownCodeBlock(response.Content)
	err = json.Unmarshal([]byte(cleanedText), &fpi)
	if err != nil {
		slog.Error("worker: failed to unmarshal forecast periods information", slog.String("error", err.Error()), slog.String("response", cleanedText))
		return
	}

	joinedPeriods := make([]JoinedForecastPeriodsInformation, 0)
	for _, period := range periods {
		for _, fpiPeriod := range fpi {
			if period.Name == fpiPeriod.Name {
				joinedPeriods = append(joinedPeriods, JoinedForecastPeriodsInformation{
					Name:             fpiPeriod.Name,
					TimeOfDay:        fpiPeriod.TimeOfDay,
					Icon:             fpiPeriod.Icon,
					Beaufort:         fpiPeriod.Beaufort,
					DetailedForecast: period.DetailedForecast,
					ShortForecast:    period.ShortForecast,
					StartTime:        period.StartTime,
					EndTime:          period.EndTime,
					Temperature:      period.Temperature,
					WindSpeed:        period.WindSpeed,
					WindDirection:    period.WindDirection,
				})
			}
		}
	}

	fpiResponse := GetForecastPeriodsInformationResponse{
		Periods:     joinedPeriods,
		LastUpdated: time.Now(),
	}

	fpiJson, err := json.Marshal(fpiResponse)
	if err != nil {
		slog.Error("worker: failed to marshal forecast periods information", slog.String("error", err.Error()))
		return
	}

	err = w.DragonflyClient.Client.Set(timeoutCtx, fmt.Sprintf("%s-%s", w.DragonflyClient.KeyPrefix, "forecast-periods-information"), fpiJson, w.DragonflyClient.CacheResultsDuration).Err()
	if err != nil {
		slog.Error("worker: could not set forecast periods information in cache", slog.String("error", err.Error()))
		return
	}

	slog.Info("worker: forecast periods information generated and cached")
}

// Helper types and functions (duplicated from handlers to avoid circular imports)

type multiShot struct {
	Input  string
	Output string
}

func multiShotWrapper(ms []multiShot) string {
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

func buildFinalPrompt(prompt string, ms []multiShot, inputData string) string {
	return fmt.Sprintf("%s\n\n%s\n\n%s", prompt, multiShotWrapper(ms), fmt.Sprintf("input: %s", inputData))
}

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
