package enrichment

import (
	"context"
	"strconv"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
	"yomifolio/internal/taskcontrol"
)

// CleanupAccess uses the existing runtime. No cleanup recovery/resident task exists.
type CleanupAccess struct {
	Delegated *delegated.Service
	Start     func(context.Context, task.RunSpec, task.Body) (task.Launched, error)
}

func (s *Service) WithCleanup(access CleanupAccess) *Service { s.cleanup = &access; return s }

type CleanupBatchInput struct {
	LibraryID  int64          `json:"library_id"`
	Operations []CleanupInput `json:"operations"`
}

// StartCleanup accepts a bounded, explicit same-library list, never a discovery
// sweep. Each item has its own durable retry receipt. A disconnected client can
// query the shared run and individual cleanup receipts by their request keys.
func (s *Service) StartCleanup(ctx context.Context, actor delegated.Subject, in CleanupBatchInput) (delegated.Run, error) {
	if s.cleanup == nil || len(in.Operations) < 1 || len(in.Operations) > 20 {
		return delegated.Run{}, delegated.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return delegated.Run{}, err
	}
	defer tx.Rollback()
	for _, item := range in.Operations {
		if !validKey(item.RequestKey) {
			return delegated.Run{}, delegated.ErrInvalid
		}
		r, e := (database.MetadataJournal{Tx: tx}).Get(ctx, item.OperationID)
		if e != nil || r.LibraryID != in.LibraryID {
			return delegated.Run{}, delegated.ErrDenied
		}
		if e = cleanupAuthority(ctx, tx, actor, r); e != nil {
			return delegated.Run{}, e
		}
	}
	if err = tx.Rollback(); err != nil {
		return delegated.Run{}, err
	}
	id := operation(actor, "", 0, in.LibraryID, "cleanup").ID
	spec := task.RunSpec{Identity: task.Identity{Type: "metadata_cleanup", Scope: task.ScopeLibrary, ScopeID: in.LibraryID, Variant: task.Variant(id)}, LibraryIDs: []int64{in.LibraryID}, Trigger: task.TriggerManual, Key: "metadata_cleanup", Total: len(in.Operations), CanCancel: true}
	body := func(ctx context.Context, h *runhandle.Handle) (task.Result, error) {
		partial := false
		for index, item := range in.Operations {
			if e := taskcontrol.Wait(ctx); e != nil {
				return task.Result{}, e
			}
			result, e := s.Cleanup(ctx, actor, item)
			if e != nil {
				if check := taskcontrol.Wait(ctx); check != nil {
					return task.Result{}, check
				}
				return task.Result{}, e
			}
			partial = partial || result.Outcome == "partial"
			current := index + 1
			h.Report(runhandle.Frame{Current: &current, Phase: "cleanup", Metrics: map[string]int64{"processed": int64(current)}})
		}
		if partial {
			return task.Result{Params: map[string]string{"partial": strconv.FormatBool(partial)}}, nil
		}
		return task.Result{}, nil
	}
	if actor.CredentialID != "" {
		spec, body, err = s.cleanup.Delegated.PrepareRun(ctx, actor, in.LibraryID, delegated.Cleanup, spec, body)
		if err != nil {
			return delegated.Run{}, err
		}
	} else {
		spec.ExecutionCheck = func(ctx context.Context) error {
			tx, e := s.db.BeginTx(ctx, nil)
			if e != nil {
				return e
			}
			defer tx.Rollback()
			if database.RequireMetadataAdmin(ctx, tx, actor.UserID) != nil {
				return context.Canceled
			}
			return nil
		}
	}
	launched, err := s.cleanup.Start(ctx, spec, body)
	if err != nil {
		return delegated.Run{}, err
	}
	return delegated.Run{ID: launched.Run.ID, LibraryID: in.LibraryID, Status: launched.Run.Status, Current: launched.Run.Current, Total: launched.Run.Total, Coalesced: launched.Coalesced}, nil
}

// GetCleanup never authorizes by guessed request ID alone or falls back to Read.
func (s *Service) GetCleanup(ctx context.Context, actor delegated.Subject, key string) (CleanupResult, error) {
	if !validKey(key) {
		return CleanupResult{}, delegated.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CleanupResult{}, err
	}
	defer tx.Rollback()
	var payload, operationID string
	err = tx.QueryRowContext(ctx, `SELECT operation_id,payload FROM metadata_cleanup_requests WHERE user_id=? AND credential_id=? AND request_key=?`, actor.UserID, actor.CredentialID, key).Scan(&operationID, &payload)
	if err != nil {
		return CleanupResult{}, delegated.ErrDenied
	}
	r, err := (database.MetadataJournal{Tx: tx}).Get(ctx, operationID)
	if err != nil {
		return CleanupResult{}, delegated.ErrDenied
	}
	if err = cleanupAuthority(ctx, tx, actor, r); err != nil {
		return CleanupResult{}, err
	}
	return decodeCleanup(payload)
}

// InspectCleanup is the administrator's durable result view, including results
// created by revoked credentials. It never grants delegated cross-credential access.
func (s *Service) InspectCleanup(ctx context.Context, actor delegated.Subject, id string) (CleanupResult, error) {
	if actor.CredentialID != "" {
		return CleanupResult{}, delegated.ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CleanupResult{}, err
	}
	defer tx.Rollback()
	if database.RequireMetadataAdmin(ctx, tx, actor.UserID) != nil {
		return CleanupResult{}, delegated.ErrDenied
	}
	var payload, key, hash, operationID, credentialID string
	var userID int64
	if err = tx.QueryRowContext(ctx, `SELECT payload,request_key,request_hash,operation_id,user_id,credential_id FROM metadata_cleanup_requests WHERE id=?`, id).Scan(&payload, &key, &hash, &operationID, &userID, &credentialID); err != nil {
		return CleanupResult{}, delegated.ErrDenied
	}
	out, err := decodeCleanup(payload)
	if err != nil {
		return CleanupResult{}, err
	}
	// Never transfer another actor's retry namespace to the inspecting admin.
	// Existing printable ASCII keys (1-80 bytes) and V1 digests stay unchanged.
	if out.Outcome == "pending" && userID == actor.UserID && credentialID == "" &&
		validKey(key) && out.OperationID == operationID && hash == cleanupDigest(CleanupInput{RequestKey: key, OperationID: operationID}) {
		r, e := (database.MetadataJournal{Tx: tx}).Get(ctx, operationID)
		if e == nil && cleanupAuthority(ctx, tx, actor, r) == nil {
			out.RetryRequestKey = key
		}
	}
	return out, nil
}
