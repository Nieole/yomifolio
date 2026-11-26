package identification

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/config"
	"yomifolio/internal/metadata"
)

// Enricher supplements an identified series inside the transaction that confirmed it, takes back
// what a withdrawn entry supplemented inside the transaction that withdrew it, and corrects one
// source value inside the transaction that records the correction.
type Enricher interface {
	EnrichIdentified(context.Context, *sql.Tx, delegated.Subject, enrichment.IdentifiedInput) (enrichment.Operation, error)
	RetractIdentified(context.Context, *sql.Tx, delegated.Subject, enrichment.RetractInput) (enrichment.Operation, error)
	CorrectField(context.Context, *sql.Tx, delegated.Subject, enrichment.CorrectionInput) (enrichment.Operation, error)
	Publish()
}

// WithEnrichment has every confirmed identification supplement its series through enricher, taking
// source values in the priority the current configuration names. It must be set before use.
func (s *Service) WithEnrichment(enricher Enricher, cfg func() config.Config) *Service {
	s.enricher, s.config = enricher, cfg
	return s
}

// citedCandidate is a candidate a confirmer identifies the series with, and the source snapshot
// its evidence kept.
type citedCandidate struct {
	EvidenceCandidate
	facts metadata.SeriesMetadata
}

// confirm records the cited candidates as the series' conclusions by one confirmer, then supplements
// the series from every source it is identified with, all in the caller's transaction: an
// identification never commits without its supplementation. It is the one identification use case
// of agents and the owner; the caller has already decided the series may be identified with these
// candidates. An owner-reviewed series is not supplemented. receiptKey names the enrichment receipt
// and must be new for actor. The returned receipt is zero when nothing was supplemented.
func (s *Service) confirm(ctx context.Context, tx *sql.Tx, actor delegated.Subject, confirmer, receiptKey string, seriesID int64, cited []citedCandidate, reason string) (enrichment.Operation, error) {
	now := s.now().UnixMilli()
	for _, c := range cited {
		facts := c.facts
		facts.MatchEvidence = nil
		raw, err := json.Marshal(facts)
		if err != nil {
			return enrichment.Operation{}, err
		}
		_, entry, _ := strings.Cut(c.ID, ":")
		if _, err := tx.ExecContext(ctx, `INSERT INTO identification_conclusions (series_id, source, entry_id, entry_url, title, original_title, aliases,
				confirmer, user_id, credential_id, confirmed_at_ms, reason, facts) VALUES (?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, ?, ?, ?)`,
			seriesID, c.Source, entry, c.EntryURL, cmp.Or(c.facts.Title, c.Title), cmp.Or(c.facts.OriginalTitle, c.OriginalTitle),
			confirmer, actor.UserID, actor.CredentialID, now, reason, string(raw)); err != nil {
			return enrichment.Operation{}, err
		}
	}
	if s.enricher == nil {
		return enrichment.Operation{}, nil
	}
	status, err := seriesStatus(ctx, tx, seriesID)
	if err != nil || status == StatusOwnerReviewed {
		return enrichment.Operation{}, err
	}
	sources, err := s.identifiedSources(ctx, tx, seriesID)
	if err != nil {
		return enrichment.Operation{}, err
	}
	tags := config.DefaultTagPolicy
	if s.config != nil {
		tags = s.config().Identification.Tags
	}
	return s.enricher.EnrichIdentified(ctx, tx, actor, enrichment.IdentifiedInput{RequestKey: receiptKey, SeriesID: seriesID, Reason: reason, Sources: sources, Tags: tags})
}

// identifiedSources lists every source the series is identified with, in the configured priority.
func (s *Service) identifiedSources(ctx context.Context, q queryer, seriesID int64) ([]enrichment.IdentifiedSource, error) {
	rows, err := q.QueryContext(ctx, `SELECT source, entry_id, entry_url, facts FROM identification_conclusions WHERE series_id = ?`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []enrichment.IdentifiedSource
	for rows.Next() {
		var source, entry, link, raw string
		if err := rows.Scan(&source, &entry, &link, &raw); err != nil {
			return nil, err
		}
		identified := enrichment.IdentifiedSource{CandidateID: candidateID(source, entry), Source: source, EntryURL: link}
		var facts metadata.SourceFacts
		if raw != "" && raw != "{}" && json.Unmarshal([]byte(raw), &facts) == nil {
			identified.Facts = &facts
		}
		out = append(out, identified)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	order := config.DefaultSourcePriority.Order()
	if s.config != nil {
		order = s.config().Identification.SourcePriority.Order()
	}
	rank := func(source string) int {
		if i := slices.Index(order, source); i >= 0 {
			return i
		}
		return len(order)
	}
	slices.SortStableFunc(out, func(a, b enrichment.IdentifiedSource) int { return cmp.Compare(rank(a.Source), rank(b.Source)) })
	return out, nil
}

// verdictReceiptKey names the enrichment receipt of one identify verdict: one request key answers up
// to a whole batch, while receipt keys are unique per credential.
func verdictReceiptKey(subject delegated.Subject, key string, seriesID int64) string {
	sum := sha256.Sum256([]byte(subject.CredentialID + "\x00" + key + "\x00" + strconv.FormatInt(seriesID, 10)))
	return "identify-" + hex.EncodeToString(sum[:16])
}
