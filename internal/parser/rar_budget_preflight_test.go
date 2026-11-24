package parser

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwaples/rardecode/v2"
)

type tracedRarFS struct{ bytes *int64 }

func (f tracedRarFS) Open(name string) (fs.File, error) {
	r, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return tracedRarFile{r, f.bytes}, nil
}

type tracedRarFile struct {
	fs.File
	bytes *int64
}

func (f tracedRarFile) Read(p []byte) (int, error) {
	n, e := f.File.Read(p)
	*f.bytes += int64(n)
	return n, e
}

// This is a real RAR5 compressed stream, not a stored payload with solid bits.
// The original emits 24 MiB. The hostile copy removes the first checksum and
// changes only its declared size to 1, with a recomputed header CRC.
func TestRarDependencyHiddenSolidDrain(t *testing.T) {
	rr, err := rardecode.OpenReader("testdata/solid-compressed.cbr")
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	h, err := rr.Next()
	if err != nil || h.Solid {
		t.Fatalf("first header: %+v %v", h, err)
	}
	n, err := io.Copy(io.Discard, rr)
	if err != nil || n != 24<<20 {
		t.Fatalf("original output=%d %v", n, err)
	}
	var bytes int64
	hostile, err := rardecode.OpenReader("testdata/solid-undersized.cbr", rardecode.FileSystem(tracedRarFS{&bytes}), rardecode.BufferSize(1024))
	if err != nil {
		t.Fatal(err)
	}
	defer hostile.Close()
	h, err = hostile.Next()
	if err != nil || h.Solid || h.UnPackedSize != 1 {
		t.Fatalf("hostile header: %+v %v", h, err)
	}
	n, err = io.Copy(io.Discard, hostile)
	if err != nil || n != 1 {
		t.Fatalf("visible output=%d %v", n, err)
	}
	before := bytes
	h, err = hostile.Next()
	if err != nil || h.Name != "ComicInfo.xml" {
		t.Fatalf("next: %+v %v", h, err)
	}
	if bytes-before < 1024 {
		t.Fatalf("Next did not consume hidden compressed remainder: %d", bytes-before)
	}
	t.Logf("visible output 1; Next consumed %d additional compressed bytes from the original 24 MiB stream", bytes-before)
}

func TestBoundedRarRejectsGlobalSolidBeforeDecoder(t *testing.T) {
	for _, path := range []string{"testdata/solid-compressed.cbr", "testdata/solid-undersized.cbr"} {
		t.Run(path, func(t *testing.T) {
			arc, err := OpenArchive(path)
			if err != nil {
				t.Fatal(err)
			}
			defer arc.Close()
			for _, kind := range []string{"directory", "page", "ComicInfo"} {
				var err error
				switch kind {
				case "directory":
					_, err = GetPagesLimited(context.Background(), arc)
				case "page":
					_, err = ReadPageLimited(context.Background(), arc, "page.jpg", 16<<20)
				case "ComicInfo":
					_, err = ReadMetadataLimited(context.Background(), arc, "ComicInfo.xml", 64<<10)
				}
				// Strict classification, not an incidental decoder or declared-size error.
				if err == nil || !errors.Is(err, ErrRarEvidenceUnsupported) {
					t.Errorf("%s: %v", kind, err)
				}
			}
		})
	}
}

func TestBoundedNonSolidCompressedAndOrdinarySolid(t *testing.T) {
	for _, path := range []string{"testdata/nonsolid-compressed.cbr", "testdata/solid-compressed.cbr"} {
		arc, err := OpenArchive(path)
		if err != nil {
			t.Fatal(err)
		}
		defer arc.Close()
		data, err := arc.ReadMetadataFile("ComicInfo.xml")
		if err != nil || string(data) != "<ComicInfo/>" {
			t.Fatalf("ordinary metadata: %q %v", data, err)
		}
		data, err = arc.ReadPage("page.jpg")
		if err != nil || len(data) == 0 {
			t.Fatalf("ordinary page: %q %v", data, err)
		}
		if path == "testdata/nonsolid-compressed.cbr" {
			data, err = ReadMetadataLimited(t.Context(), arc, "ComicInfo.xml", 64<<10)
			if err != nil || string(data) != "<ComicInfo/>" {
				t.Fatalf("bounded metadata: %q %v", data, err)
			}
			pages, err := GetPagesLimited(t.Context(), arc)
			if err != nil || len(pages) != 1 {
				t.Fatalf("bounded directory: %+v %v", pages, err)
			}
			data, err = ReadPageLimited(t.Context(), arc, "page.jpg", 16<<20)
			if err != nil || len(data) != 2048 {
				t.Fatalf("bounded page: %d %v", len(data), err)
			}
		}
	}
}

// Raw, CRC-valid RAR4/5 headers classify archive-wide flags independently from
// FileHeader.Solid. No external compressor is needed to maintain these cases.
func TestBoundedRarHeaderClassification(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, kind := range []string{"plain", "global-solid", "multi-volume", "encrypted-header", "split", "unknown-size", "encrypted-file", "file-solid", "unknown-block", "truncated", "bad-crc"} {
			t.Run(fmt.Sprintf("rar%d/%s", version, kind), func(t *testing.T) {
				var wire []byte
				if version == 5 {
					wire = classificationRar5(kind)
				} else {
					wire = classificationRar4(kind)
				}
				path := filepath.Join(t.TempDir(), "book.cbr")
				if err := os.WriteFile(path, wire, 0600); err != nil {
					t.Fatal(err)
				}
				arc, err := OpenArchive(path)
				if err != nil {
					t.Fatal(err)
				}
				defer arc.Close()
				_, err = GetPagesLimited(t.Context(), arc)
				if kind == "plain" {
					if err != nil {
						t.Fatal(err)
					}
				} else if !errors.Is(err, ErrRarEvidenceUnsupported) {
					t.Fatalf("classification=%v", err)
				}
			})
		}
	}
}

func classificationRar5(kind string) []byte {
	wire := []byte("Rar!\x1a\x07\x01\x00")
	block := func(typ, flags uint64, body []byte) {
		h := binary.AppendUvarint(nil, typ)
		h = binary.AppendUvarint(h, flags)
		h = append(h, body...)
		h = append(binary.AppendUvarint(nil, uint64(len(h))), h...)
		wire = binary.LittleEndian.AppendUint32(wire, crc32.ChecksumIEEE(h))
		wire = append(wire, h...)
	}
	main := uint64(0)
	if kind == "global-solid" {
		main = 4
	}
	if kind == "multi-volume" {
		main = 1
	}
	if kind == "encrypted-header" {
		block(4, 0, []byte{0, 0, 0})
	}
	block(1, 0, binary.AppendUvarint(nil, main))
	flags, ff, comp := uint64(2), uint64(0), uint64(0)
	if kind == "split" {
		flags |= 8
	}
	if kind == "unknown-size" {
		ff = 8
	}
	if kind == "file-solid" {
		comp = 64
	}
	if kind == "encrypted-file" {
		flags |= 1
	}
	body := []byte{}
	if flags&1 != 0 {
		body = append(body, 2)
	}
	body = append(body, 0) // packed=0
	body = binary.AppendUvarint(body, ff)
	body = append(body, 0, 0)
	body = binary.AppendUvarint(body, comp)
	body = append(body, 1, 1, 'x')
	if flags&1 != 0 {
		body = append(body, 1, 1)
	} // length=1, type=crypt
	block(2, flags, body)
	if kind == "unknown-block" {
		block(99, 0, []byte{0})
	}
	block(5, 0, []byte{0})
	if kind == "truncated" {
		wire = wire[:len(wire)-1]
	}
	if kind == "bad-crc" {
		wire[8] ^= 1
	}
	return wire
}

func classificationRar4(kind string) []byte {
	wire := []byte("Rar!\x1a\x07\x00")
	block := func(typ byte, flags uint16, body []byte) {
		h := []byte{typ}
		h = binary.LittleEndian.AppendUint16(h, flags)
		h = binary.LittleEndian.AppendUint16(h, uint16(len(body)+7))
		h = append(h, body...)
		wire = binary.LittleEndian.AppendUint16(wire, uint16(crc32.ChecksumIEEE(h)))
		wire = append(wire, h...)
	}
	main := uint16(0)
	if kind == "global-solid" {
		main = 8
	}
	if kind == "multi-volume" {
		main = 1
	}
	if kind == "encrypted-header" {
		main = 128
	}
	block(0x73, main, make([]byte, 6))
	flags := uint16(0x8000)
	if kind == "split" {
		flags |= 1
	}
	if kind == "encrypted-file" {
		flags |= 4
	}
	if kind == "file-solid" {
		flags |= 16
	}
	body := make([]byte, 26)
	body[25] = 'x'
	// Body starts at header offset7: PACK_SIZE(4), UNP_SIZE(4), OS(1),
	// CRC(4), TIME(4), VER(1), METHOD(1), NAME_SIZE(2), ATTR(4), NAME.
	body[17] = 20
	body[18] = 0x30
	binary.LittleEndian.PutUint16(body[19:21], 1)
	if kind == "unknown-size" {
		binary.LittleEndian.PutUint32(body[4:8], 0xffffffff)
	}
	block(0x74, flags, body)
	if kind == "unknown-block" {
		block(0x78, 0, nil)
	}
	block(0x7b, 0, nil)
	if kind == "truncated" {
		wire = wire[:len(wire)-1]
	}
	if kind == "bad-crc" {
		wire[7] ^= 1
	}
	return wire
}
