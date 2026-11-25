package metadata

import (
	"context"
	"sync"
	"time"
)

// SourcePacer spaces the requests sent to each catalogue source. Every caller sharing one pacer
// shares each source's schedule, so concurrent runs never add up to a faster rate. It complements
// the per-source concurrency gate, which bounds parallel requests but not their rate.
type SourcePacer struct {
	mu   sync.Mutex
	next map[string]time.Time
}

// NewSourcePacer returns a pacer with no request scheduled yet.
func NewSourcePacer() *SourcePacer {
	return &SourcePacer{next: map[string]time.Time{}}
}

// Wait reserves the source's next slot, at least interval after the slot reserved before it, and
// blocks until that slot. A non-positive interval reserves nothing and returns at once. Ending ctx
// stops the wait with its error; the reserved slot stays spent.
func (p *SourcePacer) Wait(ctx context.Context, source string, interval time.Duration) error {
	if interval <= 0 {
		return ctx.Err()
	}
	p.mu.Lock()
	slot := time.Now()
	if next := p.next[source]; next.After(slot) {
		slot = next
	}
	p.next[source] = slot.Add(interval)
	p.mu.Unlock()
	wait := time.Until(slot)
	if wait <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
