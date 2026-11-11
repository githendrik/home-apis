package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

type ctxBlockFetcher struct{}

func (ctxBlockFetcher) Fetch(ctx context.Context, from, to time.Time) ([]*ical.Calendar, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestColdGetTimesOut(t *testing.T) {
	c := &clock{t: zt(t, 8, 0)}
	s := NewService(ctxBlockFetcher{}, 300*time.Second, zurich(t), c.now)
	s.coldTimeout = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := s.Get(context.Background(), 0, 3)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from hung cold fetch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cold Get did not time out")
	}
}

func TestFetchFailureResetsDiscovery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	f := &icloudFetcher{httpc: srv.Client(), base: srv.URL, calendarPath: "/cal/"}
	now := time.Now()
	if _, err := f.Fetch(context.Background(), now, now.Add(time.Hour)); err == nil {
		t.Fatal("expected query error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.httpc != nil || f.base != "" || f.calendarPath != "" {
		t.Fatal("discovery cache not reset after failure")
	}
}
