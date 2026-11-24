package parser

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Pause the initial preflight after taking its Err snapshot. Cancellation then
// occurs after preflight, not before the API starts, while ordinary IO owns mu.
type evidencePreflightBarrier struct {
	context.Context
	once    sync.Once
	entered chan struct{}
	resume  chan struct{}
}

func (c *evidencePreflightBarrier) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.entered); <-c.resume })
	return err
}

func TestEvidenceCancellationDoesNotWaitForOrdinaryRarSession(t *testing.T) {
	for _, name := range []string{"directory", "selected-page", "comicinfo"} {
		t.Run(name, func(t *testing.T) {
			arc, err := OpenArchive(testRarPath)
			if err != nil {
				t.Fatal(err)
			}
			defer arc.Close()
			a := arc.(*RarArchive)
			a.mu.Lock()
			locked := true
			defer func() {
				if locked {
					a.mu.Unlock()
				}
			}()
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &evidencePreflightBarrier{Context: base, entered: make(chan struct{}), resume: make(chan struct{})}
			result := make(chan error, 1)
			go func() {
				switch name {
				case "directory":
					_, err = GetPagesLimited(ctx, arc)
				case "comicinfo":
					_, err = ReadMetadataLimited(ctx, arc, "ComicInfo.xml", 64<<10)
				default:
					_, err = ReadPageLimited(ctx, arc, "1.jpg", 16<<20)
				}
				result <- err
			}()
			<-ctx.entered
			cancel()
			close(ctx.resume)
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled evidence: %v", err)
				}
			case <-time.After(2 * time.Second):
				// Cleanup only AFTER recording failure; unlocking cannot make this pass.
				t.Error("cancelled evidence waited for ordinary session mutex")
				a.mu.Unlock()
				locked = false
				<-result
				return
			}
			a.mu.Unlock()
			locked = false
			data, err := arc.ReadPage("1.jpg")
			if err != nil || string(data) != "j1" {
				t.Fatalf("ordinary reading changed: %q, %v", data, err)
			}
			if err := arc.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := GetPagesLimited(context.Background(), arc); !errors.Is(err, ErrArchiveClosed) {
				t.Fatalf("directory after close: %v", err)
			}
			if _, err := ReadMetadataLimited(context.Background(), arc, "ComicInfo.xml", 64<<10); !errors.Is(err, ErrArchiveClosed) {
				t.Fatalf("metadata after close: %v", err)
			}
			if _, err := ReadPageLimited(context.Background(), arc, "1.jpg", 16<<20); !errors.Is(err, ErrArchiveClosed) {
				t.Fatalf("page after close: %v", err)
			}
		})
	}
}

// Done is evaluated by the cancellable acquisition select, after preflight.
// This barrier proves cancellation wakes an already queued request as well.
type evidenceQueuedContext struct {
	context.Context
	once   sync.Once
	queued chan struct{}
}

func (c *evidenceQueuedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.queued) })
	return c.Context.Done()
}

func TestEvidenceQueuedCancellationWithConcurrentClose(t *testing.T) {
	for _, name := range []string{"directory", "selected-page", "comicinfo"} {
		t.Run(name, func(t *testing.T) {
			arc, err := OpenArchive(testRarPath)
			if err != nil {
				t.Fatal(err)
			}
			a := arc.(*RarArchive)
			a.mu.Lock()
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &evidenceQueuedContext{Context: base, queued: make(chan struct{})}
			result := make(chan error, 1)
			go func() {
				switch name {
				case "directory":
					_, err = GetPagesLimited(ctx, arc)
				case "comicinfo":
					_, err = ReadMetadataLimited(ctx, arc, "ComicInfo.xml", 64<<10)
				default:
					_, err = ReadPageLimited(ctx, arc, "1.jpg", 16<<20)
				}
				result <- err
			}()
			select {
			case <-ctx.queued:
			case <-time.After(2 * time.Second):
				t.Error("evidence did not reach cancellable acquisition")
				cancel()
				a.mu.Unlock()
				<-result
				_ = arc.Close()
				return
			}
			closed := make(chan error, 1)
			go func() { closed <- arc.Close() }()
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("queued cancellation: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Error("queued evidence did not cancel while Close/ordinary session remained blocked")
				a.mu.Unlock()
				<-result
				<-closed
				return
			}
			select {
			case err := <-closed:
				t.Fatalf("Close bypassed ordinary session protection: %v", err)
			default:
			}
			a.mu.Unlock()
			if err := <-closed; err != nil {
				t.Fatal(err)
			}
			if _, err := GetPagesLimited(context.Background(), arc); !errors.Is(err, ErrArchiveClosed) {
				t.Fatalf("manifest after concurrent Close: %v", err)
			}
			if _, err := ReadMetadataLimited(context.Background(), arc, "ComicInfo.xml", 64<<10); !errors.Is(err, ErrArchiveClosed) {
				t.Fatalf("metadata after close: %v", err)
			}
			if _, err := ReadPageLimited(context.Background(), arc, "1.jpg", 16<<20); !errors.Is(err, ErrArchiveClosed) {
				t.Fatalf("page after concurrent Close: %v", err)
			}
		})
	}
}
