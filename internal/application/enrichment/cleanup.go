package enrichment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/config"
	"yomifolio/internal/covermaterial"
	"yomifolio/internal/database"
	"yomifolio/internal/taskcontrol"
)

// CleanupInput uses an independent, versioned intent and retry namespace.
type CleanupInput struct {
	RequestKey  string `json:"request_key"`
	OperationID string `json:"operation_id"`
}
type MaterialResult struct {
	Key     string `json:"key"`
	Outcome string `json:"outcome"`
}
type CleanupResult struct {
	// RetryRequestKey is projected only to the original web actor on inspection.
	// It is not a credential and never changes the stored V1 intent.
	RetryRequestKey string            `json:"retry_request_key,omitempty"`
	ID              string            `json:"id"`
	OperationID     string            `json:"operation_id"`
	Actor           delegated.Subject `json:"actor"`
	CreatedAt       string            `json:"created_at"`
	Outcome         string            `json:"outcome"`
	UndoUnavailable bool              `json:"undo_unavailable"`
	Materials       []MaterialResult  `json:"materials"`
	Replayed        bool              `json:"replayed"`
}

func cleanupDigest(in CleanupInput) string {
	b, _ := json.Marshal(struct {
		Version   int    `json:"version"`
		Key       string `json:"request_key"`
		Operation string `json:"operation_id"`
	}{1, in.RequestKey, in.OperationID})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func cleanupAuthority(ctx context.Context, tx *sql.Tx, actor delegated.Subject, receipt database.MetadataReceipt) error {
	// Historical library attribution alone cannot authorize a moved target.
	current, err := database.New(tx).GetSeries(ctx, receipt.SeriesID)
	if err != nil {
		if actor.CredentialID != "" {
			return delegated.ErrDenied
		}
	} else if current.LibraryID != receipt.LibraryID {
		return delegated.ErrDenied
	}
	if actor.CredentialID == "" {
		if database.RequireMetadataAdmin(ctx, tx, actor.UserID) != nil {
			return delegated.ErrDenied
		}
		return nil
	}
	return delegated.AuthorizeTx(ctx, tx, actor, receipt.LibraryID, delegated.Cleanup)
}
func writeCleanup(ctx context.Context, tx *sql.Tx, out CleanupResult) error {
	out.Replayed = false
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE metadata_cleanup_requests SET payload=? WHERE id=?`, string(b), out.ID)
	return err
}

// Cleanup permanently discards one operation's reversal snapshots. At most two
// immutable files are considered, never a filesystem sweep. Marking loss of undo,
// releasing references and fencing unreferenced keys commit BEFORE unlink. Pending
// unlinks can resume only by replay under current cleanup authority; no startup janitor.
func (s *Service) Cleanup(ctx context.Context, actor delegated.Subject, in CleanupInput) (CleanupResult, error) {
	if !validKey(in.RequestKey) || len(in.OperationID) == 0 || len(in.OperationID) > 80 {
		return CleanupResult{}, delegated.ErrInvalid
	}
	if err := taskcontrol.Wait(ctx); err != nil {
		return CleanupResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CleanupResult{}, err
	}
	defer tx.Rollback()
	j := database.MetadataJournal{Tx: tx}
	receipt, err := j.Get(ctx, in.OperationID)
	if err != nil {
		return CleanupResult{}, delegated.ErrDenied
	}
	if err = cleanupAuthority(ctx, tx, actor, receipt); err != nil {
		return CleanupResult{}, err
	}
	var out CleanupResult
	var hash, payload string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,payload FROM metadata_cleanup_requests WHERE user_id=? AND credential_id=? AND request_key=?`, actor.UserID, actor.CredentialID, in.RequestKey).Scan(&hash, &payload)
	replay := err == nil
	if replay {
		if hash != cleanupDigest(in) {
			return out, delegated.ErrInvalid
		}
		if err = json.Unmarshal([]byte(payload), &out); err != nil {
			return out, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	} else {
		original, e := decode(receipt)
		if e != nil {
			return out, e
		}
		identity := operation(actor, in.RequestKey, receipt.SeriesID, receipt.LibraryID, "cleanup")
		out = CleanupResult{ID: identity.ID, OperationID: receipt.ID, Actor: actor, CreatedAt: identity.CreatedAt, Outcome: "pending", UndoUnavailable: true, Materials: []MaterialResult{}}
		if original.UndoUnavailable {
			out.Outcome = "already-cleaned"
		} else {
			keys := []string{original.OldCoverMaterial, original.NewCoverMaterial}
			original.Before = State{}
			original.After = State{}
			original.OldCoverMaterial = ""
			original.NewCoverMaterial = ""
			original.CoverSelection = nil
			original.UndoUnavailable = true
			original.CleanedBy = out.ID
			original.Applied = false
			for i := range original.Fields {
				original.Fields[i].Sources = nil
			}
			b, e := json.Marshal(original)
			if e != nil {
				return out, e
			}
			if _, err = tx.ExecContext(ctx, `UPDATE metadata_operations SET payload=? WHERE id=?`, string(b), receipt.ID); err != nil {
				return out, err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM metadata_cover_materials WHERE operation_id=?`, receipt.ID); err != nil {
				return out, err
			}
			for _, key := range keys {
				if key == "" {
					continue
				}
				if len(out.Materials) > 0 && out.Materials[0].Key == key {
					continue
				}
				material := MaterialResult{Key: key, Outcome: "protected"}
				if covermaterial.ValidKey(key) {
					var referenced bool
					err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM books WHERE cover_path=? UNION ALL SELECT 1 FROM series_stats WHERE cover_path=? UNION ALL SELECT 1 FROM metadata_cover_materials WHERE path=?)`, key, key, key).Scan(&referenced)
					if err != nil {
						return out, err
					}
					if !referenced {
						_, err = tx.ExecContext(ctx, `INSERT INTO metadata_material_tombstones(path,cleanup_id) VALUES(?,?) ON CONFLICT(path) DO NOTHING`, key, out.ID)
						if err != nil {
							return out, err
						}
						material.Outcome = "pending"
					}
				}
				out.Materials = append(out.Materials, material)
			}
		}
		b, e := json.Marshal(out)
		if e != nil {
			return out, e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO metadata_cleanup_requests(id,user_id,credential_id,request_key,request_hash,operation_id,payload) VALUES(?,?,?,?,?,?,?)`, out.ID, actor.UserID, actor.CredentialID, in.RequestKey, cleanupDigest(in), receipt.ID, string(b))
		if err != nil {
			return out, err
		}
	}
	if err = tx.Commit(); err != nil {
		return CleanupResult{}, err
	}
	if out.Outcome == "pending" {
		for i := range out.Materials {
			if out.Materials[i].Outcome != "pending" {
				continue
			}
			if err = taskcontrol.Wait(ctx); err != nil {
				return out, err
			}
			// Keep current authority and the permanent publication fence through unlink.
			unit, e := s.db.BeginTx(ctx, nil)
			if e != nil {
				return out, e
			}
			e = cleanupAuthority(ctx, unit, actor, receipt)
			if e == nil {
				var retired bool
				e = unit.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM metadata_material_tombstones WHERE path=?)`, out.Materials[i].Key).Scan(&retired)
				if e == nil && !retired {
					e = delegated.ErrDenied
				}
			}
			if e != nil {
				_ = unit.Rollback()
				return out, e
			}
			// A concurrent same-key replay may have completed this unit while
			// this call waited for the write transaction. Never overwrite its
			// durable file failure or terminal result with a stale local copy.
			var latest string
			e = unit.QueryRowContext(ctx, `SELECT payload FROM metadata_cleanup_requests WHERE id=?`, out.ID).Scan(&latest)
			if e == nil {
				out, e = decodeCleanup(latest)
			}
			if e != nil {
				_ = unit.Rollback()
				return out, e
			}
			if out.Materials[i].Outcome != "pending" {
				_ = unit.Rollback()
				continue
			}
			if s.covers == nil {
				e = delegated.ErrInvalid
			} else {
				e = covermaterial.RemoveRetired(config.ThumbnailDir(s.covers.Config()), out.Materials[i].Key)
			}
			out.Materials[i].Outcome = "deleted"
			if e != nil {
				out.Materials[i].Outcome = "failed"
			}
			if e = writeCleanup(ctx, unit, out); e == nil {
				e = unit.Commit()
			} else {
				_ = unit.Rollback()
			}
			if e != nil {
				return out, e
			}
		}
		unit, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return out, e
		}
		defer unit.Rollback()
		if e = cleanupAuthority(ctx, unit, actor, receipt); e != nil {
			return out, e
		}
		var latest string
		if e = unit.QueryRowContext(ctx, `SELECT payload FROM metadata_cleanup_requests WHERE id=?`, out.ID).Scan(&latest); e != nil {
			return out, e
		}
		if out, e = decodeCleanup(latest); e != nil {
			return out, e
		}
		if out.Outcome == "pending" {
			out.Outcome = "complete"
			for _, m := range out.Materials {
				if m.Outcome == "failed" {
					out.Outcome = "partial"
				}
			}
		}
		if e = writeCleanup(ctx, unit, out); e != nil {
			return out, e
		}
		if e = unit.Commit(); e != nil {
			return out, e
		}
	}
	out.Replayed = replay
	return out, nil
}

func decodeCleanup(payload string) (CleanupResult, error) {
	var out CleanupResult
	err := json.Unmarshal([]byte(payload), &out)
	return out, err
}
