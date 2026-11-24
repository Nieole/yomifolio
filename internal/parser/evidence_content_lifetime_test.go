package parser_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yomifolio/internal/bookcontent"
	"yomifolio/internal/config"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
)

type evidenceAccess struct {
	active   atomic.Int32
	admitted chan struct{}
	resume   <-chan struct{}
}

func (a *evidenceAccess) LibraryAccessVersion() uint64 { return 1 }
func (a *evidenceAccess) AcquireLibraryRead(int64) (func(), error) {
	a.active.Add(1)
	if a.admitted != nil {
		a.admitted <- struct{}{}
		<-a.resume
	}
	return func() { a.active.Add(-1) }, nil
}

// Observe the first cancellation checkpoint after the service has borrowed the
// archive. No polling: each check is made only when the production API calls Err.
type borrowedEvidenceContext struct {
	context.Context
	path    string
	once    sync.Once
	entered chan struct{}
	resume  chan struct{}
}

func (c *borrowedEvidenceContext) Err() error {
	err := c.Context.Err()
	if parser.ActiveArchiveBorrows(c.path) == 2 {
		c.once.Do(func() { close(c.entered); <-c.resume })
	}
	return err
}

func TestEvidenceSessionCancellationReturnsContentResources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbr")
	data, err := os.ReadFile("testdata/vol.cbr")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	parser.InitPool(8)
	defer parser.CloseArchivePool()
	arc, release, err := parser.GetArchiveFromPool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	unlock := parser.HoldRarSessionForTest(arc)
	defer func() { unlock() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	barrier := &borrowedEvidenceContext{Context: ctx, path: path, entered: make(chan struct{}), resume: make(chan struct{})}
	access := &evidenceAccess{}
	cfg := config.Config{}
	cfg.Library.StorageProfile = config.StorageProfileHDDExternal
	source := bookcontent.Source{ID: 1, LibraryID: 7, Path: path}
	service := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) { return source, nil }, func() config.Config { return cfg }, nil, 1, 1)
	result := make(chan error, 1)
	go func() { _, err := service.Evidence(barrier, 1, 7, []int64{1}, false, access); result <- err }()
	select {
	case <-barrier.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("did not reach borrowed manifest checkpoint")
	}
	if access.active.Load() != 1 {
		t.Fatal("test never acquired library admission")
	}
	leaseHeld := false
	for _, v := range storageio.Default.Snapshot() {
		if v.VolumeKey == config.VolumeKey(path) && v.Active == 1 && v.ReaderActive == 1 {
			leaseHeld = true
		}
	}
	if !leaseHeld {
		t.Error("test never acquired the configured IO lease")
	}
	cancel()
	close(barrier.resume)
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("evidence: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("content cancellation waited for ordinary RAR IO")
		unlock()
		unlock = func() {}
		_ = <-result
		return
	}
	if access.active.Load() != 0 || parser.ActiveArchiveBorrows(path) != 1 {
		t.Fatalf("retained library=%d archive=%d (ordinary borrower must remain)", access.active.Load(), parser.ActiveArchiveBorrows(path))
	}
	for _, v := range storageio.Default.Snapshot() {
		if v.VolumeKey == config.VolumeKey(path) && (v.Active != 0 || v.ReaderActive != 0 || v.ReaderWaiting != 0) {
			t.Fatalf("retained IO lease: %+v", v)
		}
	}
	// Both evidence slots must be reusable BEFORE ordinary IO is released. Hold
	// two new requests at library admission, ahead of archive/IO acquisition.
	resume := make(chan struct{})
	next := &evidenceAccess{admitted: make(chan struct{}, 2), resume: resume}
	nextCtx, nextCancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	for range 2 {
		go func() { _, _ = service.Evidence(nextCtx, 1, 7, []int64{1}, false, next); done <- struct{}{} }()
	}
	for range 2 {
		select {
		case <-next.admitted:
		case <-time.After(2 * time.Second):
			t.Error("evidence slot retained after cancellation")
		}
	}
	nextCancel()
	close(resume)
	for range 2 {
		<-done
	}
	if next.active.Load() != 0 {
		t.Fatal("reused requests retained admission")
	}
}
