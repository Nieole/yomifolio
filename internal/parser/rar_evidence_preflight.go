package parser

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
)

// ErrRarEvidenceUnsupported reports layouts for which the SDK cannot guarantee
// bounded decoder advancement. Ordinary RAR reading is not restricted.
var ErrRarEvidenceUnsupported = errors.New("RAR evidence unavailable: unsupported archive layout")

// The SDK hides decoder output behind UnPackedSize, then drains the remainder
// in decodeReader.nextFile when arcSolid is set (even if FileHeader.Solid is
// false). There is no output-budget hook. Classify raw headers without decoding,
// on the SAME open file that the SDK will consume; never reopen after approval.
// This is deliberately not a general RAR parser: unknown blocks fail closed.
func preflightRarEvidence(ctx context.Context, f *os.File) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	p := rarPreflight{ctx: ctx, f: f, size: st.Size(), remaining: 16 << 20}
	var signature [8]byte
	if err = p.read(signature[:7]); err != nil {
		return err
	}
	switch string(signature[:7]) {
	case "Rar!\x1a\x07\x00":
		err = p.rar4()
	case "Rar!\x1a\x07\x01":
		if err = p.read(signature[7:]); err == nil {
			if signature[7] != 0 {
				return ErrRarEvidenceUnsupported
			}
			err = p.rar5()
		}
	default:
		return ErrRarEvidenceUnsupported // including SFX / unknown signatures
	}
	if err != nil {
		return err
	}
	_, err = f.Seek(0, io.SeekStart)
	return err
}

type rarPreflight struct {
	ctx                  context.Context
	f                    *os.File
	size, off, remaining int64
}

func (p *rarPreflight) read(b []byte) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if int64(len(b)) > p.remaining || int64(len(b)) > p.size-p.off {
		return ErrRarEvidenceUnsupported
	}
	if _, err := io.ReadFull(p.f, b); err != nil {
		return err
	}
	p.off += int64(len(b))
	p.remaining -= int64(len(b))
	return nil
}
func (p *rarPreflight) skip(n uint64) error {
	if n > uint64(p.size-p.off) {
		return ErrRarEvidenceUnsupported
	}
	p.off += int64(n)
	_, err := p.f.Seek(p.off, io.SeekStart)
	return err
}

// Strict varints reject truncation and overflow rather than guessing a boundary.
func rarVint(b *[]byte) (uint64, bool) {
	v, n := binary.Uvarint(*b)
	if n <= 0 {
		return 0, false
	}
	*b = (*b)[n:]
	return v, true
}
func (p *rarPreflight) rar5() error {
	for blocks := 0; blocks < 65536; blocks++ {
		var crc [4]byte
		if err := p.read(crc[:]); err != nil {
			return err
		}
		var sizeBytes []byte
		for range 10 {
			var b [1]byte
			if err := p.read(b[:]); err != nil {
				return err
			}
			sizeBytes = append(sizeBytes, b[0])
			if b[0] < 128 {
				break
			}
		}
		tmp := sizeBytes
		size, ok := rarVint(&tmp)
		if !ok || size < 3 || size > 1<<20 {
			return ErrRarEvidenceUnsupported
		}
		body := make([]byte, int(size))
		if err := p.read(body); err != nil {
			return err
		}
		hash := crc32.NewIEEE()
		_, _ = hash.Write(sizeBytes)
		_, _ = hash.Write(body)
		if hash.Sum32() != binary.LittleEndian.Uint32(crc[:]) {
			return ErrRarEvidenceUnsupported
		}
		b := body
		kind, ok := rarVint(&b)
		if !ok {
			return ErrRarEvidenceUnsupported
		}
		flags, ok := rarVint(&b)
		if !ok || flags & ^uint64(7) != 0 {
			return ErrRarEvidenceUnsupported
		} // split/unknown flags
		var extra, packed uint64
		if flags&1 != 0 {
			extra, ok = rarVint(&b)
			if !ok {
				return ErrRarEvidenceUnsupported
			}
		}
		if flags&2 != 0 {
			packed, ok = rarVint(&b)
			if !ok {
				return ErrRarEvidenceUnsupported
			}
		}
		if extra > uint64(len(b)) {
			return ErrRarEvidenceUnsupported
		}
		data, extras := b[:uint64(len(b))-extra], b[uint64(len(b))-extra:]
		if blocks == 0 {
			// Main flags: reject global solid, multi-volume, volume numbering and unknown flags.
			main, valid := rarVint(&data)
			if kind != 1 || !valid || main != 0 || packed != 0 {
				return ErrRarEvidenceUnsupported
			}
		} else {
			switch kind {
			case 2:
				fileFlags, valid := rarVint(&data)
				if !valid || fileFlags & ^uint64(7) != 0 {
					return ErrRarEvidenceUnsupported
				} // unknown size included
				size, valid := rarVint(&data)
				if !valid || size > uint64(^uint64(0)>>1) {
					return ErrRarEvidenceUnsupported
				}
				if _, valid = rarVint(&data); !valid {
					return ErrRarEvidenceUnsupported
				} // attributes
				fixed := 0
				if fileFlags&2 != 0 {
					fixed += 4
				}
				if fileFlags&4 != 0 {
					fixed += 4
				}
				if len(data) < fixed {
					return ErrRarEvidenceUnsupported
				}
				data = data[fixed:]
				comp, valid := rarVint(&data)
				if !valid || comp&64 != 0 {
					return ErrRarEvidenceUnsupported
				}
				if _, valid = rarVint(&data); !valid {
					return ErrRarEvidenceUnsupported
				} // host
				name, valid := rarVint(&data)
				if !valid || name > uint64(len(data)) {
					return ErrRarEvidenceUnsupported
				}
			case 5:
				end, valid := rarVint(&data)
				if !valid || end != 0 || packed != 0 || extra != 0 {
					return ErrRarEvidenceUnsupported
				}
				return nil
			default:
				return ErrRarEvidenceUnsupported // encrypted headers, services, unknown blocks
			}
		}
		// Extra records are length delimited. Reject encryption before the SDK sees it;
		// opaque non-encryption records cannot change arcSolid or decoder advancement.
		for len(extras) > 0 {
			n, valid := rarVint(&extras)
			if !valid || n > uint64(len(extras)) {
				return ErrRarEvidenceUnsupported
			}
			record := extras[:n]
			extras = extras[n:]
			kind, valid := rarVint(&record)
			if !valid || kind == 1 && blocks != 0 {
				return ErrRarEvidenceUnsupported
			}
		}
		if err := p.skip(packed); err != nil {
			return err
		}
	}
	return ErrRarEvidenceUnsupported
}
func (p *rarPreflight) rar4() error {
	for blocks := 0; blocks < 65536; blocks++ {
		if p.off == p.size && blocks > 0 {
			return nil
		} // RAR4 permits no end block
		var prefix [7]byte
		if err := p.read(prefix[:]); err != nil {
			return err
		}
		kind := prefix[2]
		flags := binary.LittleEndian.Uint16(prefix[3:5])
		n := int(binary.LittleEndian.Uint16(prefix[5:]))
		if n < 7 {
			return ErrRarEvidenceUnsupported
		}
		header := make([]byte, n)
		copy(header, prefix[:])
		if err := p.read(header[7:]); err != nil {
			return err
		}
		if uint16(crc32.ChecksumIEEE(header[2:])) != binary.LittleEndian.Uint16(header[:2]) {
			return ErrRarEvidenceUnsupported
		}
		var packed uint64
		if blocks == 0 {
			if kind != 0x73 || n != 13 || flags & ^uint16(0x110) != 0 {
				return ErrRarEvidenceUnsupported
			} // only naming/first-volume flags; no volume, solid, encryption, comment
		} else {
			switch kind {
			case 0x74:
				if n < 32 || flags&0x8000 == 0 || flags&(1|2|4|8|16|0x400) != 0 {
					return ErrRarEvidenceUnsupported
				}
				packed = uint64(binary.LittleEndian.Uint32(header[7:11]))
				unpacked := uint64(binary.LittleEndian.Uint32(header[11:15]))
				nameOffset := 32
				if flags&0x100 != 0 {
					if n < 40 {
						return ErrRarEvidenceUnsupported
					}
					packed |= uint64(binary.LittleEndian.Uint32(header[32:36])) << 32
					unpacked |= uint64(binary.LittleEndian.Uint32(header[36:40])) << 32
					nameOffset = 40
				} else if unpacked == 0xffffffff {
					return ErrRarEvidenceUnsupported
				}
				if unpacked > uint64(^uint64(0)>>1) || int(binary.LittleEndian.Uint16(header[26:28])) > n-nameOffset {
					return ErrRarEvidenceUnsupported
				}
			case 0x7b:
				if flags&1 != 0 || flags&0x8000 != 0 {
					return ErrRarEvidenceUnsupported
				}
				return nil
			default:
				return ErrRarEvidenceUnsupported
			}
		}
		if err := p.skip(packed); err != nil {
			return err
		}
	}
	return ErrRarEvidenceUnsupported
}
