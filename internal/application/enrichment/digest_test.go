package enrichment

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"yomifolio/internal/seriesmeta"
)

// Frozen from the explicit V1 shapes at a92df90f (09, before cover merge).
// Keep these bytes independent of the current response/command structs.
func TestEnrichmentDigest09FrozenAndCoverIntent(t *testing.T) {
	in := Input{RequestKey: "nine", SeriesID: 7, ProposalIDs: []int64{3}, Fields: []string{"authors"}, Expected: ExpectedState{Authors: []seriesmeta.Author{{Name: "Alias", Role: "Artist"}}}, AuthorCorrections: []AuthorCorrection{{From: seriesmeta.Author{Name: "Alias", Role: "Artist"}, To: seriesmeta.Author{Name: "Canonical", Role: "Writer"}, Reason: "Published credit"}}}
	frozen := `{"request_key":"nine","series_id":7,"proposal_ids":[3],"fields":["authors"],"expected":{"title":null,"summary":null,"publisher":null,"status":null,"rating":null,"tags":null,"links":null,"authors":[{"name":"Alias","role":"Artist"}]},"author_corrections":[{"from":{"name":"Alias","role":"Artist"},"to":{"name":"Canonical","role":"Writer"},"reason":"Published credit"}]}`
	if got, want := applyDigest(in), commandHashRaw(frozen); got != want {
		t.Fatalf("09 digest changed: %s != %s", got, want)
	}
	base := applyDigest(in)
	in.Cover = &CoverSelection{BookID: 4, Page: 2, QualityCompared: true, Reason: "Complete artwork"}
	in.Expected.Cover = &seriesmeta.CoverState{BookID: 4, Path: "auto.png", Automatic: true}
	covered := applyDigest(in)
	if covered == base {
		t.Fatal("cover intent omitted")
	}
	mutations := []func(){func() { in.Cover.BookID++ }, func() { in.Cover.Page++ }, func() { in.Cover.QualityCompared = false }, func() { in.Cover.Reason += " clearer" }, func() { in.Expected.Cover.BookID++ }, func() { in.Expected.Cover.Path += "x" }, func() { in.Expected.Cover.Locked = true }, func() { in.Expected.Cover.Automatic = false }, func() { in.Expected.Authors[0].Role = "Writer" }, func() { in.AuthorCorrections[0].Reason += " revised" }, func() { in.AuthorCorrections[0].To.Role = "Artist" }}
	for i, mutate := range mutations {
		before := applyDigest(in)
		mutate()
		if applyDigest(in) == before {
			t.Fatalf("intent mutation %d omitted", i)
		}
	}
	in.Expected.Authors = nil
	nilDigest := applyDigest(in)
	in.Expected.Authors = []seriesmeta.Author{}
	if applyDigest(in) != nilDigest {
		t.Fatal("nil and empty author sets differ")
	}
}

func commandHashRaw(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
