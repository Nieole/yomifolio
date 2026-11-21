package images

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"

	"golang.org/x/image/draw"
)

const (
	EvidenceEdge        = 384
	EvidenceSourceBytes = 16 << 20
	EvidenceImageBytes  = 96 << 10
	EvidencePixels      = 16_000_000
)

var ErrEvidenceLimit = errors.New("image evidence exceeds safe budget")

type EvidenceImage struct {
	Width, Height int
	Data          []byte
}

// EvidenceThumbnail always decodes and encodes a bounded JPEG; no raw fallback.
// Callers must bound concurrent materialization before reading input bytes.
func EvidenceThumbnail(ctx context.Context, data []byte) (EvidenceImage, error) {
	var out EvidenceImage
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if len(data) > EvidenceSourceBytes {
		return out, ErrEvidenceLimit
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return out, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 16384 || cfg.Height > 16384 || int64(cfg.Width)*int64(cfg.Height) > EvidencePixels {
		return out, ErrEvidenceLimit
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	w, h := cfg.Width, cfg.Height
	if w > EvidenceEdge || h > EvidenceEdge {
		if w >= h {
			h = max(1, h*EvidenceEdge/w)
			w = EvidenceEdge
		} else {
			w = max(1, w*EvidenceEdge/h)
			h = EvidenceEdge
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var buf bytes.Buffer
	if err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 65}); err != nil {
		return out, err
	}
	if buf.Len() > EvidenceImageBytes {
		return out, ErrEvidenceLimit
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return EvidenceImage{Width: w, Height: h, Data: buf.Bytes()}, nil
}
