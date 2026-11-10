package calendar

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func at(loc *time.Location, y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

func TestBuildSortsAllDayFirstThenStart(t *testing.T) {
	loc := zurich(t)
	now := at(loc, 2026, 11, 10, 8, 0)
	occ := []Occurrence{
		{Summary: "late", Start: at(loc, 2026, 11, 10, 15, 0), End: at(loc, 2026, 11, 10, 16, 0)},
		{Summary: "early", Start: at(loc, 2026, 11, 10, 9, 0), End: at(loc, 2026, 11, 10, 10, 0)},
		{Summary: "allday", Start: at(loc, 2026, 11, 10, 0, 0), End: at(loc, 2026, 11, 11, 0, 0), AllDay: true},
	}
	r := Build(occ, now, 0, 10, loc, now)
	got := []string{}
	for _, e := range r.Today.Events {
		got = append(got, e.Summary)
	}
	want := []string{"allday", "early", "late"}
	for i := range want {
		if len(got) != 3 || got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if r.Today.Events[0].Start != "2026-11-10" || r.Today.Events[0].End != "2026-11-11" {
		t.Errorf("all-day format: %+v", r.Today.Events[0])
	}
	if r.Today.Events[1].Start != "2026-11-10T09:00:00" {
		t.Errorf("timed format: %+v", r.Today.Events[1])
	}
}

func TestBuildLimit(t *testing.T) {
	loc := zurich(t)
	now := at(loc, 2026, 11, 10, 8, 0)
	var occ []Occurrence
	for h := 9; h < 14; h++ {
		occ = append(occ, Occurrence{Summary: "e", Start: at(loc, 2026, 11, 10, h, 0), End: at(loc, 2026, 11, 10, h+1, 0)})
	}
	r := Build(occ, now, 0, 3, loc, now)
	if len(r.Today.Events) != 3 || r.Today.Events[2].Start != "2026-11-10T11:00:00" {
		t.Fatalf("%+v", r.Today.Events)
	}
}

func TestBuildMultiDaySpansBothDays(t *testing.T) {
	loc := zurich(t)
	now := at(loc, 2026, 11, 10, 8, 0)
	occ := []Occurrence{{Summary: "trip", Start: at(loc, 2026, 11, 9, 0, 0), End: at(loc, 2026, 11, 13, 0, 0), AllDay: true}}
	r := Build(occ, now, 0, 3, loc, now)
	if len(r.Today.Events) != 1 || len(r.Tomorrow.Events) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestBuildTimedEventSpanningMidnight(t *testing.T) {
	loc := zurich(t)
	now := at(loc, 2026, 11, 10, 8, 0)
	occ := []Occurrence{{Summary: "night", Start: at(loc, 2026, 11, 10, 23, 0), End: at(loc, 2026, 11, 11, 1, 0)}}
	r := Build(occ, now, 0, 3, loc, now)
	if len(r.Today.Events) != 1 || len(r.Tomorrow.Events) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestBuildShift1(t *testing.T) {
	loc := zurich(t)
	now := at(loc, 2026, 11, 10, 22, 5)
	r := Build(nil, now, 1, 3, loc, now)
	if r.Today.Date != "2026-11-11" || r.Tomorrow.Date != "2026-11-12" {
		t.Fatalf("%+v", r)
	}
	if r.Today.Label != "Today" || r.Tomorrow.Label != "Tomorrow" {
		t.Fatalf("labels %+v", r)
	}
}

func TestBuildDSTDay(t *testing.T) {
	loc := zurich(t)
	now := at(loc, 2026, 10, 25, 12, 0)
	r := Build(nil, now, 0, 3, loc, now)
	if r.Today.Date != "2026-10-25" || r.Tomorrow.Date != "2026-10-26" {
		t.Fatalf("%+v", r)
	}
}

func TestEncodeGolden(t *testing.T) {
	r := Response{
		Today: Day{Label: "Today", Date: "2026-06-05", Events: []Event{
			{Summary: "Dentist & Co", Start: "2026-06-05T14:00:00", End: "2026-06-05T15:00:00"},
		}},
		Tomorrow: Day{Label: "Tomorrow", Date: "2026-06-06", Events: []Event{
			{Summary: "Hochzeitstag", Start: "2026-06-06", End: "2026-06-07", AllDay: true},
		}},
		FetchedAt: "2026-06-05T08:00:00.000Z",
	}
	var buf bytes.Buffer
	if err := Encode(&buf, r); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/events_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.Bytes(), want)
	}
}

func TestEncodeEmptyDayAndFetchedAt(t *testing.T) {
	loc := zurich(t)
	now := at(loc, 2026, 11, 10, 8, 0)
	fetched := time.Date(2026, 11, 10, 14, 22, 3, 721_000_000, time.UTC)
	r := Build(nil, now, 0, 3, loc, fetched)
	var buf bytes.Buffer
	if err := Encode(&buf, r); err != nil {
		t.Fatal(err)
	}
	want := `{
  "today": {
    "label": "Today",
    "date": "2026-11-10",
    "events": []
  },
  "tomorrow": {
    "label": "Tomorrow",
    "date": "2026-11-11",
    "events": []
  },
  "fetched_at": "2026-11-10T14:22:03.721Z"
}`
	if buf.String() != want {
		t.Fatalf("got:\n%s", buf.String())
	}
}
