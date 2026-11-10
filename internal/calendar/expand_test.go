package calendar

import (
	"os"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

func zurich(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func load(t *testing.T, name string) *ical.Calendar {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cal, err := ical.NewDecoder(f).Decode()
	if err != nil {
		t.Fatal(err)
	}
	return cal
}

func expand(t *testing.T, name string, from, to time.Time) []Occurrence {
	t.Helper()
	occ, err := Expand([]*ical.Calendar{load(t, name)}, from, to, zurich(t))
	if err != nil {
		t.Fatal(err)
	}
	return occ
}

func window(t *testing.T, days int) (time.Time, time.Time) {
	from := time.Date(2026, 11, 10, 0, 0, 0, 0, zurich(t))
	return from, from.AddDate(0, 0, days)
}

func wantOne(t *testing.T, occ []Occurrence) Occurrence {
	t.Helper()
	if len(occ) != 1 {
		t.Fatalf("want 1 occurrence, got %d: %+v", len(occ), occ)
	}
	return occ[0]
}

func TestTimedTZID(t *testing.T) {
	from, to := window(t, 3)
	o := wantOne(t, expand(t, "timed_tzid.ics", from, to))
	loc := zurich(t)
	if !o.Start.Equal(time.Date(2026, 11, 10, 8, 0, 0, 0, loc)) || !o.End.Equal(time.Date(2026, 11, 10, 16, 45, 0, 0, loc)) {
		t.Fatalf("got %v – %v", o.Start, o.End)
	}
	if o.Start.Location().String() != loc.String() || o.AllDay || o.Summary != "Work" {
		t.Fatalf("got %+v", o)
	}
}

func TestUTCEvent(t *testing.T) {
	from, to := window(t, 3)
	o := wantOne(t, expand(t, "utc.ics", from, to))
	if o.Start.Hour() != 8 || o.Start.Location().String() != "Europe/Zurich" {
		t.Fatalf("got %v", o.Start)
	}
}

func TestOtherTZID(t *testing.T) {
	from, to := window(t, 3)
	o := wantOne(t, expand(t, "other_tzid.ics", from, to))
	if o.Start.Hour() != 8 || o.Start.Day() != 10 {
		t.Fatalf("got %v", o.Start)
	}
}

func TestAllDay(t *testing.T) {
	from, to := window(t, 3)
	o := wantOne(t, expand(t, "allday.ics", from, to))
	loc := zurich(t)
	if !o.AllDay || !o.Start.Equal(time.Date(2026, 11, 11, 0, 0, 0, 0, loc)) || !o.End.Equal(time.Date(2026, 11, 12, 0, 0, 0, 0, loc)) {
		t.Fatalf("got %+v", o)
	}
}

func TestMultiDayAllDay(t *testing.T) {
	from, to := window(t, 3)
	o := wantOne(t, expand(t, "multiday_allday.ics", from, to))
	loc := zurich(t)
	if !o.Start.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, loc)) || !o.End.Equal(time.Date(2026, 11, 13, 0, 0, 0, 0, loc)) {
		t.Fatalf("got %+v", o)
	}
}

func TestWeeklyWithExdate(t *testing.T) {
	from, to := window(t, 3)
	if occ := expand(t, "weekly_exdate.ics", from, to); len(occ) != 0 {
		t.Fatalf("want none on exdate day, got %+v", occ)
	}
	o := wantOne(t, expand(t, "weekly_exdate.ics", from, from.AddDate(0, 0, 8)))
	if o.Start.Day() != 17 || o.Start.Hour() != 7 {
		t.Fatalf("got %v", o.Start)
	}
}

func TestRecurrenceOverride(t *testing.T) {
	loc := zurich(t)
	from := time.Date(2026, 11, 11, 0, 0, 0, 0, loc)
	o := wantOne(t, expand(t, "override.ics", from, from.AddDate(0, 0, 1)))
	if o.Start.Hour() != 11 || o.Summary != "Standup moved" {
		t.Fatalf("got %+v", o)
	}
}

func TestYearlyBirthday(t *testing.T) {
	from, to := window(t, 3)
	o := wantOne(t, expand(t, "yearly_birthday.ics", from, to))
	loc := zurich(t)
	if !o.AllDay || !o.Start.Equal(time.Date(2026, 11, 11, 0, 0, 0, 0, loc)) || !o.End.Equal(time.Date(2026, 11, 12, 0, 0, 0, 0, loc)) {
		t.Fatalf("got %+v", o)
	}
}

func TestDSTWeekly(t *testing.T) {
	loc := zurich(t)
	from := time.Date(2026, 10, 27, 0, 0, 0, 0, loc)
	o := wantOne(t, expand(t, "dst_weekly.ics", from, from.AddDate(0, 0, 1)))
	if o.Start.Hour() != 7 || o.Start.Day() != 27 {
		t.Fatalf("got %v", o.Start)
	}
}

func TestMissingSummary(t *testing.T) {
	from, to := window(t, 3)
	o := wantOne(t, expand(t, "missing_summary.ics", from, to))
	if o.Summary != "Untitled" {
		t.Fatalf("got %q", o.Summary)
	}
}
