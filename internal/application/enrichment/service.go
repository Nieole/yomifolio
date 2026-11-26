// Package enrichment owns atomic, authorized metadata supplementation, franchise relation additions and
// their durable undo.
// Receipts are immutable operations, not a projection of latest field provenance.
package enrichment

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
)

// State is the source-specific metadata snapshot used for expected-value checks.
type State = seriesmeta.State

type UndoInput struct {
	RequestKey  string `json:"request_key"`
	OperationID string `json:"operation_id"`
}
type FieldAudit struct {
	Field   string                   `json:"field"`
	Changed bool                     `json:"changed"`
	Reason  string                   `json:"reason"`
	Sources []metadata.FieldEvidence `json:"sources"`
	Formula string                   `json:"formula,omitempty"`
}
type Operation struct {
	UndoUnavailable bool                `json:"undo_unavailable,omitempty"`
	CleanedBy       string              `json:"cleaned_by,omitempty"`
	Adjudication    *AdjudicationResult `json:"adjudication,omitempty"`
	// Identification is set on the receipts of supplementation after identification.
	Identification   *IdentificationBasis `json:"identification,omitempty"`
	CoverSelection   *CoverSelection      `json:"cover_selection,omitempty"`
	Relation         *RelationChange      `json:"relation,omitempty"`
	OldCoverMaterial string               `json:"old_cover_material,omitempty"`
	NewCoverMaterial string               `json:"new_cover_material,omitempty"`
	Version          int                  `json:"version"`
	ID               string               `json:"id"`
	RequestKey       string               `json:"request_key"`
	Actor            delegated.Subject    `json:"actor"`
	SeriesID         int64                `json:"series_id"`
	LibraryID        int64                `json:"library_id"`
	Kind             string               `json:"kind"`
	ParentID         string               `json:"parent_id,omitempty"`
	CreatedAt        string               `json:"created_at"`
	Outcome          string               `json:"outcome"`
	Reason           string               `json:"reason"`
	Before           State                `json:"before"`
	After            State                `json:"after"`
	Fields           []FieldAudit         `json:"fields"`
	UndoneBy         string               `json:"undone_by,omitempty"`
	Applied          bool                 `json:"applied"`
	Replayed         bool                 `json:"replayed"`
}
type Page struct {
	Operations []Operation `json:"operations"`
	NextBefore int64       `json:"next_before,omitempty"`
}

// Service is synchronous; one series and its receipt commit together. No batch transaction,
// external calls, implicit retries, automatic expiry or process-local reversal state exists.
type Service struct {
	db               *sql.DB
	publish          func()
	cleanup          *CleanupAccess
	covers           *CoverAccess
	relationsChanged func()
	identified       IdentifiedSources
}

func New(db *sql.DB, publish func()) *Service {
	return &Service{db: db, publish: publish}
}
func validKey(key string) bool {
	if len(key) < 1 || len(key) > 80 {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func authorize(ctx context.Context, tx *sql.Tx, actor delegated.Subject, library int64, write bool) error {
	if actor.CredentialID == "" {
		if database.RequireMetadataAdmin(ctx, tx, actor.UserID) != nil {
			return delegated.ErrDenied
		}
		return nil
	}
	capability := delegated.Read
	if write {
		capability = delegated.MetadataWrite
	}
	return delegated.AuthorizeTx(ctx, tx, actor, library, capability)
}
func nullable(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}
func readState(ctx context.Context, q *database.Queries, id int64) (database.Series, State, error) {
	current, err := q.GetSeries(ctx, id)
	if err != nil {
		return current, State{}, err
	}
	state := State{Authors: []seriesmeta.Author{}, Title: nullable(current.Title), Summary: nullable(current.Summary), Publisher: nullable(current.Publisher), Status: nullable(current.Status), Tags: []string{}, Links: []seriesmeta.Link{}}
	if current.Rating.Valid {
		r := current.Rating.Float64
		state.Rating = &r
	}
	authors, err := q.GetAuthorsForSeries(ctx, id)
	if err != nil {
		return current, state, err
	}
	for _, a := range authors {
		state.Authors = append(state.Authors, seriesmeta.Author{Name: a.Name, Role: a.Role})
	}
	state.Authors = seriesmeta.AuthorSet(state.Authors)

	tags, err := q.GetTagsForSeries(ctx, id)
	if err != nil {
		return current, state, err
	}
	for _, tag := range tags {
		state.Tags = append(state.Tags, tag.Name)
	}
	sort.Strings(state.Tags)
	links, err := q.GetLinksForSeries(ctx, id)
	if err != nil {
		return current, state, err
	}
	for _, link := range links {
		state.Links = append(state.Links, seriesmeta.Link{Name: link.Name, Url: link.Url})
	}
	sort.Slice(state.Links, func(i, j int) bool {
		return state.Links[i].Name+"\x00"+state.Links[i].Url < state.Links[j].Name+"\x00"+state.Links[j].Url
	})
	state.Cover, err = readCover(ctx, q, id)
	return current, state, err
}
func value(s State, field string) any {
	switch field {
	case "cover":
		return s.Cover
	case "title":
		return s.Title
	case "summary":
		return s.Summary
	case "publisher":
		return s.Publisher
	case "status":
		return s.Status
	case "rating":
		return s.Rating
	case "authors":
		return seriesmeta.AuthorSet(s.Authors)
	case "tags":
		v := slices.Clone(s.Tags)
		if v == nil {
			v = []string{}
		}
		sort.Strings(v)
		return v
	case "source_links":
		v := slices.Clone(s.Links)
		if v == nil {
			v = []seriesmeta.Link{}
		}
		sort.Slice(v, func(i, j int) bool { return v[i].Name+"\x00"+v[i].Url < v[j].Name+"\x00"+v[j].Url })
		return v
	}
	return nil
}
func same(a, b State, f string) bool {
	x, _ := json.Marshal(value(a, f))
	y, _ := json.Marshal(value(b, f))
	return string(x) == string(y)
}
func copyField(dst *State, src State, f string) {
	switch f {
	case "cover":
		dst.Cover = src.Cover
	case "title":
		dst.Title = src.Title
	case "summary":
		dst.Summary = src.Summary
	case "publisher":
		dst.Publisher = src.Publisher
	case "status":
		dst.Status = src.Status
	case "rating":
		dst.Rating = src.Rating
	case "authors":
		dst.Authors = slices.Clone(src.Authors)
	case "tags":
		dst.Tags = slices.Clone(src.Tags)
	case "source_links":
		dst.Links = slices.Clone(src.Links)
	}
}
func scalar(s *State, f string) **string {
	switch f {
	case "title":
		return &s.Title
	case "summary":
		return &s.Summary
	case "publisher":
		return &s.Publisher
	case "status":
		return &s.Status
	}
	return nil
}
func operation(actor delegated.Subject, key string, series, library int64, kind string) Operation {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return Operation{Version: 1, ID: hex.EncodeToString(b), RequestKey: key, Actor: actor, SeriesID: series, LibraryID: library, Kind: kind, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Outcome: "no-change", Fields: []FieldAudit{}}
}
func decode(r database.MetadataReceipt) (Operation, error) {
	var out Operation
	err := json.Unmarshal([]byte(r.Payload), &out)
	return out, err
}
func save(ctx context.Context, j database.MetadataJournal, out Operation, digest string) error {
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if len(b) > 200<<10 {
		return delegated.ErrInvalid
	}
	err = j.Insert(ctx, database.MetadataReceipt{ID: out.ID, UserID: out.Actor.UserID, CredentialID: out.Actor.CredentialID, RequestKey: out.RequestKey, RequestHash: digest, SeriesID: out.SeriesID, LibraryID: out.LibraryID, ParentID: out.ParentID, Outcome: out.Outcome, Payload: string(b)})
	if err != nil {
		return err
	}
	if out.Outcome == "changed" {
		for _, key := range []string{out.OldCoverMaterial, out.NewCoverMaterial} {
			if key != "" {
				if _, err = j.Tx.ExecContext(ctx, `INSERT INTO metadata_cover_materials(operation_id,path) VALUES(?,?)`, out.ID, key); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func retry(ctx context.Context, j database.MetadataJournal, actor delegated.Subject, key, digest string) (Operation, bool, error) {
	r, err := j.Retry(ctx, actor.UserID, actor.CredentialID, key)
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, false, nil
	}
	if err != nil {
		return Operation{}, false, err
	}
	if r.RequestHash != digest {
		return Operation{}, false, delegated.ErrInvalid
	}
	out, err := decode(r)
	if err != nil {
		return Operation{}, false, err
	}
	out.Applied = false
	out.Replayed = true
	out.UndoneBy, _ = j.UndoneBy(ctx, out.ID)
	return out, true, err
}

func (s *Service) existing(ctx context.Context, actor delegated.Subject, series int64, key, digest string) (Operation, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, false, err
	}
	defer tx.Rollback()
	current, err := database.New(tx).GetSeries(ctx, series)
	if err != nil {
		return Operation{}, false, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return Operation{}, false, err
	}
	return retry(ctx, database.MetadataJournal{Tx: tx}, actor, key, digest)
}

func changedFields(fields []FieldAudit) []string {
	out := []string{}
	for _, f := range fields {
		if f.Changed {
			out = append(out, f.Field)
		}
	}
	return out
}
