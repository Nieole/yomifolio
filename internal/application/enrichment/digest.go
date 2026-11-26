package enrichment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"yomifolio/internal/seriesmeta"
)

// These command-only V1 shapes are a persistent byte contract: a stored request hash is checked
// against them on every retry, so field order, names and nulls MUST stay unchanged. New command
// semantics need explicit optional extensions (absent means old semantics), or a new command with
// its own leading discriminator; never hash State. Stored hashes of commands that no longer exist
// share the namespace: reusing such a key is a changed intent.
type authorV1 struct {
	Name string `json:"name"`
	Role string `json:"role"`
}
type correctionV1 struct {
	From   authorV1 `json:"from"`
	To     authorV1 `json:"to"`
	Reason string   `json:"reason"`
}
type undoCommandV1 struct {
	RequestKey  string `json:"request_key"`
	OperationID string `json:"operation_id"`
}

// relationCommandV1 is its own command, distinguished from the others by its leading discriminator.
type relationCommandV1 struct {
	Command        string `json:"command"`
	RequestKey     string `json:"request_key"`
	SeriesID       int64  `json:"series_id"`
	TargetSeriesID int64  `json:"target_series_id"`
	RelationType   string `json:"relation_type"`
	Reason         string `json:"reason"`
}

// authorsCommandV1 and coverCommandV1 are the primitives' own commands, each distinguished by its
// leading discriminator. Expected authors are a set, encoded as AuthorSet orders it.
type authorsCommandV1 struct {
	Command     string         `json:"command"`
	RequestKey  string         `json:"request_key"`
	SeriesID    int64          `json:"series_id"`
	Expected    []authorV1     `json:"expected_authors"`
	Corrections []correctionV1 `json:"corrections"`
}
type coverCommandV1 struct {
	Command    string `json:"command"`
	RequestKey string `json:"request_key"`
	SeriesID   int64  `json:"series_id"`
	BookID     int64  `json:"book_id"`
	Page       int64  `json:"page"`
	Reason     string `json:"reason"`
}

func authorsDigest(in AuthorsInput) string {
	command := authorsCommandV1{Command: "correct_authors", RequestKey: in.RequestKey, SeriesID: in.SeriesID, Expected: []authorV1{}, Corrections: []correctionV1{}}
	for _, a := range seriesmeta.AuthorSet(in.Expected) {
		command.Expected = append(command.Expected, authorV1{Name: a.Name, Role: a.Role})
	}
	for _, c := range in.Corrections {
		command.Corrections = append(command.Corrections, correctionV1{From: authorV1{Name: c.From.Name, Role: c.From.Role}, To: authorV1{Name: c.To.Name, Role: c.To.Role}, Reason: c.Reason})
	}
	return commandHash(command)
}
func coverDigest(in CoverInput) string {
	return commandHash(coverCommandV1{Command: "improve_cover", RequestKey: in.RequestKey, SeriesID: in.SeriesID, BookID: in.BookID, Page: in.Page, Reason: in.Reason})
}

func relationDigest(in RelationInput) string {
	return commandHash(relationCommandV1{Command: relationKind, RequestKey: in.RequestKey, SeriesID: in.SeriesID, TargetSeriesID: in.TargetSeriesID, RelationType: in.RelationType, Reason: in.Reason})
}
func undoDigest(in UndoInput) string {
	return commandHash(undoCommandV1{RequestKey: in.RequestKey, OperationID: in.OperationID})
}
func commandHash(command any) string {
	b, _ := json.Marshal(command)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
