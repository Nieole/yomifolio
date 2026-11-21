package bookcontent

import (
	"context"
	"errors"
	"testing"

	"yomifolio/internal/config"
	"yomifolio/internal/parser"
)

type evidenceManifestAccess struct{ active int }

func (a *evidenceManifestAccess) LibraryAccessVersion() uint64 { return 1 }
func (a *evidenceManifestAccess) AcquireLibraryRead(int64) (func(), error) {
	a.active++
	return func() { a.active-- }, nil
}

func TestEvidenceCancellationAtManifestReturnsAllAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := "../parser/testdata/vol.cbr"
	source := Source{ID: 1, LibraryID: 7, Path: path}
	s := New(func(context.Context, int64) (Source, error) { return source, nil }, func() config.Config { return config.Config{} }, nil, 1, 1)
	borrows := 0
	s.reader = NewArchiveReader(func(string) (parser.Archive, func(), error) {
		arc, err := parser.OpenArchive(path)
		if err != nil {
			return nil, nil, err
		}
		borrows++
		cancel()
		return arc, func() { borrows--; _ = arc.Close() }, nil
	})
	access := &evidenceManifestAccess{}
	// An out-of-range selection must not hide cancellation during manifest work.
	// The ordinary GetPages path would continue and return ErrPageNotFound instead.
	_, err := s.Evidence(ctx, 1, 7, []int64{99}, false, access)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("manifest cancellation=%v", err)
	}
	if borrows != 0 || access.active != 0 || parser.ActiveArchiveBorrows(path) != 0 {
		t.Fatalf("retained borrow=%d library=%d", borrows, access.active)
	}
}
