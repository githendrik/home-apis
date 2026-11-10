package server

import (
	"bytes"
	"context"
	"errors"
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
	snap solar.Snapshot
	ok   bool
}

func (f *fakeSolar) Latest() (solar.Snapshot, bool) { return f.snap, f.ok }
func (f *fakeSolar) Status() (time.Time, string)    { return time.Time{}, "" }

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
	h := newH("s", &fakeCal{resp: sampleResp()}, &fakeSolar{snap(), true})
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
	w := do(newH("", &fakeCal{}, &fakeSolar{snap(), true}), "/solar", "")
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
	w := do(newH("", cal, &fakeSolar{snap(), true}), "/kitchen?shift=1", "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if cal.shift != 1 || cal.max != 5 {
		t.Fatalf("args %d %d", cal.shift, cal.max)
	}
	b := w.Body.String()
	for _, s := range []string{
		"{\n  \"calendar\": {\n    \"today\": {",
		"\"fetched_at\": \"2026-11-10T14:22:03.721Z\"",
		"\"summary\": \"A & B\"",
		"\"solar\": {\n    \"pv_w\": 1000,",
		"\"server_time\": \"2026-11-10T22:05:00+01:00\"\n}",
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
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"solar": null`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestKitchenCalendarError(t *testing.T) {
	w := do(newH("", &fakeCal{err: errors.New("boom")}, &fakeSolar{snap(), true}), "/kitchen", "")
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
