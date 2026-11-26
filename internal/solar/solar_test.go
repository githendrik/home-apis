package solar

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func zurich(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	return loc
}

func readAt(t *testing.T, at time.Time) Snapshot {
	t.Helper()
	src := NewFake(func() time.Time { return at }, zurich(t))
	s, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFakeNightNoPV(t *testing.T) {
	loc := zurich(t)
	s := readAt(t, time.Date(2026, 6, 15, 2, 0, 0, 0, loc))
	if s.PVW != 0 {
		t.Fatalf("PVW = %d", s.PVW)
	}
}

func TestFakeNoonPeak(t *testing.T) {
	loc := zurich(t)
	june := readAt(t, time.Date(2026, 6, 15, 12, 30, 0, 0, loc))
	nov := readAt(t, time.Date(2026, 11, 10, 12, 30, 0, 0, loc))
	if june.PVW < 4000 || june.PVW > 8000 {
		t.Fatalf("june PVW = %d", june.PVW)
	}
	if nov.PVW >= june.PVW {
		t.Fatalf("nov %d >= june %d", nov.PVW, june.PVW)
	}
}

func TestFakeEnergyBalance(t *testing.T) {
	loc := zurich(t)
	start := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)
	for i := 0; i < 48; i++ {
		s := readAt(t, start.Add(time.Duration(i)*30*time.Minute))
		if s.PVW-s.LoadW-s.BatteryW+s.GridW != 0 {
			t.Fatalf("i=%d unbalanced: %+v", i, s)
		}
		if s.BatterySOC < 0 || s.BatterySOC > 100 {
			t.Fatalf("i=%d soc %d", i, s.BatterySOC)
		}
	}
}

func TestFakeDeterministic(t *testing.T) {
	at := time.Date(2026, 11, 10, 9, 15, 0, 0, zurich(t))
	if readAt(t, at) != readAt(t, at) {
		t.Fatal("not deterministic")
	}
}

func TestFakeShowsImportAtEvening(t *testing.T) {
	s := readAt(t, time.Date(2026, 11, 10, 19, 0, 0, 0, zurich(t)))
	if !(s.GridW > 0 || s.BatteryW < 0) {
		t.Fatalf("no import/discharge: %+v", s)
	}
}

func TestFakeHistory(t *testing.T) {
	loc := zurich(t)
	now := time.Date(2026, 6, 21, 13, 0, 0, 0, loc)
	src := NewFake(func() time.Time { return now }, loc)
	hs, ok := src.(HistorySource)
	if !ok {
		t.Fatal("fake source does not implement HistorySource")
	}
	h, err := hs.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Date != "2026-06-21" || len(h.PV) != 84 || len(h.Load) != 84 || h.PVWh <= 0 || h.LoadWh <= 0 {
		t.Fatalf("unexpected history: date=%s bins=%d/%d pvWh=%d loadWh=%d", h.Date, len(h.PV), len(h.Load), h.PVWh, h.LoadWh)
	}
	peak := 0
	for _, b := range h.PV {
		peak = max(peak, b.W)
	}
	if peak < 6000 {
		t.Fatalf("PV peak %d W, want >= 6000 at the June solstice", peak)
	}
	h2, _ := hs.History(context.Background())
	if h2.PVWh != h.PVWh || h2.PV[50] != h.PV[50] {
		t.Fatal("not deterministic")
	}
	if !h.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt %v", h.UpdatedAt)
	}
}

func TestFakeName(t *testing.T) {
	src := NewFake(time.Now, time.UTC)
	s, _ := src.Read(context.Background())
	if src.Name() != "fake" || s.Source != "fake" {
		t.Fatal("name/source")
	}
}

func TestSnapshotJSON(t *testing.T) {
	s := Snapshot{PVW: 1, LoadW: 2, GridW: -3, BatterySOC: 50, BatteryW: 4,
		UpdatedAt: time.Date(2026, 11, 10, 7, 39, 12, 999, time.FixedZone("x", 3600)),
		Source:    "fake", Stale: true}
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"pv_w":1,"load_w":2,"grid_w":-3,"battery_soc":50,"battery_w":4,"updated_at":"2026-11-10T06:39:12Z","source":"fake","stale":true}`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
}

type scriptSrc struct {
	mu   sync.Mutex
	snap Snapshot
	fail bool
}

func (s *scriptSrc) Name() string { return "script" }
func (s *scriptSrc) Read(context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return Snapshot{}, errors.New("boom")
	}
	return s.snap, nil
}

func TestPollerStaleAfterThreePeriods(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)
	now := t0
	every := time.Minute
	src := &scriptSrc{snap: Snapshot{PVW: 5, UpdatedAt: t0, Source: "script"}}
	p := NewPoller(src, every, func() time.Time { return now })
	p.pollOnce(context.Background())
	src.fail = true
	now = t0.Add(2 * every)
	p.pollOnce(context.Background())
	s, ok := p.Latest()
	if !ok || s.Stale {
		t.Fatalf("ok=%v stale=%v", ok, s.Stale)
	}
	now = t0.Add(3*every + time.Second)
	p.pollOnce(context.Background())
	s, _ = p.Latest()
	if !s.Stale || !s.UpdatedAt.Equal(t0) {
		t.Fatalf("stale=%v updated=%v", s.Stale, s.UpdatedAt)
	}
	last, errStr := p.Status()
	if !last.Equal(t0) || errStr != "boom" {
		t.Fatalf("status %v %q", last, errStr)
	}
}

func TestPollerNoDataYet(t *testing.T) {
	p := NewPoller(&scriptSrc{fail: true}, time.Minute, time.Now)
	p.pollOnce(context.Background())
	if _, ok := p.Latest(); ok {
		t.Fatal("expected no data")
	}
}

func TestPollerRunPollsImmediatelyAndStops(t *testing.T) {
	src := &scriptSrc{snap: Snapshot{PVW: 1}}
	p := NewPoller(src, time.Hour, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	deadline := time.After(2 * time.Second)
	for {
		if _, ok := p.Latest(); ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no immediate poll")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

type historySrc struct {
	scriptSrc
	calls    int
	histFail bool
	hist     History
}

func (h *historySrc) History(context.Context) (History, error) {
	h.calls++
	if h.histFail {
		return History{}, errors.New("hist boom")
	}
	return h.hist, nil
}

func TestPollerHistoryEveryFiveMinutes(t *testing.T) {
	t0 := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	now := t0
	src := &historySrc{scriptSrc: scriptSrc{snap: Snapshot{PVW: 1}}, hist: History{Date: "2026-11-26"}}
	p := NewPoller(src, time.Minute, func() time.Time { return now })
	for m := 0; m <= 11; m++ {
		now = t0.Add(time.Duration(m) * time.Minute)
		p.pollOnce(context.Background())
	}
	if src.calls != 3 {
		t.Fatalf("history calls %d, want 3 (t=0, 5, 10 min)", src.calls)
	}
	h, ok := p.LatestHistory()
	if !ok || h.Date != "2026-11-26" {
		t.Fatalf("LatestHistory %+v %v", h, ok)
	}
	last, errStr := p.HistoryStatus()
	if !last.Equal(t0.Add(10*time.Minute)) || errStr != "" {
		t.Fatalf("status %v %q", last, errStr)
	}
}

func TestPollerHistoryFailureKeepsLast(t *testing.T) {
	t0 := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	now := t0
	src := &historySrc{scriptSrc: scriptSrc{snap: Snapshot{PVW: 1}}, hist: History{Date: "2026-11-26", PVWh: 42}}
	p := NewPoller(src, time.Minute, func() time.Time { return now })
	p.pollOnce(context.Background())
	src.histFail = true
	now = t0.Add(5 * time.Minute)
	p.pollOnce(context.Background())
	h, ok := p.LatestHistory()
	if !ok || h.PVWh != 42 {
		t.Fatalf("kept history %+v %v", h, ok)
	}
	last, errStr := p.HistoryStatus()
	if !last.Equal(t0) || errStr != "hist boom" {
		t.Fatalf("status %v %q", last, errStr)
	}
	if _, ok := p.Latest(); !ok {
		t.Fatal("snapshot must not be affected")
	}
}

func TestPollerWithoutHistorySource(t *testing.T) {
	p := NewPoller(&scriptSrc{snap: Snapshot{PVW: 1}}, time.Minute, time.Now)
	p.pollOnce(context.Background())
	if _, ok := p.LatestHistory(); ok {
		t.Fatal("expected no history")
	}
}
