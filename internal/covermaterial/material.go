// Package covermaterial prepares immutable cover resources before database publication.
// Its namespace is excluded from ordinary thumbnail cleanup; only committed references
// may be served. Orphans have no automatic expiry; explicit receipt-scoped cleanup
// must durably fence publication before removing any resource.
package covermaterial

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"yomifolio/internal/images"
)

const Directory = "metadata-covers"

// RootDir keeps preparations outside the filename-served thumbnail tree. Public
// metadata-covers keys are virtual references, not a directory of that tree.
func RootDir(thumbnails string) string { return filepath.Clean(thumbnails) + ".metadata-covers" }

// FilePath resolves only server-generated virtual keys; callers must authorize them.
func FilePath(thumbnails, key string) string { return filepath.Join(RootDir(thumbnails), key) }

// IsPath includes normalized aliases so file-name HTTP routes cannot bypass publication.
func IsPath(name string) bool {
	name = strings.TrimLeft(strings.ToLower(path.Clean(strings.ReplaceAll(name, "\\", "/"))), "/")
	return name == Directory || strings.HasPrefix(name, Directory+"/")
}

// Prepare writes and syncs a fresh, non-overwriting resource. It never exposes a partial
// file under an existing key. A write/close failure removes only this call's unique file.
// The caller must publish the returned key in its metadata transaction, never rename
// or modify the resource afterward. Network and decoding costs are not reversible.
func Prepare(ctx context.Context, dir string, data []byte, extension string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > images.EvidenceSourceBytes {
		return "", images.ErrEvidenceLimit
	}
	if extension != ".jpg" && extension != ".png" && extension != ".webp" && extension != ".avif" {
		extension = ".img"
	}
	dir = RootDir(dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = root.MkdirAll(Directory, 0700); err != nil {
		return "", err
	}
	var id [24]byte
	if _, err = rand.Read(id[:]); err != nil {
		return "", err
	}
	key := Directory + "/" + hex.EncodeToString(id[:]) + extension
	file, err := root.OpenFile(key, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = root.Remove(key)
		return "", err
	}
	return key, nil
}

// Read confines the existing cache key and bounds source bytes before decoding.
func Read(ctx context.Context, dir, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if IsPath(key) {
		dir = RootDir(dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(key)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, images.EvidenceSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > images.EvidenceSourceBytes {
		return nil, images.ErrEvidenceLimit
	}
	return data, ctx.Err()
}
