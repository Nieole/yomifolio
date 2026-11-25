package scanner

import (
	"context"
	"log/slog"
	"time"
	"yomifolio/internal/database"
	"yomifolio/internal/seriesmeta"
	"yomifolio/internal/taskcontrol"
)

type ingestionStore interface {
	WithScanIngestion(context.Context, func(database.ScanIngestionWrite) error) error
	ListSeriesByLibraryLite(context.Context, int64) ([]database.Series, error)
	RefreshSeriesDerivedData(context.Context, int64) error
}

// ingestionWriter owns one scan's claims, transactional caches and committed follow-up work.
// Only this single consumer can claim old identities; interpreters have no access to it.
type ingestionWriter struct {
	store       ingestionStore
	libraryID   int64
	seriesCache map[string]database.Series
	// preexisting holds the series that existed when the scan began; only their new books are
	// reported as added.
	preexisting     map[int64]bool
	metrics         *scanMetrics
	progress        *scanProgressReporter
	renames         *renameIndex
	refreshInterval time.Duration
	onBatch         func(string)
	onAdded         func(context.Context, []AddedBook)
	enqueueCovers   func(context.Context, []coverJob)
}

// claimedArchive adds the writer's optional old-record claim to immutable source facts.
type claimedArchive struct {
	interpretedArchive
	rehome *bookRehome
}

type ingestedBatch struct {
	seriesIDs map[int64]bool
	covers    []coverJob
	rehomes   []claimedArchive
	added     []AddedBook
}

// persist publishes cache changes only after the batch transaction commits.
func (w *ingestionWriter) persist(ctx context.Context, batch []claimedArchive) (ingestedBatch, error) {
	libIDInt := w.libraryID
	seriesCache := w.seriesCache
	var coverJobs []coverJob
	var committedRehomes []claimedArchive
	var added []AddedBook
	updatedSeriesIDs := make(map[int64]bool)
	pendingSeries := make(map[string]database.Series)
	// The batch's view of series it updates from ComicInfo: the row as last written in this
	// transaction and the source records, which the scan never writes.
	currentRows := make(map[int64]database.Series)
	sourceRecords := make(map[int64][]database.SeriesMetadataProvenance)
	err := w.store.WithScanIngestion(ctx, func(q database.ScanIngestionWrite) error {
		for _, res := range batch {
			// 获取或创建/更新归属系列
			var seriesID int64
			existingS, ok := pendingSeries[res.seriesPath]
			if !ok {
				existingS, ok = seriesCache[res.seriesPath]
			}
			if ok {
				seriesID = existingS.ID
			}

			if !ok {
				metadata := seriesmeta.PrepareScanInitial(res.seriesName, res.comicInfo)
				createdSeries, err := q.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
					LibraryID:    libIDInt,
					Name:         res.seriesName,
					Path:         res.seriesPath,
					Title:        metadata.Title,
					Summary:      metadata.Summary,
					Publisher:    metadata.Publisher,
					Status:       metadata.Status,
					Rating:       metadata.Rating,
					Language:     metadata.Language,
					LockedFields: metadata.LockedFields,
					VolumeCount:  0,
					BookCount:    0,
					TotalPages:   0,
					NameInitial:  metadata.NameInitial,
				})
				if err != nil {
					w.metrics.uncertainEntries.Add(1)
					slog.ErrorContext(ctx, "Failed to create/upsert series", "series_name", res.seriesName, "error", err)
					continue
				}
				seriesID = createdSeries.ID
				// 同一次扫描里该系列的后续卷凭它走增补分支；增补时的当前值在批事务里重读。
				pendingSeries[res.seriesPath] = createdSeries
			} else if res.comicInfo != nil {
				// The cache is as old as the scan; edits, enrichment and locks made since must
				// decide what ComicInfo may replace, so the row is read inside this transaction.
				current, records, err := w.seriesForUpdate(ctx, q, seriesID, currentRows, sourceRecords)
				if err != nil {
					w.metrics.uncertainEntries.Add(1)
					slog.WarnContext(ctx, "Skipped ComicInfo metadata update because the series could not be read", "series_id", seriesID, "error", err)
				} else {
					metadata := seriesmeta.PrepareScanUpdate(current, records, res.seriesName, res.comicInfo)
					// Identity and counts stay in the scanner's single batch upsert.
					if updated, err := q.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
						LibraryID:    libIDInt,
						Name:         res.seriesName,
						Path:         res.seriesPath,
						Title:        metadata.Title,
						Summary:      metadata.Summary,
						Publisher:    metadata.Publisher,
						Status:       metadata.Status,
						Rating:       metadata.Rating,
						Language:     metadata.Language,
						LockedFields: metadata.LockedFields,
						VolumeCount:  current.VolumeCount,
						BookCount:    current.BookCount,
						TotalPages:   current.TotalPages,
						NameInitial:  metadata.NameInitial,
					}); err == nil {
						currentRows[seriesID] = updated
					}
				}
			}
			input := res.book.forSeries(libIDInt, seriesID)
			updatedSeriesIDs[seriesID] = true

			seriesmeta.AppendScanAssociations(ctx, q, seriesID, res.comicInfo)

			// 改名重连：先把那条「文件已消失」的旧记录改挂到新路径上，紧接着的 UpsertBookByPath
			// 就会经 ON CONFLICT(path) 命中同一行——id 不变，user_book_progress / 书签 / 合集归属
			// 因而全部保留。若更新落空（并发扫描已把该行改走），退回按新书插入即可。
			rehomed := false
			if res.rehome != nil {
				affected, err := q.RehomeBookPath(ctx, database.RehomeBookPathParams{
					Path:   res.book.Path,
					ID:     res.rehome.bookID,
					Path_2: res.rehome.oldPath,
				})
				switch {
				case err != nil:
					w.metrics.uncertainEntries.Add(1)
					slog.WarnContext(ctx, "Failed to rehome renamed book", "book_id", res.rehome.bookID,
						"old_path", res.rehome.oldPath, "new_path", res.book.Path, "error", err)
				case affected == 0:
					slog.InfoContext(ctx, "Skipped rehoming renamed book because the row moved concurrently",
						"book_id", res.rehome.bookID, "old_path", res.rehome.oldPath)
				default:
					rehomed = true
					committedRehomes = append(committedRehomes, res)
				}
			}

			// 同路径的书只更新元数据，保留 last_read_page / last_read_at，返回带主键的对象。
			// 两个 upsert 的差别只在 page_count 与 cover_path 该不该被这一趟改写，语义见 sql/query.sql。
			var actualBook database.Book
			var err error
			if res.openedArchive {
				actualBook, err = q.UpsertBookByPath(ctx, input)
			} else {
				actualBook, err = q.UpsertBookByPathKeepingPagesAndCover(ctx,
					database.UpsertBookByPathKeepingPagesAndCoverParams(input))
			}
			if err != nil {
				w.metrics.uncertainEntries.Add(1)
				slog.ErrorContext(ctx, "Failed to upsert book", "path", res.book.Path, "error", err)
				continue
			}
			if !res.known && !rehomed && w.preexisting[seriesID] {
				added = append(added, AddedBook{SeriesID: seriesID, BookID: actualBook.ID, Name: res.book.Name, ComicInfo: res.comicInfo})
			}
			if err := q.UpdateBookIdentity(ctx, database.UpdateBookIdentityParams{
				ID:                   actualBook.ID,
				FileHash:             string(res.fileHash),
				QuickHash:            string(res.quickHash),
				PathFingerprint:      string(res.pathFingerprint),
				PathFingerprintNoExt: string(res.pathFingerprintNoExt),
			}); err != nil {
				w.metrics.uncertainEntries.Add(1)
				slog.WarnContext(ctx, "Failed to update book identity", "book_id", actualBook.ID, "path", actualBook.Path, "error", err)
			}
			if res.coverCandidate != nil && (!actualBook.CoverPath.Valid || actualBook.CoverPath.String == "") {
				coverJobs = append(coverJobs, coverJob{
					bookID:    actualBook.ID,
					seriesID:  actualBook.SeriesID,
					libraryID: libIDInt,
					candidate: *res.coverCandidate,
				})
			}

		}
		// 读模型刷新不放在批事务内：touched 系列交由 derivedSeries.refresh 节流
		// 与 derivedSeries.drain 在扫描末尾刷新，避免逐批重算大系列。
		return nil
	})
	if err != nil {
		return ingestedBatch{}, err
	}
	for path, series := range pendingSeries {
		seriesCache[path] = series
	}
	return ingestedBatch{seriesIDs: updatedSeriesIDs, covers: coverJobs, rehomes: committedRehomes, added: added}, nil
}

// seriesForUpdate returns the series row and source records a ComicInfo update starts from,
// reading each series once per batch and then following the batch's own writes.
func (w *ingestionWriter) seriesForUpdate(ctx context.Context, q database.ScanIngestionWrite, id int64, rows map[int64]database.Series, records map[int64][]database.SeriesMetadataProvenance) (database.Series, []database.SeriesMetadataProvenance, error) {
	if _, read := records[id]; !read {
		current, err := q.GetSeries(ctx, id)
		if err != nil {
			return database.Series{}, nil, err
		}
		sources, err := q.GetSeriesMetadataProvenance(ctx, id)
		if err != nil {
			return database.Series{}, nil, err
		}
		rows[id], records[id] = current, sources
		if records[id] == nil {
			records[id] = []database.SeriesMetadataProvenance{}
		}
	}
	return rows[id], records[id], nil
}

func (w *ingestionWriter) consume(ctx context.Context, results <-chan interpretedArchive) {
	w.seriesCache = make(map[string]database.Series)
	w.preexisting = make(map[int64]bool)
	existing, err := w.store.ListSeriesByLibraryLite(ctx, w.libraryID)
	if err != nil {
		w.metrics.uncertainEntries.Add(1)
	}
	for _, series := range existing {
		w.seriesCache[series.Path] = series
		w.preexisting[series.ID] = true
	}
	dirty := newDerivedSeries(w.store)
	var batch []claimedArchive
	const batchSize = 100
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := taskcontrol.Wait(ctx); err != nil {
			return
		}
		w.progress.publish("writing_database", "", true)
		committed, err := w.persist(ctx, batch)
		if err != nil {
			slog.ErrorContext(ctx, "Batch ingest transaction failed, dropping batch", "book_count", len(batch), "error", err)
			w.metrics.failedArchives.Add(int64(len(batch)))
			w.progress.warn(ScanWarning{Code: WarnBatchIngestDropped, Detail: err.Error(), Count: int64(len(batch))})
		} else {
			w.metrics.rehomedBooks.Add(int64(len(committed.rehomes)))
			for _, res := range committed.rehomes {
				slog.InfoContext(ctx, "Rehomed renamed book", "book_id", res.rehome.bookID, "old_path", res.rehome.oldPath, "new_path", res.book.Path, "match", res.rehome.reason)
			}
			slog.InfoContext(ctx, "Successfully ingested batch", "book_count", len(batch))
			if w.onAdded != nil && len(committed.added) > 0 {
				w.onAdded(context.WithoutCancel(ctx), committed.added)
			}
			dirty.touch(committed.seriesIDs)
			if w.onBatch != nil {
				w.onBatch("batch_inserted")
			}
			w.progress.publish("queueing_covers", "", true)
			w.enqueueCovers(ctx, committed.covers)
		}
		batch = batch[:0]
	}
	interval := w.refreshInterval
	if interval <= 0 {
		interval = defaultDirtyRefreshInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case res, ok := <-results:
			if !ok {
				flush()
				dirty.drain(ctx, w.metrics)
				if w.onBatch != nil {
					w.onBatch("scan_completed")
				}
				return
			}
			// Filesystem absence checks happen before taking the SQLite write transaction.
			claimed := claimedArchive{interpretedArchive: res, rehome: w.renames.claim(rehomeRequest{path: res.book.Path, size: res.book.Size, modTime: res.book.FileModifiedAt, quickHash: string(res.quickHash), fileHash: string(res.fileHash)})}
			batch = append(batch, claimed)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
			dirty.refresh(ctx)
		}
	}
}

func (b interpretedBook) forSeries(libraryID, seriesID int64) database.UpsertBookByPathParams {
	return database.UpsertBookByPathParams{SeriesID: seriesID, LibraryID: libraryID, Name: b.Name, Path: b.Path, Size: b.Size, FileModifiedAt: b.FileModifiedAt, Volume: b.Volume, Title: b.Title, PageCount: b.PageCount, SortNumber: b.SortNumber, CoverPath: b.CoverPath}
}
