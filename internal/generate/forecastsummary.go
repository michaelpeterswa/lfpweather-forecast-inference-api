package generate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/lfpweather"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
)

// forecastSummaryInput is the forecast summary input: the next three NWS
// periods and, when the lfpweather-api has a fresh one, the condensed station
// temperature forecast.
type forecastSummaryInput struct {
	Periods         []nws.SimplifiedForecastPeriods `json:"periods"`
	StationForecast *stationForecastInput           `json:"station_forecast,omitempty"`
}

// buildForecastSummary writes the headline forecast summary from the next
// three NWS forecast periods and, when available, the station forecast.
func (g *Generator) buildForecastSummary(ctx context.Context) (*Result, error) {
	periods, err := g.nws.GetSimplifiedForecastNPeriods(g.gridPoint, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to get forecast periods: %w", err)
	}

	input := forecastSummaryInput{
		Periods:         periods,
		StationForecast: g.stationForecast(ctx),
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal forecast summary input: %w", err)
	}

	systemPrompt := fmt.Sprintf(`You are a tool that can provide concise summaries of weather forecasts.
You have access to the following list of icons:
"""
%s
"""
`, iconList)

	task := `Input is a JSON object with the key "periods" and, sometimes, the key "station_forecast".
"periods" is a JSON array with one entry per forecast period. Each entry contains relevant weather information including a detailed text forecast.
"station_forecast", when present, is the hyperlocal temperature forecast for the home weather station over the next 24 hours: "low_f" and "high_f" are the forecast low and high in degrees Fahrenheit, "low_at" and "high_at" are when they occur in local time, and "nws_low_f" and "nws_high_f", when present, are the NWS low and high over the same hours.
Output is a JSON object with the key "summary" containing the overall forecast in at most four sentences and "icon" containing the icon that best fits the soonest weather for this summary.
Do not include any information that is not present in the input.
Do not comment twice on the same weather condition.
Focus mainly on the daytime periods.
Avoid editorializing or making assumptions.
Avoid referring to "periods" in the output.
If "station_forecast" is present, add at most one sentence about the station's forecast low or high and when it occurs. Mention the NWS forecast in that sentence only if the station's value differs from the matching NWS value by 2°F or more.
If "station_forecast" is absent, never mention a station forecast.
Make the output sound like a human wrote it, with concise but friendly language and complete sentences.`

	shots := []shot{
		{
			Input: `{"periods": [
	{"name": "Tonight", "start_time": "2024-06-08T20:00:00-07:00", "end_time": "2024-06-09T06:00:00-07:00", "temperature": 54, "detailed_forecast": "Mostly cloudy, with a low around 54. East wind around 2 mph.", "wind_speed": "2 mph", "wind_direction": "E"},
	{"name": "Sunday", "start_time": "2024-06-09T06:00:00-07:00", "end_time": "2024-06-09T18:00:00-07:00", "temperature": 74, "detailed_forecast": "Mostly sunny. High near 74, with temperatures falling to around 72 in the afternoon. Southwest wind 1 to 6 mph.", "wind_speed": "1 to 6 mph", "wind_direction": "SW"},
	{"name": "Sunday Night", "start_time": "2024-06-09T18:00:00-07:00", "end_time": "2024-06-10T06:00:00-07:00", "temperature": 51, "detailed_forecast": "Mostly cloudy, with a low around 51. West wind 2 to 6 mph.", "wind_speed": "2 to 6 mph", "wind_direction": "W"}
]}`,
			Output: `{"summary": "Tonight, mostly cloudy with a low around 54. Sunday, mostly sunny with a high near 74, temperatures falling to around 72 in the afternoon. Sunday night, mostly cloudy with a low around 51. Winds light and variable.", "icon": "cloud-moon"}`,
		},
		{
			Input: `{"periods": [
	{"name": "Tonight", "start_time": "2024-10-21T18:00:00-07:00", "end_time": "2024-10-22T06:00:00-07:00", "temperature": 45, "detailed_forecast": "Mostly clear, with a low around 45. North wind around 3 mph.", "wind_speed": "3 mph", "wind_direction": "N"},
	{"name": "Tuesday", "start_time": "2024-10-22T06:00:00-07:00", "end_time": "2024-10-22T18:00:00-07:00", "temperature": 61, "detailed_forecast": "Sunny, with a high near 61. Light and variable wind.", "wind_speed": "2 to 5 mph", "wind_direction": "NW"},
	{"name": "Tuesday Night", "start_time": "2024-10-22T18:00:00-07:00", "end_time": "2024-10-23T06:00:00-07:00", "temperature": 46, "detailed_forecast": "Increasing clouds, with a low around 46. South wind around 4 mph.", "wind_speed": "4 mph", "wind_direction": "S"}
], "station_forecast": {"description": "hyperlocal temperature forecast for the home weather station for the next 24 hours", "issued_at": "Mon 5 PM", "low_f": 41, "low_at": "Tue 6 AM", "high_f": 60, "high_at": "Tue 3 PM", "nws_low_f": 45, "nws_high_f": 61}}`,
			Output: `{"summary": "Clear and cool tonight with a low around 45. Tuesday looks sunny with a high near 61 and light winds. Clouds build Tuesday night with a low around 46. The backyard station expects a colder start, bottoming out at 41 around 6 AM Tuesday versus 45 from the NWS.", "icon": "cloud-moon"}`,
		},
		{
			Input: `{"periods": [
	{"name": "Today", "start_time": "2024-08-10T06:00:00-07:00", "end_time": "2024-08-10T18:00:00-07:00", "temperature": 69, "detailed_forecast": "Partly sunny, with a high near 69. Southwest wind 3 to 7 mph.", "wind_speed": "3 to 7 mph", "wind_direction": "SW"},
	{"name": "Tonight", "start_time": "2024-08-10T18:00:00-07:00", "end_time": "2024-08-11T06:00:00-07:00", "temperature": 53, "detailed_forecast": "Mostly cloudy, with a low around 53. Southwest wind around 5 mph.", "wind_speed": "5 mph", "wind_direction": "SW"},
	{"name": "Sunday", "start_time": "2024-08-11T06:00:00-07:00", "end_time": "2024-08-11T18:00:00-07:00", "temperature": 72, "detailed_forecast": "Mostly sunny, with a high near 72. West wind 2 to 6 mph.", "wind_speed": "2 to 6 mph", "wind_direction": "W"}
], "station_forecast": {"description": "hyperlocal temperature forecast for the home weather station for the next 24 hours", "issued_at": "Sat 5 AM", "low_f": 52, "low_at": "Sun 5 AM", "high_f": 68, "high_at": "Sat 3 PM", "nws_low_f": 53, "nws_high_f": 69}}`,
			Output: `{"summary": "Partly sunny today with a high near 69 and a light southwest breeze. Tonight turns mostly cloudy with a low around 53. Sunday brings more sun and a high near 72. The backyard station expects today's high of 68 around 3 PM.", "icon": "cloud-sun"}`,
		},
	}

	return g.complete(ctx, systemPrompt, task, shots, string(inputJSON))
}

// stationForecast fetches and condenses the latest station temperature
// forecast. Any failure (no fresh run, an error, a timeout) is logged and
// returns nil, so the summary is written from the NWS periods alone.
func (g *Generator) stationForecast(ctx context.Context) *stationForecastInput {
	f, err := g.lfp.TemperatureForecastLatest(ctx)
	if errors.Is(err, lfpweather.ErrNoForecast) {
		slog.Info("no fresh station forecast, summarizing without it")
		return nil
	}
	if err != nil {
		slog.Warn("could not get station forecast, summarizing without it", slog.String("error", err.Error()))
		return nil
	}
	return condenseStationForecast(f, g.loc)
}
