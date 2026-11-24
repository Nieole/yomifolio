package parser

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwaples/rardecode/v2"
)

// Minimal RAR5 wire input, not a decoder/error substitute. Stored payloads exercise
// real traversal; the compressed header exercises dictionary rejection before allocation.
func evidenceRar5(t *testing.T, solid bool, sizes []int, comp uint64) string {
	t.Helper()
	var wire = []byte("Rar!\x1a\x07\x01\x00")
	vint := func(v uint64) []byte { return binary.AppendUvarint(nil, v) }
	block := func(kind, flags uint64, data, payload []byte) {
		h := append(vint(kind), vint(flags)...)
		if flags&2 != 0 {
			h = append(h, vint(uint64(len(payload)))...)
		}
		h = append(h, data...)
		h = append(vint(uint64(len(h))), h...)
		wire = binary.LittleEndian.AppendUint32(wire, crc32.ChecksumIEEE(h))
		wire = append(wire, h...)
		wire = append(wire, payload...)
	}
	main := uint64(0)
	if solid {
		main = 4
	}
	block(1, 0, vint(main), nil)
	for i, size := range sizes {
		name := "predecessor.bin"
		if i == len(sizes)-1 {
			name = "ComicInfo.xml"
		}
		flags := comp
		if solid && i > 0 {
			flags |= 64
		}
		data := vint(0)
		data = append(data, vint(uint64(size))...)
		data = append(data, vint(0)...)
		data = append(data, vint(flags)...)
		data = append(data, vint(1)...)
		data = append(data, vint(uint64(len(name)))...)
		data = append(data, name...)
		payload := []byte(strings.Repeat("x", size))
		if size == 12 {
			payload = []byte("<ComicInfo/>")
		}
		if comp != 0 {
			payload = nil
		}
		block(2, 2, data, payload)
	}
	block(5, 0, vint(0), nil)
	path := filepath.Join(t.TempDir(), "evidence.cbr")
	if err := os.WriteFile(path, wire, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestComicInfoRealRarBudgets(t *testing.T) {
	for _, solid := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-solid", true: "solid"}[solid], func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				sizes []int
				comp  uint64
				want  error
			}{
				{"small", []int{2, 12}, 0, nil},
				{"xml-limit", []int{2, (64 << 10) + 1}, 0, ErrMetadataTooLarge},
				{"cumulative-drain", []int{9 << 20, 9 << 20, 12}, 0, ErrMetadataTooLarge},
				{"dictionary", []int{32 << 20}, 128 | (8 << 10), ErrMetadataTooLarge},
			} {
				t.Run(tc.name, func(t *testing.T) {
					path := evidenceRar5(t, solid, tc.sizes, tc.comp)
					// The actual dependency accepts the archive header (no fake error seam).
					rr, err := rardecode.OpenReader(path)
					if err != nil {
						t.Fatal(err)
					}
					rr.Close()
					arc, err := OpenArchive(path)
					if err != nil {
						t.Fatal(err)
					}
					defer arc.Close()
					want := tc.want
					if solid {
						want = ErrRarEvidenceUnsupported
					}
					data, err := ReadMetadataLimited(context.Background(), arc, "ComicInfo.xml", 64<<10)
					if !errors.Is(err, want) {
						t.Fatalf("metadata bytes=%d error=%v want=%v", len(data), err, want)
					}
					if want == nil && len(data) != 12 {
						t.Fatalf("bytes=%d", len(data))
					}
					if tc.name == "dictionary" || tc.name == "cumulative-drain" {
						if _, err := GetPagesLimited(context.Background(), arc); !errors.Is(err, want) {
							t.Fatalf("directory: %v", err)
						}
						if _, err := ReadPageLimited(context.Background(), arc, "ComicInfo.xml", 16<<20); !errors.Is(err, want) {
							t.Fatalf("selected: %v", err)
						}
					}
				})
			}
		})
	}
}
