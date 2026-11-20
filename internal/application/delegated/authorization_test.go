package delegated_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
)

// The public checkpoint port is exercised with real SQLite, without inventing
// write/paid tools merely to test the authority reserved for later use cases.
func TestCurrentAuthorityIntersectsEveryIndependentCapability(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "authority.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner, err := store.CreateUser(ctx, database.CreateUserParams{Username: "owner", Role: database.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.CreateUser(ctx, database.CreateUserParams{Username: "reader", Role: database.RoleRegular})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Allowed", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	service := delegated.New(delegated.NewSQLStore(store.DB()))
	caps := []string{delegated.Read, delegated.MetadataWrite, delegated.ScanControl, delegated.InternalLLM, delegated.Cleanup}
	for _, user := range []database.User{owner, reader} {
		for _, granted := range caps {
			credential, err := service.Issue(ctx, owner.ID, delegated.Restrictions{UserID: user.ID, Name: "Independent", LibraryIDs: []int64{lib.ID}, Capabilities: []string{granted}})
			if err != nil {
				t.Fatal(err)
			}
			subject, err := service.Authenticate(ctx, credential.Token)
			if err != nil {
				t.Fatal(err)
			}
			for _, requested := range caps {
				err := service.Authorize(ctx, subject, lib.ID, requested)
				allowed := requested == granted && (user.IsAdmin() || requested == delegated.Read)
				if allowed && err != nil || !allowed && !errors.Is(err, delegated.ErrDenied) {
					t.Fatalf("role=%s grant=%s request=%s error=%v", user.Role, granted, requested, err)
				}
				if err := service.Authorize(ctx, subject, lib.ID+999, requested); !errors.Is(err, delegated.ErrDenied) {
					t.Fatalf("library scope escaped: %v", err)
				}
			}
		}
	}
	credential, err := service.Issue(ctx, owner.ID, delegated.Restrictions{UserID: owner.ID, Name: "Current role", LibraryIDs: []int64{lib.ID}, Capabilities: caps})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := service.Authenticate(ctx, credential.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateUserProfile(ctx, owner.ID, "", database.RoleRegular); err != nil {
		t.Fatal(err)
	}
	for _, capability := range caps[1:] {
		if err := service.Authorize(ctx, subject, lib.ID, capability); !errors.Is(err, delegated.ErrDenied) {
			t.Fatalf("demotion retained %s: %v", capability, err)
		}
	}
	if err := service.Authorize(ctx, subject, lib.ID, delegated.Read); err != nil {
		t.Fatalf("demotion changed read permission: %v", err)
	}
	if err := store.DeleteUser(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Authorize(ctx, subject, lib.ID, delegated.Read); !errors.Is(err, delegated.ErrDenied) {
		t.Fatalf("deleted user retained authority: %v", err)
	}
}
