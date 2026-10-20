package generate

import (
	"context"
	"encoding/json"
	"fmt"
)

// buildForecastSummary writes the headline forecast summary from the next
// three NWS forecast periods.
func (g *Generator) buildForecastSummary(ctx context.Context) (*Result, error) {
	periods, err := g.nws.GetSimplifiedForecastNPeriods(g.gridPoint, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to get forecast periods: %w", err)
	}

	inputJSON, err := json.Marshal(periods)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal forecast summary input: %w", err)
	}

	systemPrompt := fmt.Sprintf(`You are a tool that can provide concise summaries of weather forecasts.
You have access to the following list of icons:
"""
%s
"""
`, iconList)

	task := `Input is a JSON array with one entry per forecast period.
Output is a JSON object with the key "summary" containing the overall forecast in at most four sentences and "icon" containing the icon that best fits the soonest weather for this summary.
Each entry contains relavant weather information including a detailed text forecast.
Do not include any information that is not present in the input.
Do not comment twice on the same weather condition.
Focus mainly on the daytime periods.
Avoid editorializing or making assumptions.
Avoid referring to "periods" in the output.
Make the output sound like a human wrote it, with concise but friendly language and complete sentences.`

	shots := []shot{
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

	return g.complete(ctx, systemPrompt, task, shots, string(inputJSON))
}
