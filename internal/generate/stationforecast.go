package generate

import (
	"math"
	"time"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/lfpweather"
)

const (
	stationForecastDescription = "hyperlocal temperature forecast for the home weather station for the next 24 hours"
	stationTimeLayout          = "Mon 3 PM"
)

// stationForecastInput is the compact station forecast handed to the LLM.
type stationForecastInput struct {
	Description string `json:"description"`
	IssuedAt    string `json:"issued_at"`
	LowF        int    `json:"low_f"`
	LowAt       string `json:"low_at"`
	HighF       int    `json:"high_f"`
	HighAt      string `json:"high_at"`
	NWSLowF     *int   `json:"nws_low_f,omitempty"`
	NWSHighF    *int   `json:"nws_high_f,omitempty"`
}

// condenseStationForecast reduces a 24h station forecast to its low and high
// (with local times) and the NWS low and high over the same hours. Points
// before the current hour are dropped, since a run can be a few hours old by
// the time it is read. It returns nil when no points remain.
func condenseStationForecast(f lfpweather.TemperatureForecast, loc *time.Location, now time.Time) *stationForecastInput {
	currentHour := now.Truncate(time.Hour)
	points := make([]lfpweather.ForecastPoint, 0, len(f.Forecast))
	for _, p := range f.Forecast {
		if !p.ValidAt.Before(currentHour) {
			points = append(points, p)
		}
	}
	if len(points) == 0 {
		return nil
	}

	low, high := points[0], points[0]
	valid := make(map[time.Time]struct{}, len(points))
	for _, p := range points {
		valid[p.ValidAt.UTC()] = struct{}{}
		if p.Median < low.Median {
			low = p
		}
		if p.Median > high.Median {
			high = p
		}
	}

	out := &stationForecastInput{
		Description: stationForecastDescription,
		IssuedAt:    f.IssuedAt.In(loc).Format(stationTimeLayout),
		LowF:        roundF(low.Median),
		LowAt:       low.ValidAt.In(loc).Format(stationTimeLayout),
		HighF:       roundF(high.Median),
		HighAt:      high.ValidAt.In(loc).Format(stationTimeLayout),
	}

	var nwsLow, nwsHigh float64
	found := false
	for _, p := range f.NWS {
		if _, ok := valid[p.Time.UTC()]; !ok {
			continue
		}
		if !found {
			nwsLow, nwsHigh, found = p.Temperature, p.Temperature, true
			continue
		}
		nwsLow = math.Min(nwsLow, p.Temperature)
		nwsHigh = math.Max(nwsHigh, p.Temperature)
	}
	if found {
		l, h := roundF(nwsLow), roundF(nwsHigh)
		out.NWSLowF, out.NWSHighF = &l, &h
	}
	return out
}

func roundF(x float64) int { return int(math.Round(x)) }
