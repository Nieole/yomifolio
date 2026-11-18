package reading_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/reading"
)

func TestPositionAuthoritativeSnapshots(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	service := reading.New(store, nil)
	empty, err := service.GetPosition(ctx, uid, bid)
	if err != nil || empty.Present {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	if _, err := service.GetPosition(ctx, uid, bid+100); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.FixedZone("offset", 3600))
	later := at.Add(time.Nanosecond)
	for _, test := range []struct {
		id                        string
		at                        time.Time
		page                      int64
		applied, duplicate, stale bool
		winner                    string
		winnerAt                  time.Time
		winnerPage                int64
	}{
		{"a", at, 10, true, false, false, "a", at, 10},
		{"b", later, 3, true, false, false, "b", later, 3},
		{"a", at, 10, false, true, false, "b", later, 3},
		{"c", at, 15, false, false, true, "b", later, 3},
		{"equal", later, 8, false, false, true, "b", later, 3},
	} {
		got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: test.page, OccurredAt: &test.at, FactID: test.id})
		snap := got.Snapshot
		if err != nil || got.Applied != test.applied || got.Duplicate != test.duplicate || got.Stale != test.stale || snap.Page != test.winnerPage || snap.FactID != test.winner || snap.ChangedAtNS != test.winnerAt.UnixNano() || !snap.LastReadAt.Time.Equal(test.winnerAt) {
			t.Fatalf("test=%+v got=%+v err=%v", test, got, err)
		}
	}
	clearAt := later.Add(time.Second)
	if _, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, OccurredAt: &clearAt, OperationID: "clear"}); err != nil {
		t.Fatal(err)
	}
	cleared, err := service.GetPosition(ctx, uid, bid)
	if err != nil || !cleared.Present || !cleared.Cleared || cleared.Page != 0 || cleared.FactID != "mark:clear" || cleared.ChangedAtNS != clearAt.UnixNano() {
		t.Fatalf("clear=%+v err=%v", cleared, err)
	}
	replay, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 10, OccurredAt: &at, FactID: "a"})
	if err != nil || !replay.Duplicate || !replay.Snapshot.Cleared {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}

func TestPositionConcurrentACKRetainsOwnDecisionSnapshot(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 123, time.UTC)
	var wg sync.WaitGroup
	for i := 1; i <= 12; i++ {
		wg.Go(func() {
			at := base.Add(time.Duration(i) * time.Second)
			id := at.Format(time.RFC3339Nano)
			got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: int64(i), OccurredAt: &at, FactID: id})
			if err != nil {
				t.Error(err)
				return
			}
			if got.Applied && (got.Snapshot.FactID != id || got.Snapshot.Page != int64(i) || got.Snapshot.ChangedAtNS != at.UnixNano()) {
				t.Errorf("ACK=%+v", got)
			}
			if got.Stale && got.Snapshot.ChangedAtNS < at.UnixNano() {
				t.Errorf("stale ACK=%+v", got)
			}
		})
	}
	wg.Wait()
}

func TestPositionUnknownPageReadFlagAndRollbackSnapshot(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	if _, err := store.DB().Exec(`UPDATE books SET page_count=0 WHERE id=?`, bid); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC)
	service := reading.New(store, nil)
	if _, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	snap, err := service.GetPosition(ctx, uid, bid)
	if err != nil || !snap.IsRead || snap.Page != 0 || snap.Cleared {
		t.Fatalf("snapshot=%+v err=%v", snap, err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_basis BEFORE UPDATE ON reading_position_basis BEGIN SELECT RAISE(ABORT,'basis unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Second)
	got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 999, OccurredAt: &later, FactID: "rollback"})
	if err == nil || got.Applied || got.Snapshot.Present {
		t.Fatalf("rollback=%+v err=%v", got, err)
	}
	after, err := service.GetPosition(ctx, uid, bid)
	if err != nil || after != snap {
		t.Fatalf("after=%+v before=%+v err=%v", after, snap, err)
	}
	var receipts int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM reading_position_receipts WHERE fact_id='rollback'`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("receipts=%d err=%v", receipts, err)
	}
}
