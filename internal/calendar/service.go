package calendar

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/emersion/go-ical"
)

const (
	refreshTimeout = 30 * time.Second
	retryBackoff   = 60 * time.Second
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
//
// A cold cache is filled synchronously (single-flight). A warm cache that is
// past its TTL (or for a previous day) is served immediately while ONE
// background refresh runs; after a failed refresh no new attempt is made for
// retryBackoff.
type Service struct {
	fetcher Fetcher
	ttl     time.Duration
	loc     *time.Location
	now     func() time.Time

	coldTimeout time.Duration // bounds a cold fetch; overridable in tests

	coldMu sync.Mutex // serialises cold-cache fetches
	bg     sync.WaitGroup

	mu          sync.Mutex // guards the fields below; never held across a fetch
	occ         []Occurrence
	fetchedAt   time.Time
	windowStart time.Time
	haveCache   bool
	refreshing  bool
	lastAttempt time.Time
	lastFailed  bool
	status      Status
}

func NewService(f Fetcher, ttl time.Duration, loc *time.Location, now func() time.Time) *Service {
	return &Service{fetcher: f, ttl: ttl, loc: loc, now: now, coldTimeout: refreshTimeout}
}

func (s *Service) startOfToday(now time.Time) time.Time {
	n := now.In(s.loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, s.loc)
}

// Get returns a Response for shift/maxEvents. It only blocks on the network
// when nothing has ever been cached.
func (s *Service) Get(ctx context.Context, shift, maxEvents int) (Response, error) {
	if resp, ok := s.warm(shift, maxEvents); ok {
		return resp, nil
	}

	s.coldMu.Lock()
	defer s.coldMu.Unlock()
	if resp, ok := s.warm(shift, maxEvents); ok { // filled while we waited
		return resp, nil
	}
	cctx, cancel := context.WithTimeout(ctx, s.coldTimeout)
	defer cancel()
	if err := s.refresh(cctx); err != nil {
		return Response{}, err
	}
	resp, _ := s.warm(shift, maxEvents)
	return resp, nil
}

// warm serves from the cache if there is one, triggering a background refresh
// when it is stale and allowed.
func (s *Service) warm(shift, maxEvents int) (Response, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.haveCache {
		return Response{}, false
	}
	now := s.now()
	stale := !s.windowStart.Equal(s.startOfToday(now)) || now.Sub(s.fetchedAt) >= s.ttl
	backoff := s.lastFailed && now.Sub(s.lastAttempt) < retryBackoff
	if stale && !s.refreshing && !backoff {
		s.refreshing = true
		s.bg.Add(1)
		go func() {
			defer s.bg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
			defer cancel()
			if err := s.refresh(ctx); err != nil {
				slog.Warn("calendar background refresh failed, serving stale data", "error", err)
			}
		}()
	}
	return Build(s.occ, now, shift, maxEvents, s.loc, s.fetchedAt), true
}

// refresh fetches and expands the current window and stores the result.
func (s *Service) refresh(ctx context.Context) (err error) {
	now := s.now()
	start := s.startOfToday(now)
	end := start.AddDate(0, 0, 3)

	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.refreshing = false
		s.lastAttempt = now
		s.lastFailed = err != nil
		if err != nil {
			s.status.LastError = err.Error()
		}
	}()

	cals, err := s.fetcher.Fetch(ctx, start, end)
	if err != nil {
		return err
	}
	occ, err := Expand(cals, start, end, s.loc)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.occ, s.fetchedAt, s.windowStart, s.haveCache = occ, now, start, true
	s.status = Status{LastSuccess: now}
	s.mu.Unlock()
	return nil
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
