package solar

import (
	"encoding/json"
	"testing"
	"time"
)

func zrh(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	return loc
}

// minutes returns one point per minute in [from, to) local, all at w watts.
func minutes(loc *time.Location, day string, from, to string, w float64) []MinutePoint {
	start, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+from, loc)
	end, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+to, loc)
	var pts []MinutePoint
	for t := start; t.Before(end); t = t.Add(time.Minute) {
		pts = append(pts, MinutePoint{At: t, W: w})
	}
	return pts
}

func at(t *testing.T, loc *time.Location, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHistoryJSON(t *testing.T) {
	h := History{
		Date:      "2026-11-26",
		PV:        []Bin{{1234, true}, {0, false}},
		Load:      []Bin{{850, true}, {0, false}},
		PVWh:      40460,
		LoadWh:    11800,
		UpdatedAt: time.Date(2026, 11, 26, 19, 30, 0, 0, time.UTC),
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"date":"2026-11-26","start":"06:00","step_min":5,"unit_w":100,"pv":[12,null],"load":[9,null],"pv_wh":40460,"load_wh":11800,"updated_at":"2026-11-26T19:30:00Z"}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestBinDayMeansCompleteBinsOnly(t *testing.T) {
	loc := zrh(t)
	pts := minutes(loc, "2026-11-26", "06:00", "06:12", 1000)
	bins, _ := BuildHistory(pts, at(t, loc, "2026-11-26 06:12"), loc, at(t, loc, "2026-11-26 06:12"))
	if len(bins) != 2 || bins[0] != (Bin{1000, true}) || bins[1] != (Bin{1000, true}) {
		t.Fatalf("bins %+v", bins)
	}
}

func TestBinDayMean(t *testing.T) {
	loc := zrh(t)
	pts := append(minutes(loc, "2026-11-26", "06:00", "06:02", 1000), minutes(loc, "2026-11-26", "06:02", "06:05", 2000)...)
	bins, _ := BuildHistory(pts, at(t, loc, "2026-11-26 12:00"), loc, at(t, loc, "2026-11-26 06:05"))
	if len(bins) != 1 || bins[0] != (Bin{1600, true}) {
		t.Fatalf("bins %+v", bins)
	}
}

func TestBinDayBeforeSix(t *testing.T) {
	loc := zrh(t)
	pts := minutes(loc, "2026-11-26", "00:00", "06:00", 600)
	bins, wh := BuildHistory(pts, at(t, loc, "2026-11-26 06:03"), loc, at(t, loc, "2026-11-26 06:03"))
	if len(bins) != 0 {
		t.Fatalf("bins %+v", bins)
	}
	if wh != 3600 {
		t.Fatalf("wh %d, want 3600", wh)
	}
}

func TestBinDayNullMinutes(t *testing.T) {
	loc := zrh(t)
	pts := append(minutes(loc, "2026-11-26", "06:00", "06:05", 500), minutes(loc, "2026-11-26", "06:10", "06:15", 700)...)
	bins, _ := BuildHistory(pts, at(t, loc, "2026-11-26 07:00"), loc, at(t, loc, "2026-11-26 06:15"))
	want := []Bin{{500, true}, {0, false}, {700, true}}
	if len(bins) != 3 || bins[0] != want[0] || bins[1] != want[1] || bins[2] != want[2] {
		t.Fatalf("bins %+v", bins)
	}
}

func TestBinDayCapsAt192(t *testing.T) {
	loc := zrh(t)
	pts := minutes(loc, "2026-11-26", "00:00", "23:30", 100)
	bins, _ := BuildHistory(pts, at(t, loc, "2026-11-26 23:30"), loc, at(t, loc, "2026-11-26 23:30"))
	if len(bins) != 192 {
		t.Fatalf("len %d, want 192", len(bins))
	}
}

func TestBinDayIgnoresOtherDays(t *testing.T) {
	loc := zrh(t)
	pts := append(minutes(loc, "2026-11-25", "06:00", "07:00", 9000), minutes(loc, "2026-11-26", "06:00", "06:05", 100)...)
	bins, wh := BuildHistory(pts, at(t, loc, "2026-11-26 06:05"), loc, at(t, loc, "2026-11-26 06:05"))
	if len(bins) != 1 || bins[0] != (Bin{100, true}) {
		t.Fatalf("bins %+v", bins)
	}
	if wh != 8 { // 5 min * 100 W = 8.33 Wh
		t.Fatalf("wh %d, want 8", wh)
	}
}

func TestBinDayDSTDay(t *testing.T) {
	loc := zrh(t)
	pts := minutes(loc, "2026-03-29", "06:00", "06:05", 300)
	bins, _ := BuildHistory(pts, at(t, loc, "2026-03-29 06:05"), loc, at(t, loc, "2026-03-29 06:05"))
	if len(bins) != 1 || bins[0] != (Bin{300, true}) {
		t.Fatalf("bins %+v", bins)
	}
}
