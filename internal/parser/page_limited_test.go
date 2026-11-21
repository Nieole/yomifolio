package parser

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nwaples/rardecode/v2"
)

func TestEvidenceRarReadersShareCreationBudget(t *testing.T) {
	arc, err := OpenArchive(testRarPath)
	if err != nil {
		t.Fatal(err)
	}
	defer arc.Close()
	original := openBoundedRarReader
	defer func() { openBoundedRarReader = original }()
	calls := 0
	openBoundedRarReader = func(path string, opts ...rardecode.Option) (*rardecode.ReadCloser, error) {
		// Apply the installed dependency's real options, without decoding a new payload.
		typ := reflect.TypeOf(opts[0]).In(0)
		cfg := reflect.New(typ.Elem())
		for _, opt := range opts {
			reflect.ValueOf(opt).Call([]reflect.Value{cfg})
		}
		if got := cfg.Elem().FieldByName("maxDictSize").Int(); got != BoundedPageDictionaryBytes {
			t.Fatalf("dictionary=%d", got)
		}
		if cfg.Elem().FieldByName("fs").IsNil() {
			t.Fatal("missing cancellable filesystem")
		}
		calls++
		return original(path, opts...)
	}
	pages, err := GetPagesLimited(context.Background(), arc)
	if err != nil || len(pages) != 4 {
		t.Fatalf("manifest=%v, %v", pages, err)
	}
	data, err := ReadPageLimited(context.Background(), arc, "10.jpg", 16<<20)
	if err != nil || string(data) != "j10" {
		t.Fatalf("page=%q, %v", data, err)
	}
	if calls != 2 {
		t.Fatalf("bounded opens=%d", calls)
	}
	if a := arc.(*RarArchive); a.rr != nil || len(a.cache) != 0 {
		t.Fatal("bounded evidence populated ordinary session")
	}
	// Ordinary reading and scanning retain their existing reader defaults.
	if _, err = arc.GetPages(); err != nil {
		t.Fatal(err)
	}
	if _, err = arc.ReadPage("1.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err = arc.ReadMetadataFile("ComicInfo.xml"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("ordinary path changed")
	}
	openBoundedRarReader = func(string, ...rardecode.Option) (*rardecode.ReadCloser, error) {
		return nil, rardecode.ErrDictionaryTooLarge
	}
	if _, err = GetPagesLimited(context.Background(), arc); !errors.Is(err, ErrMetadataTooLarge) {
		t.Fatalf("manifest limit=%v", err)
	}
	if _, err = ReadPageLimited(context.Background(), arc, "1.jpg", 16<<20); !errors.Is(err, ErrMetadataTooLarge) {
		t.Fatalf("page limit=%v", err)
	}
}

func TestBoundedManifestCancellation(t *testing.T) {
	arc, err := OpenArchive(testRarPath)
	if err != nil {
		t.Fatal(err)
	}
	defer arc.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = GetPagesLimited(ctx, arc); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = ReadPageLimited(ctx, arc, "1.jpg", 16<<20); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// A deterministic cancellation boundary: no timers or large archive payloads.
type manifestCancelContext struct {
	context.Context
	checks int
}

func (c *manifestCancelContext) Err() error {
	c.checks++
	if c.checks >= 6 {
		return context.Canceled
	}
	return nil
}
func TestBoundedManifestCancelsDuringEnumeration(t *testing.T) {
	arc, err := OpenArchive(testRarPath)
	if err != nil {
		t.Fatal(err)
	}
	defer arc.Close()
	ctx := &manifestCancelContext{Context: context.Background()}
	pages, err := GetPagesLimited(ctx, arc)
	if !errors.Is(err, context.Canceled) || pages != nil {
		t.Fatalf("pages=%v err=%v checks=%d", pages, err, ctx.checks)
	}
}

type cancellingPageReader struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancellingPageReader) Read(p []byte) (int, error) {
	r.reads++
	r.cancel()
	p[0] = 0
	return 1, nil
}
func TestBoundedRarSkipObservesBytesAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &cancellingPageReader{cancel: cancel}
	if err := skipBoundedPage(ctx, r, 1, 16<<20); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r.reads != 1 {
		t.Fatalf("read after cancellation: %d", r.reads)
	}
	if err := skipBoundedPage(context.Background(), strings.NewReader("12345"), 0, 4); !errors.Is(err, ErrMetadataTooLarge) {
		t.Fatalf("actual bytes: %v", err)
	}
	if err := skipBoundedPage(context.Background(), strings.NewReader(""), 5, 4); !errors.Is(err, ErrMetadataTooLarge) {
		t.Fatalf("declared bytes: %v", err)
	}
}

func TestBoundedRarVolumeReadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f, err := (pageContextFS{ctx}).Open(testRarPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cancel()
	if _, err = f.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = (pageContextFS{ctx}).Open(testRarPath); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
