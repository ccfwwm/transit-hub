package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestRefreshSessionVerifiesExistingNewAPIAuthWhenRefreshIsUnsupported(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/user/auth/refresh" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				http.Error(w, "refresh unavailable", status)
			}))
			defer server.Close()

			expiredAt := time.Now().Add(-time.Hour).UnixMilli()
			session := Session{
				Platform: PlatformNewAPI, BaseURL: server.URL, UserID: "808",
				AccessToken: "existing-access", Cookie: "refresh=existing-cookie", TokenType: "Bearer", ExpiresAt: &expiredAt,
			}
			refreshed, err := NewPlatformService(NewHTTPClient(server.Client())).RefreshSession(session)
			if status == http.StatusServiceUnavailable {
				if errorKey(err) != ErrorUnavailable {
					t.Fatalf("transient refresh failure must be preserved: %v", err)
				}
				return
			}
			if err != nil || refreshed.AccessToken != session.AccessToken || refreshed.Cookie != session.Cookie || refreshed.ExpiresAt != nil {
				t.Fatalf("existing auth was not preserved for metric verification: session=%+v err=%v", refreshed, err)
			}
		})
	}
}

func TestSyncNewAPIRecoversWithStoredPasswordAfterRefreshForbidden(t *testing.T) {
	refreshCalls, loginCalls, selfCalls := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/auth/refresh":
			refreshCalls++
			http.Error(w, "refresh forbidden", http.StatusForbidden)
		case "/api/user/login":
			loginCalls++
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{
				"id": 808, "access_token": "fresh-token", "expires_in": 3600,
			}})
		case "/api/user/self":
			selfCalls++
			if r.Header.Get("Authorization") != "Bearer fresh-token" {
				http.Error(w, "expired session", http.StatusUnauthorized)
				return
			}
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"id": 808, "quota": 4500000, "used_quota": 0}})
		case "/api/status":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 500000}})
		case "/api/log/self/stat":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota": 0}})
		case "/api/user/self/groups", "/api/pricing":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	expiredAt := time.Now().Add(-time.Hour).UnixMilli()
	cache := newFakeSiteCache()
	cache.add(&Site{
		ID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Name: "newapi-site",
		BaseURL: server.URL, Platform: PlatformNewAPI, RequestedPlatform: PlatformNewAPI,
		Account: "saved-user", Status: StatusConnected, CanRelogin: true,
		Session: &Session{Platform: PlatformNewAPI, BaseURL: server.URL, UserID: "808", AccessToken: "expired-token", Cookie: "refresh=old", ExpiresAt: &expiredAt},
	})
	service := NewService(NewPlatformService(NewHTTPClient(server.Client())), nil, nil, cache)
	service.SetAdminAccountResolver(&fakeAccountResolver{current: map[string]string{"user-1": "admin-1"}})
	service.SetCredentialStore(&fakeCredentialStore{passwords: map[string]StoredSiteCredential{
		"site-1": {SiteID: "site-1", UserID: "user-1", AdminAccountID: "admin-1", Password: "saved-password"},
	}})

	response, err := service.Sync(context.Background(), "user-1", "site-1")
	if err != nil || response.Status != StatusConnected || response.ErrorKey != nil {
		t.Fatalf("sync did not recover: status=%s err=%v errorKey=%v", response.Status, err, response.ErrorKey)
	}
	if refreshCalls != 1 || loginCalls != 1 || selfCalls != 2 || response.Metrics.Balance.Value == nil || *response.Metrics.Balance.Value != 9 {
		t.Fatalf("unexpected recovery: refresh=%d login=%d self=%d balance=%+v", refreshCalls, loginCalls, selfCalls, response.Metrics.Balance)
	}
}
