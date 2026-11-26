package delegated

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"time"

	"yomifolio/internal/application/scanning"
	"yomifolio/internal/database"
	"yomifolio/internal/task"
)

// RunAccess shares the process engine and existing scanning use case.
type RunAccess struct {
	Engine    *task.Engine
	Scans     *scanning.Service
	Libraries interface {
		GetLibrary(context.Context, int64) (database.Library, error)
	}
}

func NewWithRuns(store Store, access RunAccess) *Service {
	return &Service{store: store, runs: &access}
}

// Run is a bounded projection; paths, arguments and technical errors remain private.
type Run struct {
	ID        int64          `json:"run_id"`
	LibraryID int64          `json:"library_id"`
	Status    task.RunStatus `json:"status"`
	Current   int            `json:"current"`
	Total     int            `json:"total"`
	Coalesced bool           `json:"coalesced"`
}

func (s *Service) StartScan(ctx context.Context, subject Subject, library int64) (Run, error) {
	if s.runs == nil {
		return Run{}, ErrDenied
	}
	if err := s.Authorize(ctx, subject, library, ScanControl); err != nil {
		return Run{}, err
	}
	lib, err := s.runs.Libraries.GetLibrary(ctx, library)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrDenied
	}
	if err != nil {
		return Run{}, err
	}
	launched, err := s.runs.Scans.ScanLibraryWithPolicy(ctx, lib, func(spec task.RunSpec, body task.Body) (task.RunSpec, task.Body, error) {
		return s.PrepareRun(ctx, subject, library, ScanControl, spec, body)
	})
	if err != nil {
		return Run{}, err
	}
	return Run{ID: launched.Run.ID, LibraryID: library, Status: launched.Run.Status, Current: launched.Run.Current, Total: launched.Run.Total, Coalesced: launched.Coalesced}, nil
}

func (s *Service) visible(ctx context.Context, subject Subject, snap task.Snapshot) (Run, error) {
	args := snap.Side.Args
	library, err := strconv.ParseInt(args["delegated_library_id"], 10, 64)
	if err != nil || library <= 0 || args["delegated_credential_id"] != subject.CredentialID || args["delegated_user_id"] != strconv.FormatInt(subject.UserID, 10) {
		return Run{}, ErrDenied
	}
	capability := args["delegated_capability"]
	if capability == "" {
		return Run{}, ErrDenied
	}
	if err = s.Authorize(ctx, subject, library, capability); err != nil {
		return Run{}, err
	}
	return Run{ID: snap.Run.ID, LibraryID: library, Status: snap.Run.Status, Current: snap.Run.Current, Total: snap.Run.Total}, nil
}

func (s *Service) GetRun(ctx context.Context, subject Subject, id int64) (Run, error) {
	if s.runs == nil || id <= 0 {
		return Run{}, ErrDenied
	}
	snap, err := s.runs.Engine.SnapshotOf(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrDenied
	}
	if err != nil {
		return Run{}, err
	}
	return s.visible(ctx, subject, snap)
}

// ListRuns paginates a library's run history by descending run ID, filtering before
// pagination. Even runs in the same library are private to the issuing credential. Any one
// capability of the library admits the listing; each run then shows only while its own
// capability is still authorized.
func (s *Service) ListRuns(ctx context.Context, subject Subject, library, before int64, limit int) ([]Run, error) {
	if s.runs == nil || limit < 1 || limit > 100 || before < 0 {
		return nil, ErrInvalid
	}
	if !slices.ContainsFunc(capabilities, func(capability string) bool { return s.Authorize(ctx, subject, library, capability) == nil }) {
		return nil, ErrDenied
	}
	variant := task.Variant("delegated:" + subject.CredentialID + ":")
	out := []Run{}
	// Scan past currently unauthorized capabilities internally, so an invisible
	// page cannot hide older authorized runs from the public before-ID cursor.
	for len(out) < limit {
		batch := limit - len(out)
		snaps, err := s.runs.Engine.ListSnapshots(ctx, task.RunFilter{Scope: task.ScopeLibrary, ScopeID: &library, VariantPrefix: &variant, BeforeID: before, Order: task.OrderIDDesc, Limit: batch})
		if err != nil {
			return nil, err
		}
		for _, snap := range snaps {
			before = snap.Run.ID
			run, err := s.visible(ctx, subject, snap)
			if err == nil {
				out = append(out, run)
			} else if !errors.Is(err, ErrDenied) {
				return nil, err
			}
		}
		if len(snaps) < batch {
			break
		}
	}
	return out, nil
}

func (s *Service) CancelRun(ctx context.Context, subject Subject, id int64) (Run, error) {
	if _, err := s.GetRun(ctx, subject, id); err != nil {
		return Run{}, err
	}
	if err := s.runs.Engine.Cancel(id); err != nil {
		return Run{}, err
	}
	return s.GetRun(ctx, subject, id)
}

// WaitRun never cancels the business run on timeout, disconnect or revoked access.
// Both polling and final return recheck current authority; the wait is at most 30s.
func (s *Service) WaitRun(ctx context.Context, subject Subject, id int64, millis int) (Run, error) {
	if millis < 0 || millis > 30000 {
		return Run{}, ErrInvalid
	}
	deadline := time.NewTimer(time.Duration(millis) * time.Millisecond)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		out, err := s.GetRun(ctx, subject, id)
		if err != nil {
			return Run{}, err
		}
		if out.Status.IsTerminal() {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return Run{}, ctx.Err()
		case <-deadline.C:
			return s.GetRun(ctx, subject, id)
		case <-tick.C:
		}
	}
}
