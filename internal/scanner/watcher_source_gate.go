package scanner

import (
	"sync"

	"github.com/fsnotify/fsnotify"
)

// drainingWatchSource keeps the native notification reader moving while accepted
// commands finish. Native Close must not race their replies: the Windows backend
// can select its shutdown request ahead of a queued Add/Remove and lose the reply.
type drainingWatchSource struct {
	source    WatchSource
	events    chan fsnotify.Event
	errs      chan error
	closing   chan struct{}
	pumped    chan struct{}
	mu        sync.Mutex
	stopped   bool
	commands  sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

func newDrainingWatchSource(source WatchSource) WatchSource {
	s := &drainingWatchSource{source: source, events: make(chan fsnotify.Event), errs: make(chan error), closing: make(chan struct{}), pumped: make(chan struct{})}
	go s.pump()
	return s
}

func (s *drainingWatchSource) Events() <-chan fsnotify.Event { return s.events }
func (s *drainingWatchSource) Errors() <-chan error          { return s.errs }

func (s *drainingWatchSource) pump() {
	defer close(s.pumped)
	defer close(s.events)
	defer close(s.errs)
	events, errs := s.source.Events(), s.source.Errors()
	for events != nil || errs != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			select {
			case s.events <- event:
			case <-s.closing:
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			select {
			case s.errs <- err:
			case <-s.closing:
			}
		}
	}
}

func (s *drainingWatchSource) admit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	s.commands.Add(1)
	return true
}

func (s *drainingWatchSource) Add(path string) error {
	if !s.admit() {
		return fsnotify.ErrClosed
	}
	defer s.commands.Done()
	return s.source.Add(path)
}

func (s *drainingWatchSource) Remove(path string) error {
	if !s.admit() {
		return fsnotify.ErrClosed
	}
	defer s.commands.Done()
	return s.source.Remove(path)
}

func (s *drainingWatchSource) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		close(s.closing)
		s.mu.Unlock()
		// Unlike waiting on FileWatcher's registrationOps, this wait has an active
		// consumer even after the application event loop stops. Both notification
		// channels must drain to let the native reader answer synchronous commands.
		s.commands.Wait()
		s.closeErr = s.source.Close()
		<-s.pumped
	})
	return s.closeErr
}
