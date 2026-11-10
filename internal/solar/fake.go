package solar

import (
	"context"
	"math"
	"time"
)

type fake struct {
	now func() time.Time
	loc *time.Location
}

// NewFake returns a deterministic, realistic-looking source driven only by
// the clock.
func NewFake(now func() time.Time, loc *time.Location) Source {
	return &fake{now: now, loc: loc}
}

func (f *fake) Name() string { return "fake" }

func round10(v float64) int { return int(math.Round(v/10)) * 10 }

func (f *fake) Read(ctx context.Context) (Snapshot, error) {
	now := f.now()
	t := now.In(f.loc)
	h := float64(t.Hour()) + float64(t.Minute())/60 + float64(t.Second())/3600

	// s: +1 at the June solstice, -1 at the December solstice.
	s := math.Cos(2 * math.Pi * float64(t.YearDay()-172) / 365)
	sunrise := 6.833 - 1.333*s
	sunset := 19.033 + 2.3*s
	peak := 4500 + 2000*s

	pv := 0.0
	if h > sunrise && h < sunset {
		pv = peak * math.Sin(math.Pi*(h-sunrise)/(sunset-sunrise))
	}

	bump := func(centre float64) float64 {
		d := (h - centre) * 60
		return 900 * math.Exp(-d*d/(2*45*45))
	}
	load := 350 + bump(7) + bump(18.5)

	soc := socAt(h)

	pvW, loadW := round10(pv), round10(load)
	surplus := pvW - loadW
	battery := 0
	switch {
	case surplus > 0 && soc < 100:
		battery = min(surplus, 3000)
	case surplus < 0 && soc > 20:
		battery = -min(-surplus, 3000)
	}

	return Snapshot{
		PVW:        pvW,
		LoadW:      loadW,
		GridW:      loadW + battery - pvW,
		BatterySOC: soc,
		BatteryW:   battery,
		UpdatedAt:  now.UTC(),
		Source:     "fake",
	}, nil
}

// socAt: 20 % at 06:00 rising to 95 % at 15:00, then falling to 20 % by 06:00.
func socAt(h float64) int {
	var v float64
	switch {
	case h >= 6 && h <= 15:
		v = 20 + 75*(h-6)/9
	case h > 15:
		v = 95 - 75*(h-15)/15
	default:
		v = 95 - 75*(h+9)/15
	}
	return int(math.Round(math.Max(0, math.Min(100, v))))
}
