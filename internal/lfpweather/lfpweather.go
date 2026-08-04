// Package lfpweather is a small client for the lfpweather-api. The inference
// API uses it to read the latest station observations and the fire danger
// summary, which feed the current conditions, smoke outlook, and fire weather
// summaries.
package lfpweather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client calls the lfpweather-api. It sends the configured API key in the
// X-API-Key header on every request.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewClient returns a Client for the lfpweather-api at baseURL. A trailing
// slash on baseURL is removed. The apiKey may be empty when the target does
// not require authentication.
func NewClient(httpClient *http.Client, baseURL, apiKey string) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
	}
}

// LastReading is the {time, last} shape that every lfpweather-api "/last"
// endpoint returns.
type LastReading struct {
	Time time.Time `json:"time"`
	Last float64   `json:"last"`
}

// FireDangerSummary is the subset of the lfpweather-api fire danger summary
// that the fire weather generator needs. The full response has more fields;
// this client decodes only the ones it uses.
type FireDangerSummary struct {
	Time      time.Time `json:"time"`
	FuelModel string    `json:"fuel_model"`
	Rating    struct {
		Class    string `json:"class"`
		Level    int    `json:"level"`
		Headline string `json:"headline"`
		Detail   string `json:"detail"`
	} `json:"rating"`
	EnergyRelease struct {
		Value      float64 `json:"value"`
		Percentile float64 `json:"percentile"`
		P50        float64 `json:"p50"`
		P80        float64 `json:"p80"`
		P90        float64 `json:"p90"`
		P97        float64 `json:"p97"`
	} `json:"energy_release"`
	BurningIndex struct {
		Value float64 `json:"value"`
	} `json:"burning_index"`
	Drought struct {
		KBDI float64 `json:"kbdi"`
		GSI  float64 `json:"gsi"`
	} `json:"drought"`
	FuelMoisture struct {
		Dead1hr   float64 `json:"dead_1hr"`
		Dead10hr  float64 `json:"dead_10hr"`
		Dead100hr float64 `json:"dead_100hr"`
		LiveHerb  float64 `json:"live_herbaceous"`
		LiveWoody float64 `json:"live_woody"`
	} `json:"fuel_moisture"`
}

// get performs an authenticated GET and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to build request for %s: %w", path, err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, path)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode %s: %w", path, err)
	}
	return nil
}

// getLast reads a single {time, last} endpoint.
func (c *Client) getLast(ctx context.Context, path string) (LastReading, error) {
	var lr LastReading
	if err := c.get(ctx, path, &lr); err != nil {
		return LastReading{}, err
	}
	return lr, nil
}

// TemperatureLast returns the latest temperature reading in degrees Fahrenheit.
func (c *Client) TemperatureLast(ctx context.Context) (LastReading, error) {
	return c.getLast(ctx, "/api/v1/temperature/last")
}

// HumidityLast returns the latest relative humidity reading in percent.
func (c *Client) HumidityLast(ctx context.Context) (LastReading, error) {
	return c.getLast(ctx, "/api/v1/humidity/last")
}

// PressureLast returns the latest barometric pressure reading in inches of
// mercury.
func (c *Client) PressureLast(ctx context.Context) (LastReading, error) {
	return c.getLast(ctx, "/api/v1/pressure/last")
}

// WindSpeedLast returns the latest wind speed reading in miles per hour.
func (c *Client) WindSpeedLast(ctx context.Context) (LastReading, error) {
	return c.getLast(ctx, "/api/v1/wind_speed/last")
}

// SolarRadiationLast returns the latest solar radiation reading in watts per
// square meter.
func (c *Client) SolarRadiationLast(ctx context.Context) (LastReading, error) {
	return c.getLast(ctx, "/api/v1/solar_radiation/last")
}

// UVIndexLast returns the latest UV index reading.
func (c *Client) UVIndexLast(ctx context.Context) (LastReading, error) {
	return c.getLast(ctx, "/api/v1/uv_index/last")
}

// RainLast24h returns the accumulated rainfall over the last 24 hours in
// inches.
func (c *Client) RainLast24h(ctx context.Context) (LastReading, error) {
	return c.getLast(ctx, "/api/v1/24h_rain/last")
}

// AQILast returns the latest air quality index reading.
func (c *Client) AQILast(ctx context.Context) (LastReading, error) {
	return c.getLast(ctx, "/api/v1/aqi/last")
}

// FireDangerSummary returns the latest fire danger summary.
func (c *Client) FireDangerSummary(ctx context.Context) (FireDangerSummary, error) {
	var s FireDangerSummary
	if err := c.get(ctx, "/api/v1/fire_danger/summary", &s); err != nil {
		return FireDangerSummary{}, err
	}
	return s, nil
}
