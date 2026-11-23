package enrichment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"yomifolio/internal/seriesmeta"
)

// These command-only V1 shapes are a persistent byte contract, independent of
// Input, response State and receipt versions. Field order, names and nulls in the
// base08 prefix MUST stay unchanged. No digest fallback or DB migration is needed.
// New command semantics need explicit optional extensions (absent means old
// semantics), or a new version with an explicit discriminator; never hash State.
// Do not add a non-optional version field to V1: old stored hashes have none.
type expectedV1 struct {
	Title     *string    `json:"title"`
	Summary   *string    `json:"summary"`
	Publisher *string    `json:"publisher"`
	Status    *string    `json:"status"`
	Rating    *float64   `json:"rating"`
	Tags      []string   `json:"tags"`
	Links     []linkV1   `json:"links"`
	Authors   []authorV1 `json:"authors,omitempty"`
}
type linkV1 struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type authorV1 struct {
	Name string `json:"name"`
	Role string `json:"role"`
}
type correctionV1 struct {
	From   authorV1 `json:"from"`
	To     authorV1 `json:"to"`
	Reason string   `json:"reason"`
}
type applyCommandV1 struct {
	RequestKey        string         `json:"request_key"`
	SeriesID          int64          `json:"series_id"`
	ProposalIDs       []int64        `json:"proposal_ids"`
	Fields            []string       `json:"fields"`
	Expected          expectedV1     `json:"expected"`
	AuthorCorrections []correctionV1 `json:"author_corrections,omitempty"`
}
type undoCommandV1 struct {
	RequestKey  string `json:"request_key"`
	OperationID string `json:"operation_id"`
}

func applyDigest(in Input) string {
	expected := expectedV1{Title: in.Expected.Title, Summary: in.Expected.Summary, Publisher: in.Expected.Publisher, Status: in.Expected.Status, Rating: in.Expected.Rating, Tags: in.Expected.Tags}
	// Preserve base08's nil versus empty link representation, as for tags above.
	if in.Expected.Links != nil {
		expected.Links = make([]linkV1, 0, len(in.Expected.Links))
	}
	for _, link := range in.Expected.Links {
		expected.Links = append(expected.Links, linkV1{Name: link.Name, URL: link.Url})
	}
	// Authors are a set: nil/empty are equivalent and encode no V1 extension.
	// Nonempty identities and every explicit correction remain part of the intent.
	for _, a := range seriesmeta.AuthorSet(in.Expected.Authors) {
		expected.Authors = append(expected.Authors, authorV1{Name: a.Name, Role: a.Role})
	}
	var corrections []correctionV1
	for _, c := range in.AuthorCorrections {
		corrections = append(corrections, correctionV1{From: authorV1{Name: c.From.Name, Role: c.From.Role}, To: authorV1{Name: c.To.Name, Role: c.To.Role}, Reason: c.Reason})
	}
	return commandHash(applyCommandV1{RequestKey: in.RequestKey, SeriesID: in.SeriesID, ProposalIDs: in.ProposalIDs, Fields: in.Fields, Expected: expected, AuthorCorrections: corrections})
}
func undoDigest(in UndoInput) string {
	return commandHash(undoCommandV1{RequestKey: in.RequestKey, OperationID: in.OperationID})
}
func commandHash(command any) string {
	b, _ := json.Marshal(command)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
