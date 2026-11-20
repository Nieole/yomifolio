package bootstrap_test

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
	"yomifolio/internal/taskcontrol"
)

func TestDelegatedRunRechecksAtExecution(t *testing.T) {
	for _, change := range []string{"revoke", "narrow", "password"} {
		t.Run(change, func(t *testing.T) { testDelegatedRunRechecksAtExecution(t, change) })
	}
}

func testDelegatedRunRechecksAtExecution(t *testing.T, change string) {
	store, cfg := fixture(t)
	p, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, timeout := context.WithTimeout(context.Background(), 20*time.Second)
	defer timeout()
	router := chi.NewRouter()
	p.HTTP.SetupRoutes(router)
	setup := httptest.NewRecorder()
	router.ServeHTTP(setup, httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"username":"admin","password":"password123"}`)))
	if setup.Code != 200 {
		t.Fatalf("setup %d %s", setup.Code, setup.Body)
	}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	if err = json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
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
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseBody := func() { once.Do(func() { close(release) }) }
	defer releaseBody()
	spec := task.RunSpec{Identity: task.Identity{Type: "test_delegated", Scope: task.ScopeLibrary, ScopeID: lib.ID}, Trigger: task.TriggerManual, LibraryIDs: []int64{lib.ID}, CanCancel: true}
	body := task.Body(func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
		// A committed write remains committed when authority is later revoked.
		tx, err := store.DB().BeginTx(ctx, nil)
		if err != nil {
			return task.Result{}, err
		}
		defer tx.Rollback()
		if err = delegated.AuthorizeTx(ctx, tx, subject, lib.ID, delegated.ScanControl); err != nil {
			return task.Result{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE libraries SET name='Committed' WHERE id=?`, lib.ID); err != nil {
			return task.Result{}, err
		}
		if err = tx.Commit(); err != nil {
			return task.Result{}, err
		}
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return task.Result{}, ctx.Err()
		}
		return task.Result{}, taskcontrol.Wait(ctx)
	})
	spec, body, err = svc.PrepareRun(ctx, subject, lib.ID, delegated.ScanControl, spec, body)
	if err != nil {
		t.Fatal(err)
	}
	launched, err := p.Application.Runs.Start(ctx, spec, body)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	switch change {
	case "revoke":
		err = svc.Revoke(ctx, 1, issued.ID)
	case "narrow":
		_, err = svc.Narrow(ctx, 1, issued.ID, delegated.Restrictions{UserID: 1, Name: "scan", LibraryIDs: []int64{}, Capabilities: []string{delegated.ScanControl}})
	case "password":
		r := httptest.NewRequest("POST", "/api/auth/change-password", strings.NewReader(`{"current_password":"password123","new_password":"replacement123"}`))
		r.AddCookie(setup.Result().Cookies()[0])
		r.Header.Set("X-CSRF-Token", auth.CSRF)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("password %d %s", w.Code, w.Body)
		}
	}
	releaseBody()
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.Application.Engine.Await(ctx, launched.Run.ID)
	if err != nil || run.Status != task.StatusCancelled {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	current, err := store.GetLibrary(ctx, lib.ID)
	if err != nil || current.Name != "Committed" {
		t.Fatalf("committed content rolled back %+v %v", current, err)
	}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = delegated.AuthorizeTx(ctx, tx, subject, lib.ID, delegated.ScanControl); err == nil {
		t.Fatal("revoked authority passed transactional fence")
	}
}
