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
		cfg := a.Config()
		names := []string{"bangumi", "anilist", "mangadex"}
		if strings.TrimSpace(cfg.Scrapers.MALClientID) != "" {
			names = append(names, "myanimelist")
		}
		if strings.TrimSpace(cfg.Scrapers.ComicVineAPIKey) != "" {
			names = append(names, "comicvine")
		}
		successes := 0
		finish := func(status string) { a.mu.Lock(); search.evidence.Status = status; a.mu.Unlock() }
		for i, name := range names {
			if err := s.Checkpoint(ctx, subject, series.LibraryID, Read); err != nil {
				finish("cancelled")
				return task.Result{}, err
			}
			p := a.Source(cfg, name)
			page := SourcePage{Source: name, Status: "failed", Candidates: []SourceCandidate{}}
			var results []*metadata.SeriesMetadata
			var total int
			var fetchErr error
			if p != nil {
				results, total, fetchErr = p.SearchMetadata(ctx, in.Query, in.Limit, in.Offset)
			} else {
				fetchErr = ErrInvalid
			}
			if err := s.Checkpoint(ctx, subject, series.LibraryID, Read); err != nil {
				finish("cancelled")
				return task.Result{}, err
			}
			if fetchErr == nil && len(results) <= in.Limit && total >= 0 {
				page.Status = "available"
				page.Total = total
				for j, result := range results {
					if result == nil || result.MatchEvidence != nil || result.SourceID < 0 || result.VolumeCount < 0 || result.Rating < 0 || result.Rating > 10 {
						page.Status = "invalid_response"
						break
					}
					raw, e := json.Marshal(result)
					if e != nil || len(raw) > 32<<10 || len(result.Tags) > 100 || len(result.Authors) > 100 {
						page.Status = "invalid_response"
						break
					}
					// Snapshot the source result; neither candidate IDs nor URLs are fetch inputs.
					var snapshot metadata.SeriesMetadata
					_ = json.Unmarshal(raw, &snapshot)
					snapshot.Provider = name
					page.Candidates = append(page.Candidates, SourceCandidate{ID: fmt.Sprintf("%s:%d", name, j), Metadata: snapshot})
				}
				if page.Status == "available" {
					successes++
					if in.Offset+len(results) < total && len(results) > 0 {
						next := in.Offset + in.Limit
						page.NextOffset = &next
					}
				} else {
					page.Candidates = []SourceCandidate{}
				}
			}
			a.mu.Lock()
			search.evidence.Sources = append(search.evidence.Sources, page)
			if bounded(search.evidence) != nil {
				search.evidence.Sources[len(search.evidence.Sources)-1] = SourcePage{Source: name, Status: "too_large", Candidates: []SourceCandidate{}}
				if page.Status == "available" {
					successes--
				}
			}
			a.mu.Unlock()
			h.Advance(i+1, len(names), "", nil)
		}
		if successes == 0 {
			finish("failed")
			return task.Result{}, fmt.Errorf("no source available")
		}
		if successes < len(names) {
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
	out, err := s.matching.queue(ctx, subject, series.LibraryID, series.ID, result)
	if err != nil {
		return MatchProposal{}, err
	}
	response := MatchProposal{Status: out.Status, ProposalID: out.Proposal.ID}
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
	if err = s.Authorize(ctx, subject, series.LibraryID, Read); err != nil {
		return MatchProposal{}, err
	}
	var raw metadata.SeriesMetadata
	if err = json.Unmarshal([]byte(review.RawPayload), &raw); err != nil {
		return MatchProposal{}, ErrInvalid
	}
	record, err := proposal.NewService(confirmedProposalStore{Store: s.matching.Store}).Get(ctx, id)
	if err != nil {
		return MatchProposal{}, err
	}
	out := MatchProposal{Status: proposal.QueueStatus(review.Status), ProposalID: id, Evidence: raw.MatchEvidence, Fields: record.Fields}
	return out, bounded(out)
}
