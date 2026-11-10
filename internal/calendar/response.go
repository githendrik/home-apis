package calendar

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"time"
)

type Event struct {
	Summary string `json:"summary"`
	Start   string `json:"start"`
	End     string `json:"end"`
	AllDay  bool   `json:"allDay"`
}

type Day struct {
	Label  string  `json:"label"`
	Date   string  `json:"date"`
	Events []Event `json:"events"`
}

type Response struct {
	Today     Day    `json:"today"`
	Tomorrow  Day    `json:"tomorrow"`
	FetchedAt string `json:"fetched_at"`
}

const (
	dateLayout     = "2006-01-02"
	dateTimeLayout = "2006-01-02T15:04:05"
	fetchedLayout  = "2006-01-02T15:04:05.000Z"
)

// Build places occurrences on day0 = today+shift and day1 = day0+1 (dates in loc).
func Build(occ []Occurrence, now time.Time, shift, maxEvents int, loc *time.Location, fetchedAt time.Time) Response {
	n := now.In(loc)
	day0 := time.Date(n.Year(), n.Month(), n.Day()+shift, 0, 0, 0, 0, loc)
	day1 := time.Date(day0.Year(), day0.Month(), day0.Day()+1, 0, 0, 0, 0, loc)
	return Response{
		Today:     buildDay("Today", day0, occ, maxEvents, loc),
		Tomorrow:  buildDay("Tomorrow", day1, occ, maxEvents, loc),
		FetchedAt: fetchedAt.UTC().Format(fetchedLayout),
	}
}

func buildDay(label string, day time.Time, occ []Occurrence, maxEvents int, loc *time.Location) Day {
	d := day.Format(dateLayout)
	var sel []Occurrence
	for _, o := range occ {
		s, e := o.Start.In(loc).Format(dateLayout), o.End.In(loc).Format(dateLayout)
		if o.AllDay {
			if s <= d && d < e {
				sel = append(sel, o)
			}
		} else if s == d {
			sel = append(sel, o)
		}
	}
	sort.SliceStable(sel, func(i, j int) bool {
		if sel[i].AllDay != sel[j].AllDay {
			return sel[i].AllDay
		}
		return sel[i].Start.Before(sel[j].Start)
	})
	if maxEvents >= 0 && len(sel) > maxEvents {
		sel = sel[:maxEvents]
	}
	events := make([]Event, 0, len(sel))
	for _, o := range sel {
		layout := dateTimeLayout
		if o.AllDay {
			layout = dateLayout
		}
		events = append(events, Event{
			Summary: o.Summary,
			Start:   o.Start.In(loc).Format(layout),
			End:     o.End.In(loc).Format(layout),
			AllDay:  o.AllDay,
		})
	}
	return Day{Label: label, Date: d, Events: events}
}

// Encode writes r as 2-space indented JSON without a trailing newline,
// matching the Deno service's JSON.stringify(data, null, 2).
func Encode(w io.Writer, r Response) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return err
	}
	_, err := w.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	return err
}
