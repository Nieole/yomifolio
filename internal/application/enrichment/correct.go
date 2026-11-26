package enrichment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
)

// CorrectableFields are the fields an agent may correct, in the order agents are offered them.
var CorrectableFields = []string{"title", "summary", "publisher", "status", "rating"}

// Why a correction was refused.
var (
	// ErrFieldLocked: the field is locked.
	ErrFieldLocked = errors.New("field is locked")
	// ErrNotSourced: the current value has no source record holding it, because a person edited it,
	// ComicInfo brought it or no source ever supplied it.
	ErrNotSourced = errors.New("current value is not a source value")
	// ErrUnchanged: the corrected value is the current value.
	ErrUnchanged = errors.New("value is unchanged")
)

// CorrectionInput replaces one source value of a series.
type CorrectionInput struct {
	// RequestKey names the receipt and must be new for the actor.
	RequestKey string
	SeriesID   int64
	// Field is one of CorrectableFields.
	Field string
	// Value is the corrected value: text, a status code, or a rating of 0-10 as decimal text. ""
	// clears the field, except the title.
	Value  string
	Reason string
}

// CorrectionBasis records on a receipt the field an agent corrected and the source record of the
// value it replaced, which undoing the receipt restores.
type CorrectionBasis struct {
	Field    string            `json:"field"`
	Replaced FieldSourceRecord `json:"replaced"`
}

// FieldSourceRecord is a field's source record: the value a source supplied, which source, and the
// proposal or operation that wrote it.
type FieldSourceRecord struct {
	Value       string  `json:"value"`
	Source      string  `json:"source"`
	SourceURL   string  `json:"source_url"`
	Confidence  float64 `json:"confidence"`
	ProposalID  int64   `json:"proposal_id,omitempty"`
	OperationID string  `json:"operation_id,omitempty"`
}

// CorrectedSource is the source a corrected value's record names.
const CorrectedSource = "agent"

const (
	maxTitleBytes   = 500
	maxSummaryBytes = 10000
)

// CorrectField replaces one source value of a series inside the caller's transaction. Only a value
// that still equals its source record may be corrected, and only in an unlocked field: a person's
// edit and a ComicInfo value carry no such record. The corrected value gets its own source record,
// so a scan keeps it like the source value it replaced, and the receipt undoes with Undo, which
// restores the replaced value and its record. It rechecks current metadata_write authority.
//
// ErrInvalid reports a value the field does not take; ErrFieldLocked, ErrNotSourced and
// ErrUnchanged report a refusal. Any error means the caller must roll back; after its commit the
// caller calls Publish.
func (s *Service) CorrectField(ctx context.Context, tx *sql.Tx, actor delegated.Subject, in CorrectionInput) (Operation, error) {
	if !validKey(in.RequestKey) || in.SeriesID <= 0 || !slices.Contains(CorrectableFields, in.Field) {
		return Operation{}, delegated.ErrInvalid
	}
	q := database.New(tx)
	current, before, err := readState(ctx, q, in.SeriesID)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return Operation{}, err
	}
	after, stored, ok := corrected(before, in.Field, in.Value)
	if !ok {
		return Operation{}, delegated.ErrInvalid
	}
	if seriesmeta.EnrichmentLocked(current, in.Field) {
		return Operation{}, ErrFieldLocked
	}
	records, err := q.GetSeriesMetadataProvenance(ctx, current.ID)
	if err != nil {
		return Operation{}, err
	}
	index := slices.IndexFunc(records, func(r database.SeriesMetadataProvenance) bool { return r.FieldName == in.Field })
	if index < 0 || !seriesmeta.SourceBacked(current, records)[in.Field] {
		return Operation{}, ErrNotSourced
	}
	if same(before, after, in.Field) {
		return Operation{}, ErrUnchanged
	}
	out := operation(actor, in.RequestKey, current.ID, current.LibraryID, "enrich")
	out.Before, out.After, out.Reason, out.Outcome, out.Applied = before, after, in.Reason, "changed", true
	out.Fields = []FieldAudit{{Field: in.Field, Changed: true, Reason: "agent correction: " + in.Reason, Sources: []metadata.FieldEvidence{}}}
	replaced := records[index]
	out.Correction = &CorrectionBasis{Field: in.Field, Replaced: FieldSourceRecord{Value: replaced.Value, Source: replaced.Source, SourceURL: replaced.SourceUrl,
		Confidence: replaced.Confidence, ProposalID: replaced.ReviewID.Int64, OperationID: replaced.OperationID.String}}
	if err = writeState(ctx, tx, q, current, after, []string{in.Field}); err != nil {
		return Operation{}, err
	}
	origin := seriesmeta.FieldOrigin{Operation: out.ID}
	if err = seriesmeta.RecordEnrichmentProvenance(ctx, q, current.ID, origin, in.Field, stored, metadata.FieldEvidence{Source: CorrectedSource}); err != nil {
		return Operation{}, err
	}
	if err = save(ctx, database.MetadataJournal{Tx: tx}, out, correctionDigest(in)); err != nil {
		return Operation{}, err
	}
	return out, nil
}

// corrected returns the state with the field set to value and the text its source record stores.
func corrected(before State, field, value string) (State, string, bool) {
	after := before
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		return after, "", false
	}
	switch field {
	case "rating":
		if value == "" {
			after.Rating = nil
			return after, "", true
		}
		rating, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(rating) || rating < 0 || rating > 10 {
			return after, "", false
		}
		after.Rating = &rating
		stored, _ := json.Marshal(rating)
		return after, string(stored), true
	case "status":
		if value != "" {
			code, ok := metadata.NormalizeStatusCodeOptional(value)
			if !ok || code == "unknown" {
				return after, "", false
			}
			value = code
		}
	case "title":
		if value == "" || len(value) > maxTitleBytes {
			return after, "", false
		}
	case "summary":
		if len(value) > maxSummaryBytes {
			return after, "", false
		}
	case "publisher":
		if len(value) > maxTitleBytes {
			return after, "", false
		}
	}
	target := scalar(&after, field)
	*target = nil
	if value != "" {
		*target = &value
	}
	return after, value, true
}

func correctionDigest(in CorrectionInput) string {
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
