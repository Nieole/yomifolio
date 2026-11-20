package bootstrap_test

import (
	"context"
	"testing"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
	"yomifolio/internal/taskstore"
)

// The lost scheduler models a crash after durable admission, just as the ordinary
// bootstrap recovery test does. Only the reopened production graph may recover.
func TestOpenDoesNotReplayDelegatedRun(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	_, err := store.CreateUser(ctx, database.CreateUserParams{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Allowed", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	svc := delegated.New(delegated.NewSQLStore(store.DB()))
	issued, err := svc.Issue(ctx, 1, delegated.Restrictions{UserID: 1, Name: "scan", LibraryIDs: []int64{lib.ID}, Capabilities: []string{delegated.ScanControl}})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := svc.Authenticate(ctx, issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	spec, body, err := svc.PrepareRun(ctx, subject, lib.ID, delegated.ScanControl, task.RunSpec{Identity: task.Identity{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: lib.ID}, LibraryIDs: []int64{lib.ID}, Trigger: task.TriggerManual, CanCancel: true}, func(context.Context, *runhandle.Handle) (task.Result, error) {
		t.Error("lost process must not execute")
		return task.Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(func()) {}})
	launched, err := engine.Start(ctx, spec, body)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.NewStore(cfg.Snapshot().Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	p, err := bootstrap.Open(ctx, reopened, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	svc = delegated.NewWithRuns(delegated.NewSQLStore(reopened.DB()), delegated.RunAccess{Engine: p.Application.Engine, Scans: p.Application.Scans, Libraries: reopened})
	subject, err = svc.Authenticate(ctx, issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.GetRun(ctx, subject, launched.Run.ID)
	if err != nil || run.Status != task.StatusInterrupted {
		t.Fatalf("recovery %+v %v", run, err)
	}
	runs, err := svc.ListRuns(ctx, subject, lib.ID, 0, 10)
	if err != nil || len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("unexpected replay %+v %v", runs, err)
	}
	if p.Application.Registry.Retryable(spec.Identity.Type, spec.Identity.Variant) || p.Application.Registry.Resumable(spec.Identity.Type, spec.Identity.Variant) {
		t.Fatal("delegated identity bypasses current authority through restart")
	}
}
