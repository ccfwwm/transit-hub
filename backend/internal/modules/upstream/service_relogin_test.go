package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeCredentialStore struct {
	passwords map[string]StoredSiteCredential
}

func (f *fakeCredentialStore) SavePassword(ctx context.Context, credential StoredSiteCredential) error {
	if f.passwords == nil {
		f.passwords = map[string]StoredSiteCredential{}
	}
	f.passwords[credential.SiteID] = credential
	return nil
}

func (f *fakeCredentialStore) LoadPassword(ctx context.Context, userID, adminAccountID, siteID string) (StoredSiteCredential, bool, error) {
	credential, ok := f.passwords[siteID]
	if !ok || credential.UserID != userID || credential.AdminAccountID != adminAccountID {
		return StoredSiteCredential{}, false, nil
	}
	return credential, true, nil
}

func (f *fakeCredentialStore) Delete(ctx context.Context, userID, adminAccountID, siteID string) error {
	delete(f.passwords, siteID)
	return nil
}

func (f *fakeCredentialStore) MarkAutomaticReloginAttempt(ctx context.Context, userID, adminAccountID, siteID string, attemptedAtUnixMilli int64) error {
	credential := f.passwords[siteID]
	credential.LastAutomaticReloginAtUnixMilli = attemptedAtUnixMilli
	f.passwords[siteID] = credential
	return nil
}

func TestReloginUsesStoredPasswordAndRefreshesSite(t *testing.T) {
	loginCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginCalls++
			writeJSON(w, map[string]any{"data": map[string]any{
				"access_token": "fresh-token", "refresh_token": "fresh-refresh", "expires_in": 3600,
			}})
		case "/api/v1/auth/me":
			if got := r.Header.Get("Authorization"); got != "Bearer fresh-token" {
				t.Fatalf("auth header = %q, want fresh token", got)
			}
			writeJSON(w, map[string]any{"data": map[string]any{"balance": 12.5, "total_recharged": 20.0}})
		case "/api/v1/usage/dashboard/stats":
			writeJSON(w, map[string]any{"data": map[string]any{"today_actual_cost": 1.5}})
		case "/api/v1/groups/available", "/api/v1/groups/rates":
			writeJSON(w, map[string]any{"data": []any{}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cache := newFakeSiteCache()
	cache.add(&Site{
		ID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Name: "test-site",
		BaseURL: server.URL, Platform: PlatformSub2API, RequestedPlatform: PlatformSub2API,
		Account: "saved@example.com", Status: StatusError,
		Session: &Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "expired-token", TokenType: "Bearer"},
	})
	credentials := &fakeCredentialStore{passwords: map[string]StoredSiteCredential{
		"site-1": {SiteID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Password: "saved-password"},
	}}
	service := NewService(NewPlatformService(NewHTTPClient(http.DefaultClient)), nil, nil, cache)
	service.SetAdminAccountResolver(&fakeAccountResolver{current: map[string]string{"user-1": "admin-1"}})
	service.SetCredentialStore(credentials)

	response, err := service.Relogin(context.Background(), "user-1", "site-1")
	if err != nil {
		t.Fatalf("Relogin returned error: %v", err)
	}
	if loginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", loginCalls)
	}
	if response.Status != StatusConnected || response.ErrorKey != nil {
		t.Fatalf("unexpected relogin response: %+v", response)
	}
	if response.Metrics.Balance.Value == nil || *response.Metrics.Balance.Value != 12.5 {
		t.Fatalf("balance was not refreshed: %+v", response.Metrics.Balance)
	}
	stored, err := cache.Get(context.Background(), "site-1")
	if err != nil || stored == nil || stored.Session == nil || stored.Session.AccessToken != "fresh-token" {
		t.Fatalf("cached session was not replaced: site=%+v err=%v", stored, err)
	}
}

func TestSyncAutomaticallyReloginsOnceAfterAuthFailure(t *testing.T) {
	loginCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginCalls++
			writeJSON(w, map[string]any{"data": map[string]any{"access_token": "fresh-token", "expires_in": 3600}})
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") == "Bearer expired-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			writeJSON(w, map[string]any{"data": map[string]any{"balance": 9.0, "total_recharged": 9.0}})
		case "/api/v1/usage/dashboard/stats":
			writeJSON(w, map[string]any{"data": map[string]any{"today_actual_cost": 0.0}})
		case "/api/v1/groups/available", "/api/v1/groups/rates":
			writeJSON(w, map[string]any{"data": []any{}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cache := newFakeSiteCache()
	cache.add(&Site{
		ID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Name: "test-site",
		BaseURL: server.URL, Platform: PlatformSub2API, RequestedPlatform: PlatformSub2API,
		Account: "saved@example.com", Status: StatusConnected, CanRelogin: true,
		Session: &Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "expired-token", TokenType: "Bearer"},
	})
	credentials := &fakeCredentialStore{passwords: map[string]StoredSiteCredential{
		"site-1": {SiteID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Password: "saved-password"},
	}}
	service := NewService(NewPlatformService(NewHTTPClient(http.DefaultClient)), nil, nil, cache)
	service.SetAdminAccountResolver(&fakeAccountResolver{current: map[string]string{"user-1": "admin-1"}})
	service.SetCredentialStore(credentials)

	response, err := service.Sync(context.Background(), "user-1", "site-1")
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if loginCalls != 1 || response.Status != StatusConnected {
		t.Fatalf("sync did not recover through one relogin: logins=%d response=%+v", loginCalls, response)
	}
}

func TestSyncDoesNotRepeatFailedAutomaticReloginDuringCooldown(t *testing.T) {
	loginCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginCalls++
			w.WriteHeader(http.StatusUnauthorized)
		case "/api/v1/auth/me":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cache := newFakeSiteCache()
	cache.add(&Site{
		ID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Name: "test-site",
		BaseURL: server.URL, Platform: PlatformSub2API, RequestedPlatform: PlatformSub2API,
		Account: "saved@example.com", Status: StatusConnected, CanRelogin: true,
		Session: &Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "expired-token", TokenType: "Bearer"},
	})
	credentials := &fakeCredentialStore{passwords: map[string]StoredSiteCredential{
		"site-1": {SiteID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Password: "wrong-password"},
	}}
	service := NewService(NewPlatformService(NewHTTPClient(http.DefaultClient)), nil, nil, cache)
	service.SetAdminAccountResolver(&fakeAccountResolver{current: map[string]string{"user-1": "admin-1"}})
	service.SetCredentialStore(credentials)

	if _, err := service.Sync(context.Background(), "user-1", "site-1"); err != nil {
		t.Fatalf("first Sync returned error: %v", err)
	}
	if _, err := service.Sync(context.Background(), "user-1", "site-1"); err != nil {
		t.Fatalf("second Sync returned error: %v", err)
	}
	if loginCalls != 1 {
		t.Fatalf("automatic relogin calls = %d, want exactly one during cooldown", loginCalls)
	}
}

func TestReloginRejectsSitesWithoutStoredPassword(t *testing.T) {
	cache := newFakeSiteCache()
	cache.add(&Site{
		ID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Name: "legacy-site",
		Platform: PlatformSub2API, RequestedPlatform: PlatformSub2API, Account: "saved@example.com",
	})
	service := NewService(NewPlatformService(NewHTTPClient(http.DefaultClient)), nil, nil, cache)
	service.SetAdminAccountResolver(&fakeAccountResolver{current: map[string]string{"user-1": "admin-1"}})
	service.SetCredentialStore(&fakeCredentialStore{})

	_, err := service.Relogin(context.Background(), "user-1", "site-1")
	if errorKey(err) != ErrorCredentialsUnavailable {
		t.Fatalf("Relogin error = %v, want %s", err, ErrorCredentialsUnavailable)
	}
}
