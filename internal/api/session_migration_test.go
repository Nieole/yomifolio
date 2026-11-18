package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yomifolio/internal/database"
)

func migrationCookies(t *testing.T, cookies []*http.Cookie, token string) {
	t.Helper()
	modern := findSessionCookie(cookies)
	if modern == nil || modern.Value != token || modern.MaxAge <= 0 || !modern.HttpOnly || modern.Path != "/" || modern.SameSite != http.SameSiteLaxMode {
		t.Fatalf("invalid migrated cookie: %+v", modern)
	}
	requireCookieCleared(t, cookies, legacySessionCookieName)
}

func requireCookieCleared(t *testing.T, cookies []*http.Cookie, name string) {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.MaxAge < 0 && cookie.Value == "" && cookie.Path == "/" {
			return
		}
	}
	t.Fatalf("cookie %s not cleared: %+v", name, cookies)
}

func TestLegacySessionMigration(t *testing.T) {
	for _, status := range []bool{false, true} {
		t.Run(map[bool]string{false: "gate", true: "status"}[status], func(t *testing.T) {
			c, store, login := newSlidingSessionFixture(t, "always")
			before := sessionExpiresAt(t, store, login.Value)
			legacy := &http.Cookie{Name: legacySessionCookieName, Value: login.Value}
			req := authedRequest(http.MethodGet, "/api/auth/me", legacy)
			var resp *http.Response
			if status {
				rec := httptest.NewRecorder()
				c.admin.authStatus(rec, req)
				var body struct {
					Authenticated bool `json:"authenticated"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.Authenticated {
					t.Fatalf("status: %s, %v", rec.Body.String(), err)
				}
				resp = rec.Result()
			} else {
				resp = serveThroughGate(c, http.HandlerFunc(c.admin.authMe), req)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			migrationCookies(t, resp.Cookies(), login.Value)
			if !findSessionCookie(resp.Cookies()).Secure {
				t.Fatal("migration lost Secure")
			}
			if got := sessionExpiresAt(t, store, login.Value); !got.Equal(before) {
				t.Fatalf("migration extended session: %v -> %v", before, got)
			}
		})
	}
}

func TestSessionCookiePriorityFailsClosed(t *testing.T) {
	c, store, login := newSlidingSessionFixture(t, "")
	ctx := context.Background()
	user, err := store.CreateUser(ctx, database.CreateUserParams{Username: "bob", PasswordHash: "unused", Role: database.RoleRegular})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, database.Session{ID: hashSessionID("bob-token"), UserID: user.ID, CSRFToken: "bob-csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	expired := "expired-token"
	if err := store.CreateSession(ctx, database.Session{ID: hashSessionID(expired), UserID: user.ID, CSRFToken: "expired-csrf", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		cookies []*http.Cookie
		want    int
	}{
		{"new identity wins", []*http.Cookie{{Name: legacySessionCookieName, Value: login.Value}, {Name: sessionCookieName, Value: "bob-token"}}, http.StatusOK},
		{"invalid new", []*http.Cookie{{Name: sessionCookieName, Value: "invalid"}, {Name: legacySessionCookieName, Value: login.Value}}, http.StatusUnauthorized},
		{"empty new", []*http.Cookie{{Name: sessionCookieName}, {Name: legacySessionCookieName, Value: login.Value}}, http.StatusUnauthorized},
		{"expired new", []*http.Cookie{{Name: sessionCookieName, Value: expired}, {Name: legacySessionCookieName, Value: login.Value}}, http.StatusUnauthorized},
		{"duplicate new", []*http.Cookie{{Name: sessionCookieName, Value: "bob-token"}, {Name: sessionCookieName, Value: login.Value}, {Name: legacySessionCookieName, Value: login.Value}}, http.StatusUnauthorized},
		{"duplicate legacy", []*http.Cookie{{Name: legacySessionCookieName, Value: login.Value}, {Name: legacySessionCookieName, Value: "bob-token"}}, http.StatusUnauthorized},
		{"invalid legacy", []*http.Cookie{{Name: legacySessionCookieName, Value: "invalid"}}, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
			for _, cookie := range tc.cookies {
				req.AddCookie(cookie)
			}
			resp := serveThroughGate(c, http.HandlerFunc(c.admin.authMe), req)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			if tc.name == "invalid new" || tc.name == "expired new" || tc.name == "invalid legacy" {
				requireCookieCleared(t, resp.Cookies(), sessionCookieName)
				requireCookieCleared(t, resp.Cookies(), legacySessionCookieName)
			}
			if tc.want == http.StatusOK {
				var body authSessionResponse
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.User.ID != user.ID || body.CSRFToken != "bob-csrf" {
					t.Fatalf("wrong identity: %+v, %v", body, err)
				}
			}
			rec := httptest.NewRecorder()
			c.admin.authStatus(rec, req)
			var body struct {
				Authenticated bool `json:"authenticated"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Authenticated != (tc.want == http.StatusOK) {
				t.Fatalf("status disagrees: %s, %v", rec.Body.String(), err)
			}
		})
	}
}

func TestSessionMigrationNewCookieCannotUseLegacyPrivileges(t *testing.T) {
	c, store, login := newSlidingSessionFixture(t, "")
	ctx := context.Background()
	user, err := store.CreateUser(ctx, database.CreateUserParams{Username: "bob", PasswordHash: "unused", Role: database.RoleRegular})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, database.Session{ID: hashSessionID("bob-token"), UserID: user.ID, CSRFToken: "bob-csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	adminSession, _, err := store.GetSessionWithUser(ctx, hashSessionID(login.Value), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, csrf := range []string{adminSession.CSRFToken, "bob-csrf"} {
		req := httptest.NewRequest(http.MethodPost, "/api/users", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "bob-token"})
		req.AddCookie(&http.Cookie{Name: legacySessionCookieName, Value: login.Value})
		req.Header.Set("X-CSRF-Token", csrf)
		resp := serveThroughGate(c, okHandler(), req)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("legacy privileges leaked: %d", resp.StatusCode)
		}
	}
	if err := store.DeleteSession(ctx, hashSessionID("bob-token")); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "bob-token"})
	req.AddCookie(&http.Cookie{Name: legacySessionCookieName, Value: login.Value})
	if resp := serveThroughGate(c, okHandler(), req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked new session fell back: %d", resp.StatusCode)
	}
}

func TestLegacySessionCSRFAndTouchFailure(t *testing.T) {
	c, store, login := newSlidingSessionFixture(t, "")
	legacy := &http.Cookie{Name: legacySessionCookieName, Value: login.Value}
	ageSession(t, store, login.Value, time.Hour)
	resp := serveThroughGate(c, okHandler(), authedRequest(http.MethodPost, "/api/users", legacy))
	if resp.StatusCode != http.StatusForbidden || len(resp.Cookies()) != 0 {
		t.Fatalf("CSRF denial migrated cookie: %d, %+v", resp.StatusCode, resp.Cookies())
	}
	failing := &touchFailingStore{Store: store}
	c.admin.store = failing
	resp = serveThroughGate(c, okHandler(), authedRequest(http.MethodGet, "/api/auth/me", legacy))
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 0 || failing.calls != 1 {
		t.Fatalf("failed touch must preserve legacy cookie: %d, %+v", resp.StatusCode, resp.Cookies())
	}
	c.admin.store = store
	resp = serveThroughGate(c, okHandler(), authedRequest(http.MethodGet, "/api/auth/me", legacy))
	migrationCookies(t, resp.Cookies(), login.Value)
}

func TestSessionMigrationLogoutClearsAndRevokesBoth(t *testing.T) {
	c, store, login := newSlidingSessionFixture(t, "")
	sess, _, err := store.GetSessionWithUser(context.Background(), hashSessionID(login.Value), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	legacyToken := "other-legacy-session"
	if err := store.CreateSession(context.Background(), database.Session{ID: hashSessionID(legacyToken), UserID: sess.UserID, CSRFToken: "legacy-csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	req := authedRequest(http.MethodPost, "/api/auth/logout", login)
	req.AddCookie(&http.Cookie{Name: legacySessionCookieName, Value: legacyToken})
	req.Header.Set("X-CSRF-Token", sess.CSRFToken)
	resp := serveThroughGate(c, http.HandlerFunc(c.admin.logout), req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	requireCookieCleared(t, resp.Cookies(), sessionCookieName)
	requireCookieCleared(t, resp.Cookies(), legacySessionCookieName)
	for _, token := range []string{login.Value, legacyToken} {
		if _, _, err := store.GetSessionWithUser(context.Background(), hashSessionID(token), time.Now()); err == nil {
			t.Fatalf("logout left token valid: %s", token)
		}
	}
}
