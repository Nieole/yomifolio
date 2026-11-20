package taskstore

import (
	"context"
	"yomifolio/internal/task"
)

// CreateRunWithArgs commits required attribution and the run together, or neither.
func (s *Store) CreateRunWithArgs(ctx context.Context, run task.Run, args map[string]string) (task.Run, error) {
	params, err := encodeMessageParams(run.MessageParams)
	if err != nil {
		return task.Run{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return task.Run{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO `+tableRuns+` (`+runInsertColumns+`) VALUES (`+runPlaceholders+`)`, runValues(run, params)...)
	if err != nil {
		return task.Run{}, admissionError(err, run.Status)
	}
	run.ID, err = result.LastInsertId()
	if err != nil {
		return task.Run{}, err
	}
	for key, value := range args {
		if _, err = tx.ExecContext(ctx, `INSERT INTO `+tableRunArgs+` (run_id,key,value) VALUES (?,?,?)`, run.ID, key, value); err != nil {
			return task.Run{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return task.Run{}, err
	}
	return run, nil
}
