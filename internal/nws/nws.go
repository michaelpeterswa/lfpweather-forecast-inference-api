package nws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type NWSClient struct {
	httpClient *http.Client
}

type ForecastResponse struct {
	Context  []any  `json:"@context"`
	Type     string `json:"type"`
	Geometry struct {
		Type        string        `json:"type"`
		Coordinates [][][]float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		Units             string    `json:"units"`
		ForecastGenerator string    `json:"forecastGenerator"`
		GeneratedAt       time.Time `json:"generatedAt"`
		UpdateTime        time.Time `json:"updateTime"`
		ValidTimes        string    `json:"validTimes"`
		Elevation         struct {
			UnitCode string  `json:"unitCode"`
			Value    float64 `json:"value"`
		} `json:"elevation"`
		Periods []struct {
			Number                     int       `json:"number"`
			Name                       string    `json:"name"`
			StartTime                  time.Time `json:"startTime"`
			EndTime                    time.Time `json:"endTime"`
			IsDaytime                  bool      `json:"isDaytime"`
			Temperature                int       `json:"temperature"`
			TemperatureUnit            string    `json:"temperatureUnit"`
			TemperatureTrend           string    `json:"temperatureTrend"`
			ProbabilityOfPrecipitation struct {
				UnitCode string `json:"unitCode"`
				Value    int    `json:"value"`
			} `json:"probabilityOfPrecipitation"`
			WindSpeed        string `json:"windSpeed"`
			WindDirection    string `json:"windDirection"`
			Icon             string `json:"icon"`
			ShortForecast    string `json:"shortForecast"`
			DetailedForecast string `json:"detailedForecast"`
		} `json:"periods"`
	} `json:"properties"`
}

type SimplifiedForecastPeriods struct {
	DetailedForecast string    `json:"detailed_forecast"`
	ShortForecast    string    `json:"short_forecast"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	Temperature      int       `json:"temperature"`
	WindSpeed        string    `json:"wind_speed"`
	WindDirection    string    `json:"wind_direction"`
	Name             string    `json:"name"`
}

func NewNWSClient(httpClient *http.Client) *NWSClient {
	return &NWSClient{
		httpClient: httpClient,
	}
}

func (nc *NWSClient) GetForecast(gridpoints string) (ForecastResponse, error) {
	forecastURL := "https://api.weather.gov/gridpoints/" + gridpoints + "/forecast"
	slog.Info("getting forecast", slog.String("url", forecastURL))
	resp, err := nc.httpClient.Get(forecastURL)
	if err != nil {
		slog.Error("could not get forecast", slog.String("error", err.Error()))
		return ForecastResponse{}, err
	}
	defer resp.Body.Close()

	var forecast ForecastResponse
	if err := json.NewDecoder(resp.Body).Decode(&forecast); err != nil {
		slog.Error("could not decode forecast", slog.String("error", err.Error()))
		return ForecastResponse{}, err
	}

	return forecast, nil
}

func (nc *NWSClient) GetSimplifiedForecast(gridpoints string) ([]SimplifiedForecastPeriods, error) {
	forecast, err := nc.GetForecast(gridpoints)
	if err != nil {
		return nil, err
	}

	return forecastResponeToSimplifiedForecastPeriods(forecast), nil
}

func (nc *NWSClient) GetSimplifiedForecastNPeriods(gridpoints string, n int) ([]SimplifiedForecastPeriods, error) {
	forecast, err := nc.GetForecast(gridpoints)
	if err != nil {
		return nil, err
	}

	if n == -1 {
		return forecastResponeToSimplifiedForecastPeriods(forecast), nil
	}

	return forecastResponeToSimplifiedForecastPeriods(forecast)[:n], nil
}

func forecastResponeToSimplifiedForecastPeriods(forecast ForecastResponse) []SimplifiedForecastPeriods {
	var periods []SimplifiedForecastPeriods
	for _, period := range forecast.Properties.Periods {
		periods = append(periods, SimplifiedForecastPeriods{
			DetailedForecast: period.DetailedForecast,
			ShortForecast:    period.ShortForecast,
			StartTime:        period.StartTime,
			EndTime:          period.EndTime,
			Temperature:      period.Temperature,
			WindSpeed:        period.WindSpeed,
			WindDirection:    period.WindDirection,
			Name:             period.Name,
		})
	}
	return periods
}
