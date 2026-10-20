package nws

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const twoPeriods = `{"properties":{"periods":[
{"name":"Tonight","startTime":"2026-10-19T18:00:00-07:00","endTime":"2026-10-20T06:00:00-07:00","temperature":45},
{"name":"Tuesday","startTime":"2026-10-20T06:00:00-07:00","endTime":"2026-10-20T18:00:00-07:00","temperature":61}
]}}`

func newServer(t *testing.T, status int, body string) *NWSClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewNWSClientWithBaseURL(&http.Client{Timeout: time.Second}, srv.URL)
}

func TestGetForecastNon200IsError(t *testing.T) {
	c := newServer(t, http.StatusServiceUnavailable, `{"title":"unavailable"}`)
	if _, err := c.GetForecast("SEW/127,75"); err == nil {
		t.Error("GetForecast returned no error on 503")
	}
	if _, err := c.GetSimplifiedForecastNPeriods("SEW/127,75", 3); err == nil {
		t.Error("GetSimplifiedForecastNPeriods returned no error on 503")
	}
}

func TestGetSimplifiedForecastNPeriodsClampsToAvailable(t *testing.T) {
	c := newServer(t, http.StatusOK, twoPeriods)
	got, err := c.GetSimplifiedForecastNPeriods("SEW/127,75", 3)
	if err != nil {
		t.Fatalf("returned error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d periods, want 2", len(got))
	}

	got, err = c.GetSimplifiedForecastNPeriods("SEW/127,75", 1)
	if err != nil || len(got) != 1 {
		t.Errorf("n=1: got %d periods, err %v; want 1", len(got), err)
	}

	got, err = c.GetSimplifiedForecastNPeriods("SEW/127,75", -1)
	if err != nil || len(got) != 2 {
		t.Errorf("n=-1: got %d periods, err %v; want all 2", len(got), err)
	}
}
