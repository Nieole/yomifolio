// Package enrichment owns atomic, authorized metadata supplementation and durable undo.
// Receipts are immutable operations, not a projection of latest field provenance.
package enrichment

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
)

// Evidence supplies persisted, explicitly confirmed matching decisions for the target.
type Evidence interface {
	GetMatchProposal(context.Context, delegated.Subject, int64, int64) (delegated.MatchProposal, error)
}

// State is the source-specific metadata snapshot used for expected-value checks.
type State = seriesmeta.State

// ExpectedState is the command snapshot, not the extensible response State.
// Authors is optional so pre-author MCP requests still validate against the schema.
// New command fields must also be represented explicitly in applyDigest.
type ExpectedState struct {
	Title     *string                `json:"title"`
	Summary   *string                `json:"summary"`
	Publisher *string                `json:"publisher"`
	Status    *string                `json:"status"`
	Rating    *float64               `json:"rating"`
	Tags      []string               `json:"tags"`
	Links     []seriesmeta.Link      `json:"links"`
	Authors   []seriesmeta.Author    `json:"authors,omitempty"`
	Cover     *seriesmeta.CoverState `json:"cover,omitempty"`
}

func (e ExpectedState) state() State {
	return State{Title: e.Title, Summary: e.Summary, Publisher: e.Publisher, Status: e.Status, Rating: e.Rating, Tags: e.Tags, Links: e.Links, Authors: e.Authors, Cover: e.Cover}
}

type Input struct {
	RequestKey        string             `json:"request_key"`
	SeriesID          int64              `json:"series_id"`
	ProposalIDs       []int64            `json:"proposal_ids"`
	Fields            []string           `json:"fields"`
	Expected          ExpectedState      `json:"expected"`
	Cover             *CoverSelection    `json:"cover,omitempty"`
	AuthorCorrections []AuthorCorrection `json:"author_corrections,omitempty"`
}
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
	CoverSelection   *CoverSelection   `json:"cover_selection,omitempty"`
	OldCoverMaterial string            `json:"old_cover_material,omitempty"`
	NewCoverMaterial string            `json:"new_cover_material,omitempty"`
	Version          int               `json:"version"`
	ID               string            `json:"id"`
	RequestKey       string            `json:"request_key"`
	Actor            delegated.Subject `json:"actor"`
	SeriesID         int64             `json:"series_id"`
	LibraryID        int64             `json:"library_id"`
	Kind             string            `json:"kind"`
	ParentID         string            `json:"parent_id,omitempty"`
	CreatedAt        string            `json:"created_at"`
	Outcome          string            `json:"outcome"`
	Reason           string            `json:"reason"`
	Before           State             `json:"before"`
	After            State             `json:"after"`
	Fields           []FieldAudit      `json:"fields"`
	UndoneBy         string            `json:"undone_by,omitempty"`
	Applied          bool              `json:"applied"`
	Replayed         bool              `json:"replayed"`
}
type Page struct {
	Operations []Operation `json:"operations"`
	NextBefore int64       `json:"next_before,omitempty"`
}
type BatchResult struct {
	Operations []Operation `json:"operations"`
}

// Service is synchronous; one series and its receipt commit together. No batch transaction,
// external calls, implicit retries, automatic expiry or process-local reversal state exists.
type Service struct {
	db       *sql.DB
	evidence Evidence
	publish  func()
	covers   *CoverAccess
}

func New(db *sql.DB, evidence Evidence, publish func()) *Service {
	return &Service{db: db, evidence: evidence, publish: publish}
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
	out.Applied = false
	out.Replayed = true
	out.UndoneBy, _ = j.UndoneBy(ctx, out.ID)
	return out, true, err
}

// State reads per-field expected values, not a global content version.
func (s *Service) State(ctx context.Context, actor delegated.Subject, series int64) (State, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback()
	current, state, err := readState(ctx, database.New(tx), series)
	if err != nil {
		return State{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, false); err != nil {
		return State{}, err
	}
	return state, nil
}

// Apply requires selected fields and explicitly confirmed evidence. Identical retries
// return the original immutable outcome with replayed=true and applied=false.
func (s *Service) Apply(ctx context.Context, actor delegated.Subject, in Input) (Operation, error) {
	if !validKey(in.RequestKey) || in.SeriesID <= 0 || len(in.Fields) < 1 || len(in.Fields) > 9 || len(in.ProposalIDs) > 10 {
		return Operation{}, delegated.ErrInvalid
	}
	seen := map[string]bool{}
	for _, f := range in.Fields {
		if seen[f] || !slices.Contains([]string{"title", "summary", "publisher", "status", "rating", "tags", "source_links", "cover", "authors"}, f) {
			return Operation{}, delegated.ErrInvalid
		}
		seen[f] = true
	}
	if (len(in.Fields) > 1 || !seen["cover"]) && len(in.ProposalIDs) == 0 {
		return Operation{}, delegated.ErrInvalid
	}
	if seen["cover"] != (in.Cover != nil) || len(in.AuthorCorrections) > 50 || (len(in.AuthorCorrections) > 0 && !seen["authors"]) {
		return Operation{}, delegated.ErrInvalid
	}
	if out, found, err := s.existing(ctx, actor, in.SeriesID, in.RequestKey, applyDigest(in)); err != nil || found {
		return out, err
	}
	evidence := map[string][]metadata.FieldEvidence{}
	reason := ""
	for _, id := range in.ProposalIDs {
		proposal, err := s.evidence.GetMatchProposal(ctx, actor, in.SeriesID, id)
		if err != nil {
			return Operation{}, err
		}
		e := proposal.Evidence
		if e == nil || !e.IdentityConfirmed || !e.EditionConfirmed || strings.TrimSpace(e.Summary) == "" {
			return Operation{}, delegated.ErrInvalid
		}
		if reason == "" {
			reason = e.Summary
		}
		keys := make([]string, 0, len(e.Fields))
		for f := range e.Fields {
			keys = append(keys, f)
		}
		sort.Strings(keys)
		for _, f := range keys {
			evidence[f] = append(evidence[f], e.Fields[f])
			source := e.Fields[f]
			if !slices.ContainsFunc(evidence["source_links"], func(existing metadata.FieldEvidence) bool {
				return existing.Source == source.Source && existing.Metadata.SourceURL == source.Metadata.SourceURL
			}) {
				evidence["source_links"] = append(evidence["source_links"], source)
			}
		}
	}
	var prepared *preparedCover
	if in.Cover != nil {
		var err error
		prepared, err = s.prepareCover(ctx, actor, in)
		if err != nil {
			return Operation{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	q := database.New(tx)
	j := database.MetadataJournal{Tx: tx}
	current, before, err := readState(ctx, q, in.SeriesID)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return Operation{}, err
	}
	if out, found, e := retry(ctx, j, actor, in.RequestKey, applyDigest(in)); e != nil || found {
		return out, e
	}
	out := operation(actor, in.RequestKey, current.ID, current.LibraryID, "enrich")
	out.Before = before
	out.After = before
	if prepared != nil {
		out.CoverSelection = in.Cover
		out.OldCoverMaterial = prepared.old
		out.NewCoverMaterial = prepared.new
	}
	for _, f := range in.Fields {
		if seriesmeta.EnrichmentLocked(current, f) || !same(before, in.Expected.state(), f) || (f == "cover" && !eligibleCover(before.Cover)) {
			out.Outcome = "conflict"
			out.Reason = "current field or lock differs"
		}
		if f == "cover" {
			if before.Cover != nil {
				out.After.Cover = &seriesmeta.CoverState{BookID: before.Cover.BookID, Path: prepared.new, Automatic: true}
			}
			out.Fields = append(out.Fields, FieldAudit{Field: f, Changed: true, Reason: in.Cover.Reason, Sources: []metadata.FieldEvidence{}})
			continue
		}
		sources := evidence[f]
		if len(sources) == 0 {
			return Operation{}, delegated.ErrInvalid
		}
		audit := FieldAudit{Field: f, Reason: reason, Sources: sources}
		switch f {
		case "authors":
			var e error
			out.After.Authors, e = correctAuthors(in.Expected.Authors, sources, in.AuthorCorrections)
			if e != nil {
				return Operation{}, e
			}
			for _, correction := range in.AuthorCorrections {
				audit.Reason += "; " + correction.Reason
			}
		case "rating":
			r, valid, formula := seriesmeta.EnrichmentRating(sources)
			audit.Sources = valid
			audit.Formula = formula
			if before.Rating == nil {
				out.After.Rating = r
			}
		case "tags":
			out.After.Tags = slices.Clone(before.Tags)
			for _, source := range sources {
				for _, tag := range seriesmeta.ContentTags(source.Metadata.Tags) {
					if !slices.Contains(out.After.Tags, tag) {
						out.After.Tags = append(out.After.Tags, tag)
					}
				}
			}
			sort.Strings(out.After.Tags)
		case "source_links":
			out.After.Links = slices.Clone(before.Links)
			for _, source := range sources {
				u, e := url.Parse(source.Metadata.SourceURL)
				if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
					continue
				}
				exists := false
				for _, l := range out.After.Links {
					if l.Name == source.Source || l.Url == u.String() {
						exists = true
					}
				}
				if !exists {
					out.After.Links = append(out.After.Links, seriesmeta.Link{Name: source.Source, Url: u.String()})
				}
			}
		default:
			target := scalar(&out.After, f)
			if *target == nil || strings.TrimSpace(**target) == "" {
				facts := sources[0].Metadata
				v := ""
				switch f {
				case "title":
					v = facts.Title
				case "summary":
					v = facts.Summary
				case "publisher":
					v = facts.Publisher
				case "status":
					v = facts.Status
				}
				if strings.TrimSpace(v) != "" {
					*target = &v
				}
			}
		}
		audit.Changed = !same(before, out.After, f)
		if !audit.Changed {
			audit.Reason = "preserved existing value or no eligible source change: " + reason
		}
		out.Fields = append(out.Fields, audit)
	}
	if out.Outcome == "conflict" {
		out.After = before
		for i := range out.Fields {
			out.Fields[i].Changed = false
		}
	} else {
		for _, f := range out.Fields {
			if f.Changed {
				out.Outcome = "changed"
				out.Applied = true
			}
		}
	}
	if in.Cover != nil {
		source, e := q.GetBook(ctx, in.Cover.BookID)
		if e != nil || source.SeriesID != current.ID || source.LibraryID != current.LibraryID {
			return Operation{}, delegated.ErrDenied
		}
	}
	if out.Applied {
		failed, e := j.TryChange(ctx, func() error { return writeState(ctx, tx, q, current, out.After, changedFields(out.Fields)) })
		if e != nil {
			return Operation{}, e
		}
		if failed {
			out.Applied = false
			out.Outcome = "failed"
			out.Reason = "metadata write failed; nothing changed"
			out.After = out.Before
			for i := range out.Fields {
				out.Fields[i].Changed = false
			}
		}
	}
	if err = save(ctx, j, out, applyDigest(in)); err != nil {
		return Operation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Operation{}, err
	}
	if out.Applied && s.publish != nil {
		s.publish()
	}
	return out, nil
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

// Batch reports independent series results, never whole-batch atomicity.
func (s *Service) Batch(ctx context.Context, actor delegated.Subject, inputs []Input) (BatchResult, error) {
	if len(inputs) < 1 || len(inputs) > 20 {
		return BatchResult{}, delegated.ErrInvalid
	}
	out := BatchResult{Operations: []Operation{}}
	size := 0
	for _, in := range inputs {
		reportKey := in.RequestKey
		if !validKey(reportKey) {
			reportKey = ""
		}
		// Reserve one maximum 200 KiB receipt plus framing and all remaining
		// unprocessed entries before starting another independently committed unit.
		if ctx.Err() != nil || size > 24<<10 {
			out.Operations = append(out.Operations, Operation{RequestKey: reportKey, SeriesID: in.SeriesID, Outcome: "unprocessed", Reason: "request cancelled or response budget exhausted"})
			continue
		}
		op, err := s.Apply(ctx, actor, in)
		if err != nil {
			op = Operation{RequestKey: reportKey, SeriesID: in.SeriesID, Outcome: "failed", Reason: "operation rejected or unavailable; no metadata committed"}
		}
		out.Operations = append(out.Operations, op)
		encoded, _ := json.Marshal(op)
		size += len(encoded)
	}
	return out, nil
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
