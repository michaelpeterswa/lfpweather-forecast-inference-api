package generate

import (
	"context"
	"encoding/json"
	"fmt"
)

// currentConditionsInput is the latest station reading, framed for the model.
type currentConditionsInput struct {
	TemperatureF      float64 `json:"temperature_f"`
	HumidityPct       float64 `json:"relative_humidity_pct"`
	WindSpeedMph      float64 `json:"wind_speed_mph"`
	PressureInHg      float64 `json:"pressure_inhg"`
	SolarRadiationWm2 float64 `json:"solar_radiation_wm2"`
	UVIndex           float64 `json:"uv_index"`
	Rain24hIn         float64 `json:"rain_last_24h_in"`
	ObservedAt        string  `json:"observed_at"`
}

// buildCurrentConditions writes a short "right now" sentence from the latest
// station observations.
func (g *Generator) buildCurrentConditions(ctx context.Context) (*Result, error) {
	temperature, err := g.lfp.TemperatureLast(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get temperature: %w", err)
	}
	humidity, err := g.lfp.HumidityLast(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get humidity: %w", err)
	}
	windSpeed, err := g.lfp.WindSpeedLast(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get wind speed: %w", err)
	}
	pressure, err := g.lfp.PressureLast(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pressure: %w", err)
	}
	solar, err := g.lfp.SolarRadiationLast(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get solar radiation: %w", err)
	}
	uv, err := g.lfp.UVIndexLast(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get uv index: %w", err)
	}
	rain, err := g.lfp.RainLast24h(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get 24h rain: %w", err)
	}

	input := currentConditionsInput{
		TemperatureF:      temperature.Last,
		HumidityPct:       humidity.Last,
		WindSpeedMph:      windSpeed.Last,
		PressureInHg:      pressure.Last,
		SolarRadiationWm2: solar.Last,
		UVIndex:           uv.Last,
		Rain24hIn:         rain.Last,
		ObservedAt:        temperature.Time.Format("2006-01-02T15:04:05Z07:00"),
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal current conditions input: %w", err)
	}

	systemPrompt := fmt.Sprintf(`You are a tool that describes the current weather at a backyard weather station near Seattle, Washington.
You have access to the following list of icons:
"""
%s
"""
`, iconList)

	task := `Input is a JSON object with the latest sensor readings from the station.
Output is a JSON object with the key "summary" and the key "icon".
"summary" is a single friendly sentence, or at most two, that describes the conditions right now.
"icon" is the icon that best fits the current conditions.
Report the temperature, and then the one or two other readings that stand out (for example strong wind, high humidity, recent rain, or strong sun).
Use the solar radiation and the UV index to judge how sunny it is; a low solar radiation value means it is cloudy, dark, or night.
Do not include any information that is not present in the input.
Do not list every reading.
Avoid editorializing or making assumptions.
Make the output sound like a human wrote it, with concise but friendly language and complete sentences.`

	shots := []shot{
		{
			Input:  `{"temperature_f":72.4,"relative_humidity_pct":41,"wind_speed_mph":8,"pressure_inhg":30.05,"solar_radiation_wm2":710,"uv_index":6,"rain_last_24h_in":0,"observed_at":"2024-07-14T14:20:00-07:00"}`,
			Output: `{"summary": "It is a warm 72 degrees and sunny right now, with a light breeze and comfortable humidity.", "icon": "sun"}`,
		},
	}

	return g.complete(ctx, systemPrompt, task, shots, string(inputJSON))
}

// smokeOutlookInput pairs the latest air quality reading with the near-term
// NWS forecast text, which is where smoke and haze are called out.
type smokeOutlookInput struct {
	AQINow          float64          `json:"aqi_now"`
	AQIObservedAt   string           `json:"aqi_observed_at"`
	ForecastPeriods []forecastPeriod `json:"forecast_periods"`
}

// forecastPeriod is the part of an NWS period the smoke outlook needs.
type forecastPeriod struct {
	Name             string `json:"name"`
	DetailedForecast string `json:"detailed_forecast"`
}

// buildSmokeOutlook grounds the "now" in the AirGradient AQI reading and uses
// the NWS forecast text for the forward outlook.
func (g *Generator) buildSmokeOutlook(ctx context.Context) (*Result, error) {
	aqi, err := g.lfp.AQILast(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get aqi: %w", err)
	}

	periods, err := g.nws.GetSimplifiedForecastNPeriods(g.gridPoint, 4)
	if err != nil {
		return nil, fmt.Errorf("failed to get forecast periods: %w", err)
	}

	input := smokeOutlookInput{
		AQINow:        aqi.Last,
		AQIObservedAt: aqi.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	for _, p := range periods {
		input.ForecastPeriods = append(input.ForecastPeriods, forecastPeriod{
			Name:             p.Name,
			DetailedForecast: p.DetailedForecast,
		})
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal smoke outlook input: %w", err)
	}

	systemPrompt := fmt.Sprintf(`You are a tool that describes the wildfire smoke and air quality outlook near Seattle, Washington.
You have access to the following list of icons:
"""
%s
"""
`, iconList)

	task := `Input is a JSON object.
"aqi_now" is the current air quality index from a local sensor. Lower is cleaner: 0 to 50 is good, 51 to 100 is moderate, 101 to 150 is unhealthy for sensitive groups, and above 150 is unhealthy.
"forecast_periods" is the near-term NWS forecast text, where smoke and haze are called out.
Output is a JSON object with the key "summary" and the key "icon".
"summary" is a single sentence, or at most two, that describes the air quality now and whether smoke or haze is expected to build or clear.
Use "aqi_now" to describe the air right now.
Read the forecast text for words like smoke, haze, or wildfire to describe the outlook.
If the air is clean now and the forecast does not mention smoke, say the air is clear and no smoke is expected.
"icon" is "cloud-fog" when the air is smoky or hazy, and "sun" when the air is clear.
Do not include any information that is not present in the input.
Do not give health advice.
Avoid editorializing or making assumptions.
Make the output sound like a human wrote it, with concise but friendly language and complete sentences.`

	shots := []shot{
		{
			Input:  `{"aqi_now":18,"aqi_observed_at":"2024-08-02T10:00:00-07:00","forecast_periods":[{"name":"Today","detailed_forecast":"Sunny, with a high near 82. Light west wind."}]}`,
			Output: `{"summary": "Air quality is good right now with an AQI of 18, and no smoke is expected today.", "icon": "sun"}`,
		},
		{
			Input:  `{"aqi_now":126,"aqi_observed_at":"2024-08-20T13:00:00-07:00","forecast_periods":[{"name":"This Afternoon","detailed_forecast":"Areas of smoke after 2pm. Sunny and hazy, with a high near 90."}]}`,
			Output: `{"summary": "Air quality is unhealthy for sensitive groups at an AQI of 126, and areas of smoke are expected to move in this afternoon.", "icon": "cloud-fog"}`,
		},
	}

	return g.complete(ctx, systemPrompt, task, shots, string(inputJSON))
}

// fireWeatherInput is the part of the fire danger summary the read needs.
type fireWeatherInput struct {
	Class         string  `json:"danger_class"`
	Headline      string  `json:"headline"`
	ERCValue      float64 `json:"erc_value"`
	ERCPercentile float64 `json:"erc_percentile"`
	BurningIndex  float64 `json:"burning_index"`
	KBDI          float64 `json:"kbdi_drought_0_to_800"`
	GSI           float64 `json:"greenup_0_to_1"`
	Dead10hrPct   float64 `json:"dead_10hr_moisture_pct"`
	Dead100hrPct  float64 `json:"dead_100hr_moisture_pct"`
	LiveHerbPct   float64 `json:"live_herbaceous_moisture_pct"`
	ObservedAt    string  `json:"observed_at"`
}

// buildFireWeather writes a plain-language read of today's fire danger from the
// lfpweather-api fire danger summary.
func (g *Generator) buildFireWeather(ctx context.Context) (*Result, error) {
	summary, err := g.lfp.FireDangerSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get fire danger summary: %w", err)
	}

	input := fireWeatherInput{
		Class:         summary.Rating.Class,
		Headline:      summary.Rating.Headline,
		ERCValue:      summary.EnergyRelease.Value,
		ERCPercentile: summary.EnergyRelease.Percentile,
		BurningIndex:  summary.BurningIndex.Value,
		KBDI:          summary.Drought.KBDI,
		GSI:           summary.Drought.GSI,
		Dead10hrPct:   summary.FuelMoisture.Dead10hr,
		Dead100hrPct:  summary.FuelMoisture.Dead100hr,
		LiveHerbPct:   summary.FuelMoisture.LiveHerb,
		ObservedAt:    summary.Time.Format("2006-01-02T15:04:05Z07:00"),
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fire weather input: %w", err)
	}

	systemPrompt := fmt.Sprintf(`You are a tool that describes today's fire weather near Seattle, Washington for a general audience.
You have access to the following list of icons:
"""
%s
"""
`, iconList)

	task := `Input is a JSON object with today's fire danger for the station.
"danger_class" is the danger rating, such as Low, Moderate, High, Very High, or Extreme.
"erc_percentile" ranks today's energy release component against the season, from 0 to 1; a high percentile means the fuels hold more energy than usual for this time of year.
"kbdi_drought_0_to_800" is a drought index; a higher value means drier soil.
"greenup_0_to_1" describes live plant growth; a value near 1 means the plants are green.
The moisture values are the water content of dead and live fuels, in percent; lower values mean drier fuels.
Output is a JSON object with the key "summary" and the key "icon".
"summary" is a single sentence, or at most two, that states the danger class and the one or two conditions that drive it, such as dry fuels, drought, or green plant growth.
Say whether today is high or low for the season, using the percentile.
"icon" is "thermometer-sun" when the danger is High or above, and "sun" when the danger is Moderate or below.
Do not include any information that is not present in the input.
Do not give safety instructions.
Avoid jargon and avoid editorializing.
Make the output sound like a human wrote it, with concise but friendly language and complete sentences.`

	shots := []shot{
		{
			Input:  `{"danger_class":"Low","headline":"Low fire danger","erc_value":12,"erc_percentile":0.22,"burning_index":8,"kbdi_drought_0_to_800":90,"greenup_0_to_1":0.85,"dead_10hr_moisture_pct":18,"dead_100hr_moisture_pct":22,"live_herbaceous_moisture_pct":140,"observed_at":"2024-05-10T13:00:00-07:00"}`,
			Output: `{"summary": "Fire danger is low today, which is typical for this time of year, with damp fuels and green plant growth keeping conditions mild.", "icon": "sun"}`,
		},
		{
			Input:  `{"danger_class":"Very High","headline":"Very high fire danger","erc_value":58,"erc_percentile":0.95,"burning_index":72,"kbdi_drought_0_to_800":540,"greenup_0_to_1":0.2,"dead_10hr_moisture_pct":6,"dead_100hr_moisture_pct":8,"live_herbaceous_moisture_pct":45,"observed_at":"2024-08-18T13:00:00-07:00"}`,
			Output: `{"summary": "Fire danger is very high today and near the top of the range for the season, driven by very dry fuels and deep drought.", "icon": "thermometer-sun"}`,
		},
	}

	return g.complete(ctx, systemPrompt, task, shots, string(inputJSON))
}
