package calendar

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/emersion/go-ical"
)

// Fetcher retrieves raw calendar objects overlapping [from, to).
type Fetcher interface {
	Fetch(ctx context.Context, from, to time.Time) ([]*ical.Calendar, error)
}

// Status reports the health of the calendar refresh loop.
type Status struct {
	LastSuccess time.Time
	LastError   string
}

// Service caches the expanded occurrences of the window
// [today 00:00, today+3d) and serves Responses from them.
type Service struct {
	fetcher Fetcher
	ttl     time.Duration
	loc     *time.Location
	now     func() time.Time

	mu          sync.Mutex // held across the fetch: single-flight
	occ         []Occurrence
	fetchedAt   time.Time
	windowStart time.Time
	haveCache   bool
	status      Status
}

func NewService(f Fetcher, ttl time.Duration, loc *time.Location, now func() time.Time) *Service {
	return &Service{fetcher: f, ttl: ttl, loc: loc, now: now}
}

func (s *Service) startOfToday(now time.Time) time.Time {
	n := now.In(s.loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, s.loc)
}

// Get returns a Response for shift/maxEvents, refreshing the cache if it is
// older than the TTL or the day changed. On refresh failure stale data is
// served if available.
func (s *Service) Get(ctx context.Context, shift, maxEvents int) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	start := s.startOfToday(now)
	fresh := s.haveCache && s.windowStart.Equal(start) && now.Sub(s.fetchedAt) < s.ttl
	if !fresh {
		if err := s.refresh(ctx, now, start); err != nil {
			s.status.LastError = err.Error()
			if !s.haveCache {
				return Response{}, err
			}
			slog.Warn("calendar refresh failed, serving stale data", "error", err)
		}
	}
	return Build(s.occ, now, shift, maxEvents, s.loc, s.fetchedAt), nil
}

func (s *Service) refresh(ctx context.Context, now, start time.Time) error {
	end := start.AddDate(0, 0, 3)
	cals, err := s.fetcher.Fetch(ctx, start, end)
	if err != nil {
		return err
	}
	occ, err := Expand(cals, start, end, s.loc)
	if err != nil {
		return err
	}
	s.occ, s.fetchedAt, s.windowStart, s.haveCache = occ, now, start, true
	s.status = Status{LastSuccess: now}
	return nil
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
