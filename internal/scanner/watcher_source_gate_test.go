package scanner

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// replyRaceSource models the Windows reader: notifications can hold up a command
// reply, and native Close can retire the reader without answering that command.
type replyRaceSource struct {
	events    chan fsnotify.Event
	errs      chan error
	accepted  chan struct{}
	produced  chan struct{}
	closing   chan struct{}
	retired   chan struct{}
	release   chan struct{}
	rescue    chan struct{}
	once      sync.Once
	bypassAdd bool
}

func newReplyRaceSource() *replyRaceSource {
	return &replyRaceSource{events: make(chan fsnotify.Event), errs: make(chan error), accepted: make(chan struct{}), produced: make(chan struct{}), closing: make(chan struct{}), retired: make(chan struct{}), release: make(chan struct{}), rescue: make(chan struct{})}
}
func (s *replyRaceSource) Events() <-chan fsnotify.Event { return s.events }
func (s *replyRaceSource) Errors() <-chan error          { return s.errs }
func (s *replyRaceSource) Add(string) error {
	if s.bypassAdd {
		return nil
	}
	return s.command()
}
func (s *replyRaceSource) Remove(string) error { return s.command() }
func (s *replyRaceSource) command() error {
	close(s.accepted)
	// With no application consumer, even an adapter receiving the first event
	// cannot deliver it. The second notification keeps the reply outstanding.
	for index := range 2 {
		var delivered bool
		if index == 0 {
			select {
			case s.events <- fsnotify.Event{Name: "changed.cbz", Op: fsnotify.Write}:
				delivered = true
			case <-s.closing:
			}
		} else {
			select {
			case s.errs <- fsnotify.ErrEventOverflow:
				delivered = true
			case <-s.closing:
			}
		}
		if !delivered {
			close(s.produced)
			<-s.rescue // Test cleanup only; the native bug has no such escape.
			return fsnotify.ErrClosed
		}
	}
	close(s.produced)
	return nil
}
func (s *replyRaceSource) Close() error {
	s.once.Do(func() {
		close(s.closing)
		<-s.produced
		<-s.release
		close(s.events)
		close(s.errs)
		close(s.retired)
	})
	return nil
}

func awaitWatcherSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func TestWatchSourceCloseFinishesAcceptedCommands(t *testing.T) {
	for _, command := range []string{"Add", "Remove"} {
		t.Run(command, func(t *testing.T) {
			native := newReplyRaceSource()
			source := newDrainingWatchSource(native)
			commandDone := make(chan struct{})
			commandResult := make(chan error, 1)
			closeDone := make(chan struct{})
			var release sync.Once
			t.Cleanup(func() {
				close(native.rescue)
				release.Do(func() { close(native.release) })
				awaitWatcherSignal(t, commandDone, "command leaked")
				awaitWatcherSignal(t, closeDone, "Close leaked")
			})
			go func() {
				if command == "Add" {
					commandResult <- source.Add("library")
				} else {
					commandResult <- source.Remove("library")
				}
				close(commandDone)
			}()
			awaitWatcherSignal(t, native.accepted, "command not accepted")
			go func() { _ = source.Close(); close(closeDone) }()
			awaitWatcherSignal(t, commandDone, "accepted command stranded by Close")
			if err := <-commandResult; err != nil {
				t.Fatalf("command lost its native reply: %v", err)
			}
			awaitWatcherSignal(t, native.closing, "native Close not reached after command reply")
			select {
			case <-closeDone:
				t.Fatal("Close returned before native resources retired")
			default:
			}
			for _, late := range []func(string) error{source.Add, source.Remove} {
				if err := late("late"); !errors.Is(err, fsnotify.ErrClosed) {
					t.Errorf("command after closing = %v, want ErrClosed", err)
				}
			}
			release.Do(func() { close(native.release) })
			awaitWatcherSignal(t, closeDone, "Close failed to retire resources")
			awaitWatcherSignal(t, native.retired, "native reader still alive")
			if _, ok := <-source.Events(); ok {
				t.Error("event pump not closed")
			}
			if _, ok := <-source.Errors(); ok {
				t.Error("error pump not closed")
			}
		})
	}
}

func TestWatcherConcurrentStopDrainsAcceptedCommands(t *testing.T) {
	for _, command := range []string{"Add", "Remove"} {
		t.Run(command, func(t *testing.T) {
			native := newReplyRaceSource()
			native.bypassAdd = command == "Remove"
			fw, err := NewFileWatcherWithSource(stubWatcherHooks(), newDrainingWatchSource(native))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if command == "Remove" {
				if err := fw.WatchLibrary(1, root, ""); err != nil {
					t.Fatal(err)
				}
			}
			commandDone := make(chan struct{})
			stops := make([]chan struct{}, 4)
			var release sync.Once
			t.Cleanup(func() {
				close(native.rescue)
				release.Do(func() { close(native.release) })
				awaitWatcherSignal(t, commandDone, "registration leaked")
				for _, stopped := range stops {
					if stopped != nil {
						awaitWatcherSignal(t, stopped, "Stop leaked")
					}
				}
				fw.Stop()
			})
			go func() {
				if command == "Add" {
					_ = fw.WatchLibrary(1, root, "")
				} else {
					fw.UnwatchLibrary(root)
				}
				close(commandDone)
			}()
			awaitWatcherSignal(t, native.accepted, "command not accepted")
			for i := range stops {
				stops[i] = make(chan struct{})
				go func(done chan struct{}) { fw.Stop(); close(done) }(stops[i])
			}
			awaitWatcherSignal(t, commandDone, "registration blocked Stop")
			awaitWatcherSignal(t, native.closing, "native Close not reached")
			for _, stopped := range stops {
				select {
				case <-stopped:
					t.Fatal("Stop returned before source resources retired")
				default:
				}
			}
			if err := fw.WatchLibrary(2, root, ""); err == nil {
				t.Error("registration after Stop admitted")
			}
			release.Do(func() { close(native.release) })
			for _, stopped := range stops {
				awaitWatcherSignal(t, stopped, "concurrent Stop did not finish")
			}
			awaitWatcherSignal(t, native.retired, "native reader not retired")
		})
	}
}
