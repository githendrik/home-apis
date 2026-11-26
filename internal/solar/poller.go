package solar

import (
	"context"
	"sync"
	"time"
)

// Poller periodically reads a Source and keeps the latest successful snapshot.
type Poller struct {
	src   Source
	every time.Duration
	now   func() time.Time

	mu          sync.Mutex
	latest      Snapshot
	have        bool
	lastSuccess time.Time
	lastErr     string

	histTried   bool
	histAttempt time.Time
	hist        History
	haveHist    bool
	histSuccess time.Time
	histErr     string
}

func NewPoller(src Source, every time.Duration, now func() time.Time) *Poller {
	return &Poller{src: src, every: every, now: now}
}

// Run polls immediately, then every period until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.pollOnce(ctx)
	t := time.NewTicker(p.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	snap, err := p.src.Read(ctx)
	p.mu.Lock()
	if err != nil {
		p.lastErr = err.Error()
	} else {
		p.latest, p.have = snap, true
		p.lastSuccess = p.now()
		p.lastErr = ""
	}
	p.mu.Unlock()
	p.pollHistory(ctx)
}

// pollHistory fetches history at most every HistoryEvery, if the source has it.
// A failure keeps the previous history.
func (p *Poller) pollHistory(ctx context.Context) {
	hs, ok := p.src.(HistorySource)
	if !ok {
		return
	}
	now := p.now()
	p.mu.Lock()
	due := !p.histTried || now.Sub(p.histAttempt) >= HistoryEvery
	if due {
		p.histTried, p.histAttempt = true, now
	}
	p.mu.Unlock()
	if !due {
		return
	}
	h, err := hs.History(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.histErr = err.Error()
		return
	}
	p.hist, p.haveHist = h, true
	p.histSuccess = p.now()
	p.histErr = ""
}

// LatestHistory returns the last successfully fetched history (any date).
func (p *Poller) LatestHistory() (History, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hist, p.haveHist
}

func (p *Poller) HistoryStatus() (lastSuccess time.Time, lastErr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.histSuccess, p.histErr
}

// Latest returns the last successful snapshot with Stale computed now.
// ok is false until the first success.
func (p *Poller) Latest() (Snapshot, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.have {
		return Snapshot{}, false
	}
	s := p.latest
	s.Stale = p.now().Sub(p.lastSuccess) > 3*p.every
	return s, true
}

func (p *Poller) Status() (lastSuccess time.Time, lastErr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSuccess, p.lastErr
}
