package calendar

import (
	"strings"
	"testing"

	"github.com/emersion/go-ical"
)

func parse(t *testing.T, s string) *ical.Calendar {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(strings.ReplaceAll(s, "\n", "\r\n"))).Decode()
	if err != nil {
		t.Fatal(err)
	}
	return cal
}

const skipHeader = "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//t//EN\n"

func TestBadTZIDEventSkipped(t *testing.T) {
	cal := parse(t, skipHeader+`BEGIN:VEVENT
UID:bad
DTSTAMP:20260101T000000Z
DTSTART;TZID=Nowhere/Land:20261110T090000
DTEND;TZID=Nowhere/Land:20261110T100000
SUMMARY:Bad
END:VEVENT
BEGIN:VEVENT
UID:good
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Zurich:20261110T090000
DTEND;TZID=Europe/Zurich:20261110T100000
SUMMARY:Good
END:VEVENT
END:VCALENDAR
`)
	from, to := window(t, 3)
	occ, err := Expand([]*ical.Calendar{cal}, from, to, zurich(t))
	if err != nil {
		t.Fatal(err)
	}
	if o := wantOne(t, occ); o.Summary != "Good" {
		t.Fatalf("got %+v", o)
	}
}

func TestSkippedMasterDoesNotDropLaterMastersOfSameUID(t *testing.T) {
	cal := parse(t, skipHeader+`BEGIN:VEVENT
UID:same
DTSTAMP:20260101T000000Z
SUMMARY:NoStart
END:VEVENT
BEGIN:VEVENT
UID:same
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Zurich:20261110T090000
DTEND;TZID=Europe/Zurich:20261110T100000
SUMMARY:Second
END:VEVENT
END:VCALENDAR
`)
	from, to := window(t, 3)
	occ, err := Expand([]*ical.Calendar{cal}, from, to, zurich(t))
	if err != nil {
		t.Fatal(err)
	}
	if o := wantOne(t, occ); o.Summary != "Second" {
		t.Fatalf("got %+v", o)
	}
}
