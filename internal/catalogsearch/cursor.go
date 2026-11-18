package catalogsearch

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"

	"yomifolio/internal/database"
)

var ErrCursor = errors.New("invalid_search_cursor")
var ErrLimit = errors.New("invalid search limit")

// Fixed-width wire fields: format, order, query digest, target, reader,
// permission digest, tier, position, extra, kind, ID. Digests bind versioned
// JSON tuples, never delimiter concatenations. No unbounded text is serialized.
const cursorBytes = 93
const MaxCursorSize = 8 * 1024

func digest(value [2]string) [32]byte {
	raw, _ := json.Marshal(value)
	return sha256.Sum256(raw)
}

func cursorPrefix(query, target string, reader int64, authority string) []byte {
	b := make([]byte, 75)
	b[0], b[1] = 1, 1
	q := digest([2]string{"search-normalize-v1", database.NormalizeSearchText(query)})
	copy(b[2:34], q[:])
	switch target {
	case "all":
		b[34] = 0
	case "book":
		b[34] = 1
	case "series":
		b[34] = 2
	}
	binary.BigEndian.PutUint64(b[35:43], uint64(reader))
	p := digest([2]string{"catalogue-policy-v1", authority})
	copy(b[43:75], p[:])
	return b
}

func decodeCursor(raw string, prefix []byte) (*SortKey, error) {
	if len(raw) > MaxCursorSize {
		return nil, ErrCursor
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(b) != cursorBytes || base64.RawURLEncoding.EncodeToString(b) != raw || !bytes.Equal(b[:75], prefix) {
		return nil, ErrCursor
	}
	k := &SortKey{SearchMatchRank: database.SearchMatchRank{Tier: b[75], Position: binary.BigEndian.Uint32(b[76:80]), Extra: binary.BigEndian.Uint32(b[80:84])}, Kind: b[84], ID: int64(binary.BigEndian.Uint64(b[85:93]))}
	if k.Tier > 2 || k.Position > k.Extra || (k.Tier == 1 && k.Extra == 0) || k.Position > math.MaxInt32 || k.Extra > math.MaxInt32 || k.Kind > 1 || k.ID <= 0 || (k.Tier == 0 && (k.Position != 0 || k.Extra != 0)) || (k.Tier == 2 && k.Kind != 0) || (prefix[34] == 1 && k.Kind != 0) || (prefix[34] == 2 && k.Kind != 1) {
		return nil, ErrCursor
	}
	return k, nil
}

func encodeCursor(prefix []byte, k SortKey) string {
	b := make([]byte, cursorBytes)
	copy(b, prefix)
	b[75] = k.Tier
	binary.BigEndian.PutUint32(b[76:80], k.Position)
	binary.BigEndian.PutUint32(b[80:84], k.Extra)
	b[84] = k.Kind
	binary.BigEndian.PutUint64(b[85:93], uint64(k.ID))
	return base64.RawURLEncoding.EncodeToString(b)
}
