package lfpweather

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLastEndpoints(t *testing.T) {
	cases := []struct {
		name     string
		wantPath string
		call     func(*Client, context.Context) (LastReading, error)
	}{
		{"temperature", "/api/v1/temperature/last", (*Client).TemperatureLast},
		{"humidity", "/api/v1/humidity/last", (*Client).HumidityLast},
		{"pressure", "/api/v1/pressure/last", (*Client).PressureLast},
		{"wind_speed", "/api/v1/wind_speed/last", (*Client).WindSpeedLast},
		{"solar_radiation", "/api/v1/solar_radiation/last", (*Client).SolarRadiationLast},
		{"uv_index", "/api/v1/uv_index/last", (*Client).UVIndexLast},
		{"24h_rain", "/api/v1/24h_rain/last", (*Client).RainLast24h},
		{"aqi", "/api/v1/aqi/last", (*Client).AQILast},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotKey string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotKey = r.Header.Get("X-API-Key")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"time":"2024-08-02T10:00:00Z","last":42.5}`))
			}))
			defer srv.Close()

			client := NewClient(srv.Client(), srv.URL, "secret-key")
			got, err := tc.call(client, context.Background())
			if err != nil {
				t.Fatalf("call returned error: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tc.wantPath)
			}
			if gotKey != "secret-key" {
				t.Errorf("X-API-Key = %q, want %q", gotKey, "secret-key")
			}
			if got.Last != 42.5 {
				t.Errorf("Last = %v, want 42.5", got.Last)
			}
			if !got.Time.Equal(time.Date(2024, 8, 2, 10, 0, 0, 0, time.UTC)) {
				t.Errorf("Time = %v, want 2024-08-02T10:00:00Z", got.Time)
			}
		})
	}
}

func TestFireDangerSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fire_danger/summary" {
			t.Errorf("path = %q, want /api/v1/fire_danger/summary", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"time":"2024-08-18T13:00:00Z",
			"fuel_model":"G",
			"rating":{"class":"Very High","level":4,"headline":"Very high fire danger","detail":"d"},
			"energy_release":{"value":58,"percentile":0.95,"p50":30,"p80":45,"p90":52,"p97":60},
			"burning_index":{"value":72},
			"drought":{"kbdi":540,"gsi":0.2},
			"fuel_moisture":{"dead_1hr":5,"dead_10hr":6,"dead_100hr":8,"live_herbaceous":45,"live_woody":80}
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.Client(), srv.URL, "")
	got, err := client.FireDangerSummary(context.Background())
	if err != nil {
		t.Fatalf("FireDangerSummary returned error: %v", err)
	}
	if got.Rating.Class != "Very High" {
		t.Errorf("Rating.Class = %q, want Very High", got.Rating.Class)
	}
	if got.EnergyRelease.Percentile != 0.95 {
		t.Errorf("EnergyRelease.Percentile = %v, want 0.95", got.EnergyRelease.Percentile)
	}
	if got.Drought.KBDI != 540 {
		t.Errorf("Drought.KBDI = %v, want 540", got.Drought.KBDI)
	}
	if got.FuelMoisture.Dead10hr != 6 {
		t.Errorf("FuelMoisture.Dead10hr = %v, want 6", got.FuelMoisture.Dead10hr)
	}
}

func TestGetNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.Client(), srv.URL, "")
	if _, err := client.TemperatureLast(context.Background()); err == nil {
		t.Fatal("expected error on non-200 status, got nil")
	}
}

func TestNewClientTrimsTrailingSlash(t *testing.T) {
	client := NewClient(http.DefaultClient, "https://example.com/", "k")
	if client.baseURL != "https://example.com" {
		t.Errorf("baseURL = %q, want https://example.com", client.baseURL)
	}
}

func TestTemperatureForecastLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/temperature_forecast/latest" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "k" {
			t.Errorf("X-API-Key = %q", got)
		}
		_, _ = w.Write([]byte(`{"issued_at":"2026-10-20T01:00:00Z","source":"timestral","model":"m",
"forecast":[{"valid_at":"2026-10-20T02:00:00Z","lead_hours":1,"median":49.9,"p10":49.3,"p90":50.4}],
"nws":[{"time":"2026-10-20T02:00:00Z","temperature":53}],
"observed":[{"time":"2026-10-19T13:00:00Z","temperature":45.9}]}`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.Client(), srv.URL, "k").TemperatureForecastLatest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Source != "timestral" || !got.IssuedAt.Equal(time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)) {
		t.Errorf("header fields = %+v", got)
	}
	if len(got.Forecast) != 1 || got.Forecast[0].LeadHours != 1 || got.Forecast[0].Median != 49.9 {
		t.Errorf("forecast = %+v", got.Forecast)
	}
	if len(got.NWS) != 1 || got.NWS[0].Temperature != 53 {
		t.Errorf("nws = %+v", got.NWS)
	}
}

func TestTemperatureForecastLatestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := NewClient(srv.Client(), srv.URL, "k").TemperatureForecastLatest(context.Background())
	if !errors.Is(err, ErrNoForecast) {
		t.Errorf("err = %v, want ErrNoForecast", err)
	}
}
