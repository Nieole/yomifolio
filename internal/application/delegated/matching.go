package delegated

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/proposal"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
)

// MatchingAccess uses the production factory with the configuration at call time.
// Searches retain bounded, temporary evidence; only confirmed proposals persist it.
type MatchingAccess struct {
	Store    database.Store
	Config   func() config.Config
	Source   func(config.Config, string) metadata.Provider
	Start    func(context.Context, task.RunSpec, task.Body) (task.Launched, error)
	mu       sync.Mutex
	searches map[string]*matchSearch
}
type SearchSourcesInput struct {
	SeriesID int64  `json:"series_id"`
	Query    string `json:"query"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}
type SourceCandidate struct {
	ID       string                  `json:"candidate_id"`
	Metadata metadata.SeriesMetadata `json:"metadata"`
}
type SourcePage struct {
	Source     string            `json:"source"`
	Status     string            `json:"status"`
	Total      int               `json:"total"`
	NextOffset *int              `json:"next_offset,omitempty"`
	Candidates []SourceCandidate `json:"candidates"`
	// Truncated means the offset bound prevents continuation, not source exhaustion.
	Truncated bool `json:"truncated,omitempty"`
}
type SearchEvidence struct {
	SearchID string       `json:"search_id"`
	SeriesID int64        `json:"series_id"`
	Query    string       `json:"query"`
	Status   string       `json:"status"`
	Sources  []SourcePage `json:"sources"`
}
type matchSearch struct {
	subject  Subject
	library  int64
	created  time.Time
	runID    int64
	evidence SearchEvidence
}
type StartedSearch struct {
	SearchID string `json:"search_id"`
	Run      Run    `json:"run"`
}

func (s *Service) WithMatching(access *MatchingAccess) *Service { s.matching = access; return s }

func (s *Service) SearchSources(ctx context.Context, subject Subject, in SearchSourcesInput) (StartedSearch, error) {
	if s.matching == nil || s.runs == nil {
		return StartedSearch{}, ErrDenied
	}
	if in.Limit == 0 {
		in.Limit = 5
	}
	in.Query = strings.TrimSpace(in.Query)
	if len(in.Query) == 0 || len(in.Query) > 256 || in.Limit < 1 || in.Limit > 10 || in.Offset < 0 || in.Offset > 1000 || in.Offset%in.Limit != 0 {
		return StartedSearch{}, ErrInvalid
	}
	series, err := s.GetSeries(ctx, subject, in.SeriesID)
	if err != nil {
		return StartedSearch{}, err
	}
	id, err := randomToken()
	if err != nil {
		return StartedSearch{}, err
	}
	a := s.matching
	a.mu.Lock()
	if a.searches == nil {
		a.searches = map[string]*matchSearch{}
	}
	for key, search := range a.searches {
		if time.Since(search.created) > 30*time.Minute {
			delete(a.searches, key)
		}
	}
	if len(a.searches) >= 64 {
		a.mu.Unlock()
		return StartedSearch{}, ErrInvalid
	}
	search := &matchSearch{subject: subject, library: series.LibraryID, created: time.Now(), evidence: SearchEvidence{SearchID: id, SeriesID: series.ID, Query: in.Query, Status: "running", Sources: []SourcePage{}}}
	a.searches[id] = search
	a.mu.Unlock()
	spec := task.RunSpec{Identity: task.Identity{Type: "source_search", Scope: task.ScopeLibrary, ScopeID: series.LibraryID, Variant: task.Variant(id)}, LibraryIDs: []int64{series.LibraryID}, Trigger: task.TriggerManual, Key: "source_search", CanCancel: true}
	spec, body, err := s.PrepareRun(ctx, subject, series.LibraryID, Read, spec, func(ctx context.Context, h *runhandle.Handle) (task.Result, error) {
		fanout := metadata.SourceFanout{Config: a.Config(), Open: a.Source}
		sources := len(metadata.CatalogueSources(fanout.Config))
		visited, successes := 0, 0
		finish := func(status string) { a.mu.Lock(); search.evidence.Status = status; a.mu.Unlock() }
		check := func(ctx context.Context) error { return s.Checkpoint(ctx, subject, series.LibraryID, Read) }
		err := fanout.Search(ctx, metadata.SourceQuery{Text: in.Query, Limit: in.Limit, Offset: in.Offset}, check, func(found metadata.SourcePage) {
			a.mu.Lock()
			search.evidence.Sources = append(search.evidence.Sources, sourcePage(found, in))
			if bounded(search.evidence) != nil {
				found = found.TooLarge()
				search.evidence.Sources[len(search.evidence.Sources)-1] = sourcePage(found, in)
			}
			a.mu.Unlock()
			if found.Status == metadata.SourceAvailable {
				successes++
			}
			visited++
			h.Advance(visited, sources, "", nil)
		})
		if err != nil {
			finish("cancelled")
			return task.Result{}, err
		}
		if successes == 0 {
			finish("failed")
			return task.Result{}, fmt.Errorf("no source available")
		}
		if successes < sources {
			finish("partial")
		} else {
			finish("completed")
		}
		return task.Result{}, nil
	})
	if err != nil {
		a.mu.Lock()
		delete(a.searches, id)
		a.mu.Unlock()
		return StartedSearch{}, err
	}
	launched, err := a.Start(ctx, spec, body)
	if err != nil {
		a.mu.Lock()
		delete(a.searches, id)
		a.mu.Unlock()
		return StartedSearch{}, err
	}
	a.mu.Lock()
	search.runID = launched.Run.ID
	a.mu.Unlock()
	return StartedSearch{SearchID: id, Run: Run{ID: launched.Run.ID, LibraryID: series.LibraryID, Status: launched.Run.Status, Coalesced: launched.Coalesced}}, nil
}

// sourcePage numbers candidates within the page and bounds continuation by the offset limit.
func sourcePage(found metadata.SourcePage, in SearchSourcesInput) SourcePage {
	page := SourcePage{Source: found.Source, Status: string(found.Status), Total: found.Total, Candidates: []SourceCandidate{}}
	for j, result := range found.Results {
		page.Candidates = append(page.Candidates, SourceCandidate{ID: fmt.Sprintf("%s:%d", found.Source, j), Metadata: result})
	}
	if found.Status == metadata.SourceAvailable && in.Offset+len(found.Results) < found.Total && len(found.Results) > 0 {
		next := in.Offset + in.Limit
		if next <= 1000 {
			page.NextOffset = &next
		} else {
			page.Truncated = true
		}
	}
	return page
}
func (s *Service) GetSourceSearch(ctx context.Context, subject Subject, id string) (SearchEvidence, error) {
	if s.matching == nil {
		return SearchEvidence{}, ErrDenied
	}
	a := s.matching
	a.mu.Lock()
	defer a.mu.Unlock()
	search, ok := a.searches[id]
	if !ok || search.subject != subject || time.Since(search.created) > 30*time.Minute {
		return SearchEvidence{}, ErrDenied
	}
	series, err := s.GetSeries(ctx, subject, search.evidence.SeriesID)
	if err != nil || series.LibraryID != search.library {
		return SearchEvidence{}, ErrDenied
	}
	if search.runID > 0 {
		snap, err := s.runs.Engine.SnapshotOf(ctx, search.runID)
		if err != nil {
			return SearchEvidence{}, err
		}
		if snap.Run.Status == task.StatusCancelled || snap.Run.Status == task.StatusCancelling {
			search.evidence.Status = "cancelled"
		}
		if snap.Run.Status == task.StatusInterrupted {
			search.evidence.Status = "interrupted"
		}
	}
	raw, _ := json.Marshal(search.evidence)
	var out SearchEvidence
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

type ConfirmMatchInput struct {
	SearchID          string            `json:"search_id"`
	IdentityConfirmed bool              `json:"identity_confirmed"`
	EditionConfirmed  bool              `json:"edition_confirmed"`
	Summary           string            `json:"summary"`
	Fields            map[string]string `json:"fields"`
}
type MatchProposal struct {
	Accepted        bool                    `json:"accepted"`
	Status          proposal.QueueStatus    `json:"status"`
	ProposalID      int64                   `json:"proposal_id,omitempty"`
	MetadataWritten bool                    `json:"metadata_written"`
	Evidence        *metadata.MatchEvidence `json:"evidence,omitempty"`
	Fields          []proposal.Field        `json:"fields,omitempty"`
}

func (s *Service) ProposeMatch(ctx context.Context, subject Subject, in ConfirmMatchInput) (MatchProposal, error) {
	if !in.IdentityConfirmed || !in.EditionConfirmed || len(strings.TrimSpace(in.Summary)) == 0 || len(in.Summary) > 1000 || len(in.Fields) == 0 || len(in.Fields) > 7 {
		return MatchProposal{}, ErrInvalid
	}
	evidence, err := s.GetSourceSearch(ctx, subject, in.SearchID)
	if err != nil {
		return MatchProposal{}, err
	}
	if evidence.Status != "completed" && evidence.Status != "partial" {
		return MatchProposal{}, ErrInvalid
	}
	series, err := s.GetSeries(ctx, subject, evidence.SeriesID)
	if err != nil {
		return MatchProposal{}, err
	}
	if err = s.Authorize(ctx, subject, series.LibraryID, MetadataWrite); err != nil {
		return MatchProposal{}, err
	}
	result := &metadata.SeriesMetadata{Provider: "delegated", MatchEvidence: &metadata.MatchEvidence{Summary: strings.TrimSpace(in.Summary), Query: evidence.Query, UserID: subject.UserID, CredentialID: subject.CredentialID, IdentityConfirmed: true, EditionConfirmed: true, Fields: map[string]metadata.FieldEvidence{}}}
	for field, id := range in.Fields {
		var selected *SourceCandidate
		for _, page := range evidence.Sources {
			for _, candidate := range page.Candidates {
				if candidate.ID == id {
					copy := candidate
					selected = &copy
				}
			}
		}
		if selected == nil {
			return MatchProposal{}, ErrInvalid
		}
		m := selected.Metadata
		switch field {
		case "title":
			result.Title = m.Title
		case "summary":
			result.Summary = m.Summary
		case "publisher":
			result.Publisher = m.Publisher
		case "status":
			result.Status = m.Status
		case "rating":
			result.Rating = m.Rating
		case "tags":
			result.Tags = m.Tags
		case "authors":
			result.Authors = m.Authors
		default:
			return MatchProposal{}, ErrInvalid
		}
		raw, _ := json.Marshal(m)
		var facts metadata.SourceFacts
		_ = json.Unmarshal(raw, &facts)
		result.MatchEvidence.Fields[field] = metadata.FieldEvidence{CandidateID: id, Source: m.Provider, Metadata: facts}
	}
	out, accepted, err := s.matching.queue(ctx, subject, series.LibraryID, series.ID, result)
	if err != nil {
		return MatchProposal{}, err
	}
	response := MatchProposal{Accepted: accepted, Status: out.Status, ProposalID: out.Proposal.ID}
	if out.Proposal.ID > 0 {
		var saved metadata.SeriesMetadata
		_ = json.Unmarshal([]byte(out.Proposal.RawPayload), &saved)
		response.Evidence = saved.MatchEvidence
	}
	return response, bounded(response)
}
func (s *Service) GetMatchProposal(ctx context.Context, subject Subject, seriesID, id int64) (MatchProposal, error) {
	if s.matching == nil {
		return MatchProposal{}, ErrDenied
	}
	series, err := s.GetSeries(ctx, subject, seriesID)
	if err != nil {
		return MatchProposal{}, err
	}
	review, err := s.matching.Store.GetMetadataReview(ctx, id)
	if err != nil || review.SeriesID != series.ID {
		return MatchProposal{}, ErrDenied
	}
	tx, err := s.matching.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return MatchProposal{}, err
	}
	defer tx.Rollback()
	q := database.New(tx)
	record, err := proposal.Read(ctx, q, id)
	if err != nil || record.Row.SeriesID != series.ID {
		return MatchProposal{}, ErrDenied
	}
	current, err := q.GetSeries(ctx, series.ID)
	if err != nil {
		return MatchProposal{}, ErrDenied
	}
	if err = AuthorizeTx(ctx, tx, subject, current.LibraryID, Read); err != nil {
		return MatchProposal{}, err
	}
	if err = AuthorizeTx(ctx, tx, subject, current.LibraryID, MetadataWrite); err != nil {
		return MatchProposal{}, err
	}
	var raw metadata.SeriesMetadata
	if strings.TrimSpace(record.Row.RawPayload) != "" {
		if err = json.Unmarshal([]byte(record.Row.RawPayload), &raw); err != nil {
			return MatchProposal{}, ErrInvalid
		}
	}
	accepted, err := proposal.Accepted(ctx, tx, record.Row)
	if err != nil {
		return MatchProposal{}, err
	}
	out := MatchProposal{Status: proposal.QueueStatus(record.Row.Status), Accepted: accepted, ProposalID: id, Evidence: raw.MatchEvidence, Fields: record.Fields}
	return out, bounded(out)
}
