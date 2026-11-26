package solar

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

const (
	// HistoryEvery is the minimum time between two history fetches.
	HistoryEvery = 5 * time.Minute

	historyStartMin = 6 * 60
	historyStepMin  = 5
	historyUnitW    = 100
	historyMaxBins  = (22*60 - historyStartMin) / historyStepMin // 06:00-22:00
)

// Bin is the mean power of one 5-minute bin; OK is false when the bin had no data.
type Bin struct {
	W  int
	OK bool
}

// History is today's production and consumption in 5-minute bins from 06:00
// local time, plus totals since local midnight.
type History struct {
	Date      string // local YYYY-MM-DD
	PV, Load  []Bin
	PVWh      int
	LoadWh    int
	UpdatedAt time.Time
}

// MinutePoint is one per-minute power reading; missing minutes are absent.
type MinutePoint struct {
	At time.Time
	W  float64
}

// HistorySource is implemented by sources that can provide today's history.
type HistorySource interface {
	History(ctx context.Context) (History, error)
}

type historyJSON struct {
	Date      string `json:"date"`
	Start     string `json:"start"`
	StepMin   int    `json:"step_min"`
	UnitW     int    `json:"unit_w"`
	PV        []*int `json:"pv"`
	Load      []*int `json:"load"`
	PVWh      int    `json:"pv_wh"`
	LoadWh    int    `json:"load_wh"`
	UpdatedAt string `json:"updated_at"`
}

func units(bins []Bin) []*int {
	out := make([]*int, len(bins))
	for i, b := range bins {
		if b.OK {
			v := int(math.Round(float64(b.W) / historyUnitW))
			out[i] = &v
		}
	}
	return out
}

func (h History) MarshalJSON() ([]byte, error) {
	return json.Marshal(historyJSON{
		Date: h.Date, Start: "06:00", StepMin: historyStepMin, UnitW: historyUnitW,
		PV: units(h.PV), Load: units(h.Load),
		PVWh: h.PVWh, LoadWh: h.LoadWh,
		UpdatedAt: h.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// BuildHistory bins the points of day's local date into complete 5-minute
// bins from 06:00 (bin end <= now, at most 06:00-22:00) and sums the energy
// from local midnight until now.
func BuildHistory(points []MinutePoint, day time.Time, loc *time.Location, now time.Time) ([]Bin, int) {
	d := day.In(loc)
	midnight := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	next := midnight.AddDate(0, 0, 1)
	start := time.Date(d.Year(), d.Month(), d.Day(), 6, 0, 0, 0, loc)
	step := historyStepMin * time.Minute

	n := 0
	for n < historyMaxBins && !start.Add(time.Duration(n+1)*step).After(now) {
		n++
	}
	sum := make([]float64, n)
	cnt := make([]int, n)
	wh := 0.0
	for _, p := range points {
		if p.At.Before(midnight) || !p.At.Before(next) || !p.At.Before(now) {
			continue
		}
		wh += p.W / 60
		if p.At.Before(start) {
			continue
		}
		i := int(p.At.Sub(start) / step)
		if i < n {
			sum[i] += p.W
			cnt[i]++
		}
	}
	bins := make([]Bin, n)
	for i := range bins {
		if cnt[i] > 0 {
			bins[i] = Bin{W: int(math.Round(sum[i] / float64(cnt[i]))), OK: true}
		}
	}
	return bins, int(math.Round(wh))
}
