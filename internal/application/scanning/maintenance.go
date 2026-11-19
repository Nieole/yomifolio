package scanning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"yomifolio/internal/database"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/scanner"
)

// ReindexLibraries waits for all forced scans inside an already admitted maintenance run.
// It does not acquire a second run slot; cover batches still use their own queued runs.
func (c *Service) ReindexLibraries(ctx context.Context, handle *runhandle.Handle, libs []database.Library) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var scanErrors []error
	defer wg.Wait()
	for _, lib := range libs {
		if err := handle.Checkpoint(ctx); err != nil {
			return err
		}
		wg.Go(func() {
			defer c.effects.PurgeReadingPaths()
			outcome, err := c.scanner.IngestLibrary(ctx, lib.ID, lib.Path, scanner.LibraryScanOptions{Force: true}, nil)
			if err == nil {
				err = outcome.Err()
			}
			if err != nil {
				slog.ErrorContext(ctx, "Global scan of library failed", "library_id", lib.ID, "path", lib.Path, "error", err)
				mu.Lock()
				scanErrors = append(scanErrors, fmt.Errorf("library %d (%s): %w", lib.ID, lib.Path, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	// Keep cancellation as the primary outcome even when a scan also failed.
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(scanErrors...)
}

// RebuildThumbnails scans every known format in an existing maintenance run.
func (c *Service) RebuildThumbnails(ctx context.Context, h *runhandle.Handle, libs []database.Library, observer func(database.Library, int) scanner.ScanObserver) error {
	scan := c.scanner
	for _, lib := range libs {
		if err := h.Checkpoint(ctx); err != nil {
			return err
		}
		outcome, err := scan.IngestLibrary(ctx, lib.ID, lib.Path, scanner.LibraryScanOptions{Force: true, IgnoreFormatFilter: true}, observer(lib, len(libs)))
		if err == nil {
			err = outcome.Err()
		}
		if err != nil {
			return err
		}
		c.effects.PurgeReadingPaths()
	}
	return nil
}

// CleanupThumbnails removes only unreferenced generated covers.
func (c *Service) CleanupThumbnails(ctx context.Context, progress func(int, int)) error {
	return c.scanner.CleanupThumbnails(ctx, progress)
}
