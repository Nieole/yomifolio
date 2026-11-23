package seriesmeta

import (
	"cmp"
	"slices"
)

// AuthorSet compares exact persisted identities, independent of ordering or repetition.
// Names and roles are not case-folded or trimmed: those changes require explicit correction.
func AuthorSet(authors []Author) []Author {
	out := append([]Author{}, authors...)
	slices.SortFunc(out, func(a, b Author) int {
		if n := cmp.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return cmp.Compare(a.Role, b.Role)
	})
	return slices.Compact(out)
}
