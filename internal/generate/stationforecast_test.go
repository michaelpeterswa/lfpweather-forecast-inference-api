package generate

import (
	"testing"
	"time"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/lfpweather"
)

func testLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// buildForecast makes 24 hourly points starting at issued+1h with the given medians.
func buildForecast(issued time.Time, medians []float64) lfpweather.TemperatureForecast {
	f := lfpweather.TemperatureForecast{IssuedAt: issued, Source: "test"}
	for i, m := range medians {
		f.Forecast = append(f.Forecast, lfpweather.ForecastPoint{
			ValidAt:   issued.Add(time.Duration(i+1) * time.Hour),
			LeadHours: i + 1,
			Median:    m,
		})
	}
	return f
}

func flat(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestCondenseLowHighAndTimes(t *testing.T) {
	loc := testLoc(t)
	// Issued Mon 2026-10-19 23:00 PDT -> lead 7 is Tue 06:00 PDT.
	issued := time.Date(2026, 10, 19, 23, 0, 0, 0, loc)
	m := flat(24, 50)
	m[6] = 41  // lead 7 = Tue 6 AM
	m[15] = 60 // lead 16 = Tue 3 PM
	f := buildForecast(issued, m)
	got := condenseStationForecast(f, loc)
	if got == nil {
		t.Fatal("nil")
	}
	if got.LowF != 41 || got.LowAt != "Tue 6 AM" {
		t.Errorf("low = %d %q", got.LowF, got.LowAt)
	}
	if got.HighF != 60 || got.HighAt != "Tue 3 PM" {
		t.Errorf("high = %d %q", got.HighF, got.HighAt)
	}
	if got.IssuedAt != "Mon 11 PM" {
		t.Errorf("issued = %q", got.IssuedAt)
	}
	if got.Description != "hyperlocal temperature forecast for the home weather station for the next 24 hours" {
		t.Errorf("description = %q", got.Description)
	}
}

func TestCondenseEdgeHours(t *testing.T) {
	loc := testLoc(t)
	issued := time.Date(2026, 10, 19, 23, 0, 0, 0, loc)
	m := flat(24, 50)
	m[0] = 30
	m[23] = 70
	got := condenseStationForecast(buildForecast(issued, m), loc)
	if got.LowF != 30 || got.LowAt != "Tue 12 AM" {
		t.Errorf("low = %d %q", got.LowF, got.LowAt)
	}
	if got.HighF != 70 || got.HighAt != "Tue 11 PM" {
		t.Errorf("high = %d %q", got.HighF, got.HighAt)
	}
}

func TestCondenseNoNWS(t *testing.T) {
	loc := testLoc(t)
	issued := time.Date(2026, 10, 19, 23, 0, 0, 0, loc)
	got := condenseStationForecast(buildForecast(issued, flat(24, 50)), loc)
	if got.NWSLowF != nil || got.NWSHighF != nil {
		t.Errorf("expected nil NWS, got %v %v", got.NWSLowF, got.NWSHighF)
	}
}

func TestCondenseNWSMatchesOnlyForecastHours(t *testing.T) {
	loc := testLoc(t)
	issued := time.Date(2026, 10, 19, 23, 0, 0, 0, loc)
	f := buildForecast(issued, flat(24, 50))
	f.NWS = []lfpweather.TemperaturePoint{
		{Time: issued.Add(-5 * time.Hour), Temperature: 10}, // outside window
		{Time: issued.Add(3 * time.Hour), Temperature: 45.4},
		{Time: issued.Add(9 * time.Hour), Temperature: 58.6},
		{Time: issued.Add(100 * time.Hour), Temperature: 99},
	}
	got := condenseStationForecast(f, loc)
	if got.NWSLowF == nil || *got.NWSLowF != 45 || got.NWSHighF == nil || *got.NWSHighF != 59 {
		t.Errorf("nws = %v %v", got.NWSLowF, got.NWSHighF)
	}
}

func TestCondenseRounding(t *testing.T) {
	loc := testLoc(t)
	issued := time.Date(2026, 10, 19, 23, 0, 0, 0, loc)
	m := flat(24, 50)
	m[0] = 41.5
	m[1] = 59.4
	got := condenseStationForecast(buildForecast(issued, m), loc)
	if got.LowF != 42 || got.HighF != 59 {
		t.Errorf("low/high = %d/%d", got.LowF, got.HighF)
	}
	m[0] = 41.4
	got = condenseStationForecast(buildForecast(issued, m), loc)
	if got.LowF != 41 {
		t.Errorf("low = %d", got.LowF)
	}
}

func TestCondenseEmpty(t *testing.T) {
	if got := condenseStationForecast(lfpweather.TemperatureForecast{}, testLoc(t)); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
