package parser

import (
	"context"
	"sync"
)

// rarSessionMutex keeps Close, cursor/cache access and independent evidence
// readers serialized, but allows evidence to abandon acquisition on cancellation.
// Its zero value is usable; it must not be copied after first use.
type rarSessionMutex struct {
	once  sync.Once
	token chan struct{}
}

func (m *rarSessionMutex) init() {
	m.once.Do(func() { m.token = make(chan struct{}, 1); m.token <- struct{}{} })
}

func (m *rarSessionMutex) Lock() {
	m.init()
	<-m.token
}

func (m *rarSessionMutex) Unlock() { m.token <- struct{}{} }

func (m *rarSessionMutex) lockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.init()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.token:
		// Cancellation wins even when both the token and Done are ready.
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	}
}
