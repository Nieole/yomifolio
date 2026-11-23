package enrichment_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

type evidence struct{ summary string }

func (e evidence) GetMatchProposal(context.Context, delegated.Subject, int64, int64) (delegated.MatchProposal, error) {
	summary := e.summary
	if summary == "" {
		summary = "Space journey"
	}
	return delegated.MatchProposal{Evidence: &metadata.MatchEvidence{IdentityConfirmed: true, EditionConfirmed: true, Summary: "Same identity and edition", Fields: map[string]metadata.FieldEvidence{"summary": {Source: "bangumi", Metadata: metadata.SourceFacts{Summary: summary, SourceURL: "https://bgm.tv/subject/1"}}}}}, nil
}
func TestEnrichmentAtomicReplayAndPersistentUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enrichment.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	if _, err = store.DB().Exec(`INSERT INTO users(id,username,role) VALUES(1,'owner','admin')`); err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "L", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "S", Path: lib.Path + "/s"})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := store.CreateMetadataReview(ctx, database.CreateMetadataReviewParams{SeriesID: series.ID, Provider: "bangumi", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	svc := enrichment.New(store.DB(), evidence{}, nil)
	actor := delegated.Subject{UserID: 1}
	in := enrichment.Input{RequestKey: "fill", SeriesID: series.ID, ProposalIDs: []int64{proposal.ID}, Fields: []string{"summary"}}
	out, err := svc.Apply(ctx, actor, in)
	if err != nil || out.Outcome != "changed" || !out.Applied {
		t.Fatalf("%+v %v", out, err)
	}
	replay, err := svc.Apply(ctx, actor, in)
	if err != nil || !replay.Replayed || replay.Applied || replay.ID != out.ID {
		t.Fatalf("%+v %v", replay, err)
	}
	svc = enrichment.New(store.DB(), evidence{}, nil)
	undo, err := svc.Undo(ctx, actor, enrichment.UndoInput{RequestKey: "undo", OperationID: out.ID})
	if err != nil || undo.Outcome != "changed" {
		t.Fatalf("%+v %v", undo, err)
	}
	saved, err := store.GetSeries(ctx, series.ID)
	if err != nil || saved.Summary.Valid {
		t.Fatalf("%+v %v", saved, err)
	}

	// Full receipts are never silently lost to an oversized batch response.
	svc = enrichment.New(store.DB(), evidence{summary: strings.Repeat("x", 30000)}, nil)
	batch, err := svc.Batch(ctx, actor, []enrichment.Input{
		{RequestKey: "large-first", SeriesID: series.ID, ProposalIDs: []int64{proposal.ID}, Fields: []string{"summary"}},
		{RequestKey: "large-second", SeriesID: series.ID, ProposalIDs: []int64{proposal.ID}, Fields: []string{"summary"}},
	})
	if err != nil || len(batch.Operations) != 2 || batch.Operations[0].Outcome != "changed" || batch.Operations[1].Outcome != "unprocessed" {
		t.Fatalf("batch budget: %+v %v", batch, err)
	}
	encoded, _ := json.Marshal(batch)
	if len(encoded) > 240<<10 {
		t.Fatalf("unbounded response: %d", len(encoded))
	}
	original, err := svc.Get(ctx, actor, out.ID)
	if err != nil || original.UndoneBy != undo.ID {
		t.Fatalf("%+v %v", original, err)
	}
	// Closure changes the public inbox even when no metadata field changes.
	publications := 0
	svc = enrichment.New(store.DB(), evidence{}, func() { publications++ })
	reject := enrichment.Input{RequestKey: "reject", SeriesID: series.ID, ProposalIDs: []int64{proposal.ID}, Adjudication: &enrichment.Adjudication{Mode: "reject", Reason: "Discard all remaining differences"}}
	closed, err := svc.Apply(ctx, actor, reject)
	if err != nil || closed.Adjudication.Status != "rejected" || publications != 1 {
		t.Fatal("closure did not refresh the inbox", closed, publications, err)
	}
	if _, err = svc.Apply(ctx, actor, reject); err != nil || publications != 1 {
		t.Fatal("replay republished closure", publications, err)
	}

}
