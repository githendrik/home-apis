package calendar

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/emersion/go-ical"
)

// Occurrence is a single concrete instance of a calendar event.
type Occurrence struct {
	Summary    string
	Start, End time.Time // in loc; for all-day: midnight local, End exclusive
	AllDay     bool
}

// Expand returns every occurrence overlapping [from, to) from the given
// calendar objects. RRULE, RDATE, EXDATE and RECURRENCE-ID overrides are
// supported. Floating times are interpreted in loc.
func Expand(cals []*ical.Calendar, from, to time.Time, loc *time.Location) ([]Occurrence, error) {
	var out []Occurrence
	for _, cal := range cals {
		byUID := map[string][]ical.Event{}
		var order []string
		for _, ev := range cal.Events() {
			uid, _ := ev.Props.Text(ical.PropUID)
			if _, ok := byUID[uid]; !ok {
				order = append(order, uid)
			}
			byUID[uid] = append(byUID[uid], ev)
		}
		for _, uid := range order {
			occ, err := expandUID(byUID[uid], from, to, loc)
			if err != nil {
				slog.Warn("skipping malformed event", "uid", uid, "reason", err)
				continue
			}
			slog.Debug("expanded", "uid", uid, "events", len(byUID[uid]), "occurrences", len(occ))
			out = append(out, occ...)
		}
	}
	return out, nil
}

func uidOf(ev ical.Event) string {
	uid, _ := ev.Props.Text(ical.PropUID)
	return uid
}

func expandUID(events []ical.Event, from, to time.Time, loc *time.Location) ([]Occurrence, error) {
	var masters, overrides []ical.Event
	for _, ev := range events {
		if ev.Props.Get(ical.PropRecurrenceID) != nil {
			overrides = append(overrides, ev)
		} else {
			masters = append(masters, ev)
		}
	}

	overridden := map[int64]bool{}
	var out []Occurrence
	for _, ev := range overrides {
		rid, err := ev.Props.Get(ical.PropRecurrenceID).DateTime(loc)
		if err != nil {
			return nil, fmt.Errorf("recurrence-id: %w", err)
		}
		overridden[rid.Unix()] = true
		o, ok, err := single(ev, loc)
		if err != nil {
			return nil, err
		}
		if ok && overlaps(o, from, to) {
			out = append(out, o)
		}
	}

	for _, ev := range masters {
		base, ok, err := single(ev, loc)
		if err != nil {
			slog.Warn("skipping malformed event", "uid", uidOf(ev), "reason", err)
			continue
		}
		if !ok {
			continue
		}
		set, err := ev.RecurrenceSet(loc)
		if err != nil {
			slog.Warn("skipping event with bad recurrence", "uid", uidOf(ev), "reason", err)
			continue
		}
		if set == nil {
			if overlaps(base, from, to) {
				out = append(out, base)
			}
			continue
		}
		dur := base.End.Sub(base.Start)
		days := int(math.Round(dur.Hours() / 24))
		for _, start := range set.Between(from.Add(-dur), to, true) {
			if overridden[start.Unix()] {
				continue
			}
			o := base
			if base.AllDay {
				o.Start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
				o.End = o.Start.AddDate(0, 0, days)
			} else {
				o.Start = start.In(loc)
				o.End = start.Add(dur).In(loc)
			}
			if overlaps(o, from, to) {
				out = append(out, o)
			}
		}
	}
	return out, nil
}

// single builds the base occurrence of a VEVENT from its own DTSTART/DTEND.
// ok is false for cancelled events or events without DTSTART.
func single(ev ical.Event, loc *time.Location) (Occurrence, bool, error) {
	if st, err := ev.Status(); err == nil && st == ical.EventCancelled {
		return Occurrence{}, false, nil
	}
	startProp := ev.Props.Get(ical.PropDateTimeStart)
	if startProp == nil {
		return Occurrence{}, false, nil
	}
	start, err := ev.DateTimeStart(loc)
	if err != nil {
		return Occurrence{}, false, fmt.Errorf("dtstart: %w", err)
	}
	end, err := ev.DateTimeEnd(loc)
	if err != nil {
		return Occurrence{}, false, fmt.Errorf("dtend: %w", err)
	}
	allDay := startProp.ValueType() == ical.ValueDate ||
		(startProp.ValueType() == ical.ValueDefault && len(startProp.Value) == 8)

	summary, _ := ev.Props.Text(ical.PropSummary)
	if summary == "" {
		summary = "Untitled"
	}
	o := Occurrence{Summary: summary, AllDay: allDay}
	if allDay {
		o.Start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
		o.End = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)
		if !o.End.After(o.Start) {
			o.End = o.Start.AddDate(0, 0, 1)
		}
	} else {
		o.Start = start.In(loc)
		o.End = end.In(loc)
		if o.End.Before(o.Start) {
			o.End = o.Start
		}
	}
	return o, true, nil
}

func overlaps(o Occurrence, from, to time.Time) bool {
	if !o.Start.Before(to) {
		return false
	}
	if o.End.Equal(o.Start) {
		return !o.Start.Before(from)
	}
	return o.End.After(from)
}
