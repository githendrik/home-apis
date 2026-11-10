package calendar

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

type fakeFetcher struct {
	calls atomic.Int32
	mu    sync.Mutex
	err   error
	cals  []*ical.Calendar
	froms []time.Time
}

func (f *fakeFetcher) Fetch(ctx context.Context, from, to time.Time) ([]*ical.Calendar, error) {
	f.calls.Add(1)
	time.Sleep(10 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.froms = append(f.froms, from)
	return f.cals, f.err
}

func (f *fakeFetcher) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newSvc(t *testing.T, start time.Time) (*Service, *fakeFetcher, *clock) {
	t.Helper()
	f := &fakeFetcher{cals: []*ical.Calendar{parse(t, skipHeader+`BEGIN:VEVENT
UID:a
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Zurich:20261110T090000
DTEND;TZID=Europe/Zurich:20261110T100000
SUMMARY:Hello
END:VEVENT
END:VCALENDAR
`)}}
	c := &clock{t: start}
	return NewService(f, 300*time.Second, zurich(t), c.now), f, c
}

func zt(t *testing.T, h, m int) time.Time {
	return time.Date(2026, 11, 10, h, m, 0, 0, zurich(t))
}

func TestServiceCachesWithinTTL(t *testing.T) {
	s, f, c := newSvc(t, zt(t, 8, 0))
	ctx := context.Background()
	if _, err := s.Get(ctx, 0, 3); err != nil {
		t.Fatal(err)
	}
	c.advance(100 * time.Second)
	if _, err := s.Get(ctx, 0, 3); err != nil {
		t.Fatal(err)
	}
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("calls=%d", n)
	}
}

func TestServiceRefetchesAfterTTL(t *testing.T) {
	s, f, c := newSvc(t, zt(t, 8, 0))
	ctx := context.Background()
	s.Get(ctx, 0, 3)
	c.advance(301 * time.Second)
	s.Get(ctx, 0, 3)
	if n := f.calls.Load(); n != 2 {
		t.Fatalf("calls=%d", n)
	}
}

func TestServiceRefetchesOnDayChange(t *testing.T) {
	s, f, c := newSvc(t, zt(t, 23, 59))
	ctx := context.Background()
	s.Get(ctx, 0, 3)
	c.advance(2 * time.Minute)
	s.Get(ctx, 0, 3)
	if n := f.calls.Load(); n != 2 {
		t.Fatalf("calls=%d", n)
	}
	if !f.froms[0].Equal(zt(t, 0, 0)) || !f.froms[1].Equal(zt(t, 0, 0).AddDate(0, 0, 1)) {
		t.Fatalf("windows %v", f.froms)
	}
}

func TestServiceServesStaleOnError(t *testing.T) {
	s, f, c := newSvc(t, zt(t, 8, 0))
	ctx := context.Background()
	first, err := s.Get(ctx, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	f.setErr(errors.New("boom"))
	c.advance(301 * time.Second)
	got, err := s.Get(ctx, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Today.Events) != len(first.Today.Events) || len(got.Today.Events) != 1 {
		t.Fatalf("got %+v", got)
	}
	st := s.Status()
	if st.LastError == "" || st.LastSuccess.IsZero() {
		t.Fatalf("status %+v", st)
	}
}

func TestServiceErrorWithoutCache(t *testing.T) {
	s, f, _ := newSvc(t, zt(t, 8, 0))
	f.setErr(errors.New("boom"))
	if _, err := s.Get(context.Background(), 0, 3); err == nil {
		t.Fatal("want error")
	}
}

func TestServiceSingleFlight(t *testing.T) {
	s, f, _ := newSvc(t, zt(t, 8, 0))
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Get(context.Background(), 0, 3); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("calls=%d", n)
	}
}

func TestServiceShiftAndMaxShareOneFetch(t *testing.T) {
	s, f, _ := newSvc(t, zt(t, 8, 0))
	ctx := context.Background()
	s.Get(ctx, 0, 3)
	s.Get(ctx, 1, 5)
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("calls=%d", n)
	}
}
