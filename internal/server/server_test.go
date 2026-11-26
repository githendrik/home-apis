package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/githendrik/home-apis/internal/calendar"
	"github.com/githendrik/home-apis/internal/config"
	"github.com/githendrik/home-apis/internal/solar"
)

type fakeCal struct {
	resp       calendar.Response
	err        error
	shift, max int
	status     calendar.Status
}

func (f *fakeCal) Get(_ context.Context, shift, max int) (calendar.Response, error) {
	f.shift, f.max = shift, max
	return f.resp, f.err
}
func (f *fakeCal) Status() calendar.Status { return f.status }

type fakeSolar struct {
	snap     solar.Snapshot
	ok       bool
	hist     *solar.History
	histLast time.Time
	histErr  string
}

func (f *fakeSolar) Latest() (solar.Snapshot, bool) { return f.snap, f.ok }
func (f *fakeSolar) Status() (time.Time, string)    { return time.Time{}, "" }
func (f *fakeSolar) LatestHistory() (solar.History, bool) {
	if f.hist == nil {
		return solar.History{}, false
	}
	return *f.hist, true
}
func (f *fakeSolar) HistoryStatus() (time.Time, string) { return f.histLast, f.histErr }

var zurich, _ = time.LoadLocation("Europe/Zurich")

func cfg(token string) config.Config {
	return config.Config{APIToken: token, MaxEvents: 3, KitchenMaxEvents: 5, Timezone: zurich}
}

func sampleResp() calendar.Response {
	return calendar.Response{
		Today: calendar.Day{Label: "Today", Date: "2026-11-10", Events: []calendar.Event{
			{Summary: "A & B", Start: "2026-11-10T10:00:00", End: "2026-11-10T11:00:00"},
		}},
		Tomorrow:  calendar.Day{Label: "Tomorrow", Date: "2026-11-11", Events: []calendar.Event{}},
		FetchedAt: "2026-11-10T14:22:03.721Z",
	}
}

func snap() solar.Snapshot {
	return solar.Snapshot{PVW: 1000, LoadW: 500, GridW: -500, BatterySOC: 50,
		UpdatedAt: time.Date(2026, 11, 10, 6, 39, 12, 0, time.UTC), Source: "fake"}
}

func do(h http.Handler, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func newH(token string, cal *fakeCal, sol *fakeSolar) http.Handler {
	now := func() time.Time { return time.Date(2026, 11, 10, 21, 5, 0, 0, time.UTC) }
	return New(cfg(token), cal, sol, now)
}

func TestHealthPublic(t *testing.T) {
	w := do(newH("s", &fakeCal{}, &fakeSolar{}), "/health", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !strings.HasPrefix(w.Body.String(), `{"status":"ok"`) {
		t.Fatalf("status not first: %s", w.Body)
	}
}

func TestAuthRequired(t *testing.T) {
	h := newH("s", &fakeCal{resp: sampleResp()}, &fakeSolar{snap: snap(), ok: true})
	for _, p := range []string{"/events", "/solar", "/kitchen", "/nope"} {
		w := do(h, p, "")
		if w.Code != 401 || w.Body.String() != `{"error":"Unauthorized"}` {
			t.Fatalf("%s: %d %s", p, w.Code, w.Body)
		}
		if w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("ct %q", w.Header().Get("Content-Type"))
		}
		if do(h, p, "wrong").Code != 401 {
			t.Fatalf("%s wrong token accepted", p)
		}
	}
	w := do(h, "/nope", "s")
	if w.Code != 404 || w.Body.String() != "Not Found" {
		t.Fatalf("%d %q", w.Code, w.Body)
	}
}

func TestAuthDisabled(t *testing.T) {
	w := do(newH("", &fakeCal{resp: sampleResp()}, &fakeSolar{}), "/events", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestEventsBody(t *testing.T) {
	cal := &fakeCal{resp: sampleResp()}
	w := do(newH("", cal, &fakeSolar{}), "/events", "")
	var want bytes.Buffer
	if err := calendar.Encode(&want, sampleResp()); err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != want.String() {
		t.Fatalf("got %q want %q", w.Body, want.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatal(w.Header())
	}
	if cal.shift != 0 || cal.max != 3 {
		t.Fatalf("args %d %d", cal.shift, cal.max)
	}
}

func TestEvents502(t *testing.T) {
	w := do(newH("", &fakeCal{err: errors.New("boom")}, &fakeSolar{}), "/events", "")
	if w.Code != 502 || w.Body.String() != `{"error":"boom"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestSolar(t *testing.T) {
	w := do(newH("", &fakeCal{}, &fakeSolar{snap: snap(), ok: true}), "/solar", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "\n  \"pv_w\": 1000,") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if strings.HasSuffix(w.Body.String(), "\n") {
		t.Fatal("trailing newline")
	}
}

func TestSolarNoData(t *testing.T) {
	w := do(newH("", &fakeCal{}, &fakeSolar{}), "/solar", "")
	if w.Code != 503 || w.Body.String() != `{"error":"no solar data yet"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestKitchenCombined(t *testing.T) {
	cal := &fakeCal{resp: sampleResp()}
	w := do(newH("", cal, &fakeSolar{snap: snap(), ok: true}), "/kitchen?shift=1", "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if cal.shift != 1 || cal.max != 5 {
		t.Fatalf("args %d %d", cal.shift, cal.max)
	}
	b := w.Body.String()
	for _, s := range []string{
		"{\"calendar\":{\"today\":{",
		"\"fetched_at\":\"2026-11-10T14:22:03.721Z\"",
		"\"summary\":\"A & B\"",
		"\"solar\":{\"pv_w\":1000,",
		"\"server_time\":\"2026-11-10T22:05:00+01:00\"}",
	} {
		if !strings.Contains(b, s) {
			t.Fatalf("missing %q in %s", s, b)
		}
	}
	if strings.HasSuffix(b, "\n") {
		t.Fatal("trailing newline")
	}
	if strings.Index(b, `"calendar"`) > strings.Index(b, `"solar"`) || strings.Index(b, `"solar"`) > strings.Index(b, `"server_time"`) {
		t.Fatal("key order")
	}
}

func TestKitchenSolarNull(t *testing.T) {
	w := do(newH("", &fakeCal{resp: sampleResp()}, &fakeSolar{}), "/kitchen", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"solar":null`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestKitchenCalendarError(t *testing.T) {
	w := do(newH("", &fakeCal{err: errors.New("boom")}, &fakeSolar{snap: snap(), ok: true}), "/kitchen", "")
	if w.Code != 502 || w.Body.String() != `{"error":"boom"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestKitchenBadShift(t *testing.T) {
	for _, q := range []string{"shift=2", "shift=x", "shift=-1"} {
		w := do(newH("", &fakeCal{resp: sampleResp()}, &fakeSolar{}), "/kitchen?"+q, "")
		if w.Code != 400 {
			t.Fatalf("%s: %d", q, w.Code)
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	r := httptest.NewRequest("POST", "/events", nil)
	w := httptest.NewRecorder()
	newH("", &fakeCal{}, &fakeSolar{}).ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}

func hist(date string) *solar.History {
	return &solar.History{Date: date, PV: []solar.Bin{{W: 1200, OK: true}}, Load: []solar.Bin{{W: 500, OK: true}},
		PVWh: 300, LoadWh: 125, UpdatedAt: time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)}
}

// newH's clock is 2026-11-10 22:05 in Zurich.
func TestKitchenIncludesTodaysHistory(t *testing.T) {
	w := do(newH("", &fakeCal{resp: sampleResp()}, &fakeSolar{snap: snap(), ok: true, hist: hist("2026-11-10")}), "/kitchen", "")
	b := w.Body.String()
	if w.Code != 200 || !strings.Contains(b, "\"history\":{") || !strings.Contains(b, "\"step_min\":5") {
		t.Fatalf("%d %s", w.Code, b)
	}
	if strings.Index(b, `"solar"`) > strings.Index(b, `"history"`) || strings.Index(b, `"history"`) > strings.Index(b, `"server_time"`) {
		t.Fatal("key order")
	}
}

func TestKitchenHistoryOtherDayOmitted(t *testing.T) {
	w := do(newH("", &fakeCal{resp: sampleResp()}, &fakeSolar{snap: snap(), ok: true, hist: hist("2026-11-09")}), "/kitchen", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"history"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestKitchenShiftNoHistory(t *testing.T) {
	w := do(newH("", &fakeCal{resp: sampleResp()}, &fakeSolar{snap: snap(), ok: true, hist: hist("2026-11-10")}), "/kitchen?shift=1", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"history"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestKitchenRealisticBodyFitsPanel(t *testing.T) {
	title := strings.Repeat("Elternabend Schule ", 4)[:60]
	var today, tomorrow []calendar.Event
	for i := range 5 {
		today = append(today, calendar.Event{Summary: title, Start: fmt.Sprintf("2026-11-10T%02d:00:00", 8+i), End: fmt.Sprintf("2026-11-10T%02d:30:00", 8+i)})
		tomorrow = append(tomorrow, calendar.Event{Summary: title, Start: fmt.Sprintf("2026-11-11T%02d:00:00", 8+i), End: fmt.Sprintf("2026-11-11T%02d:30:00", 8+i)})
	}
	resp := calendar.Response{
		Today:     calendar.Day{Label: "Today", Date: "2026-11-10", Events: today},
		Tomorrow:  calendar.Day{Label: "Tomorrow", Date: "2026-11-11", Events: tomorrow},
		FetchedAt: "2026-11-10T14:22:03.721Z",
	}
	h := &solar.History{Date: "2026-11-10", PVWh: 45678, LoadWh: 23456, UpdatedAt: time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)}
	for i := range 192 {
		ok := i%10 != 3 // ~10 % missing
		h.PV = append(h.PV, solar.Bin{W: 9900 - i*7, OK: ok})
		h.Load = append(h.Load, solar.Bin{W: 12000 - i*11, OK: ok})
	}
	w := do(newH("", &fakeCal{resp: resp}, &fakeSolar{snap: snap(), ok: true, hist: h}), "/kitchen", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"history"`) {
		t.Fatalf("%d", w.Code)
	}
	if n := w.Body.Len(); n > 4000 {
		t.Fatalf("body %d bytes, panel keeps at most 4000", n)
	} else {
		t.Logf("realistic /kitchen body: %d bytes", n)
	}
}

func TestHealthHistoryStatus(t *testing.T) {
	last := time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)
	w := do(newH("", &fakeCal{}, &fakeSolar{histLast: last, histErr: "boom"}), "/health", "")
	if !strings.Contains(w.Body.String(), `"history":{"last_success":"2026-11-10T21:00:00Z","last_error":"boom"}`) {
		t.Fatalf("%s", w.Body)
	}
}
