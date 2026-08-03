package upstream

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginNewAPISupportsAccessTokenResponse(t *testing.T) {
	const accessToken = "new-api-access-token"
	const userID = "808"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			w.Header().Add("Set-Cookie", "new_api_refresh=refresh-token; Path=/api/user/auth; HttpOnly")
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{
				"access_token":      accessToken,
				"token_type":        "Bearer",
				"access_expires_at": 1893456000,
				"user":              map[string]any{"id": 808, "quota": 500000},
			}})
		case "/api/status":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 500000}})
		case "/api/user/self":
			assertNewAPIAccessTokenHeaders(t, r, accessToken, userID)
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"id": 808, "quota": 500000, "used_quota": 0}})
		case "/api/log/self/stat":
			assertNewAPIAccessTokenHeaders(t, r, accessToken, userID)
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota": 0}})
		case "/api/user/self/groups":
			assertNewAPIAccessTokenHeaders(t, r, accessToken, userID)
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{}})
		case "/api/pricing":
			assertNewAPIAccessTokenHeaders(t, r, accessToken, userID)
			writeJSON(w, map[string]any{"success": true, "data": []any{}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	result, err := service.Login(server.URL, PlatformNewAPI, "user@example.com", "password")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if result.Session.AccessToken != accessToken || result.Session.TokenType != "Bearer" {
		t.Fatalf("unexpected access token session: %+v", result.Session)
	}
	if result.Session.UserID != userID {
		t.Fatalf("UserID = %q, want %q", result.Session.UserID, userID)
	}
	if result.Session.Cookie != "new_api_refresh=refresh-token" {
		t.Fatalf("Cookie = %q, want refresh cookie", result.Session.Cookie)
	}
	if result.Session.ExpiresAt == nil || *result.Session.ExpiresAt != 1893456000000 {
		t.Fatalf("ExpiresAt = %v, want unix milliseconds", result.Session.ExpiresAt)
	}
}

func TestRefreshSessionRefreshesExpiredNewAPIAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/user/auth/refresh" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Cookie"); got != "new_api_refresh=old-refresh" {
			t.Fatalf("Cookie = %q, want refresh cookie", got)
		}
		w.Header().Add("Set-Cookie", "new_api_refresh=new-refresh; Path=/api/user/auth; HttpOnly")
		writeJSON(w, map[string]any{"success": true, "data": map[string]any{
			"access_token":      "fresh-access-token",
			"token_type":        "Bearer",
			"access_expires_at": 1893456000,
		}})
	}))
	defer server.Close()

	expiredAt := int64(1)
	service := NewPlatformService(NewHTTPClient(server.Client()))
	refreshed, err := service.RefreshSession(Session{
		Platform: PlatformNewAPI, BaseURL: server.URL, Cookie: "new_api_refresh=old-refresh",
		UserID: "808", AccessToken: "expired-access-token", TokenType: "Bearer", ExpiresAt: &expiredAt,
	})
	if err != nil {
		t.Fatalf("RefreshSession returned error: %v", err)
	}
	if refreshed.AccessToken != "fresh-access-token" || refreshed.Cookie != "new_api_refresh=new-refresh" {
		t.Fatalf("unexpected refreshed session: %+v", refreshed)
	}
	if refreshed.UserID != "808" {
		t.Fatalf("UserID = %q, want inherited user ID", refreshed.UserID)
	}
}

func TestNewAPIAccessTokenSessionIsAuthenticated(t *testing.T) {
	session := Session{Platform: PlatformNewAPI, UserID: "808", AccessToken: "access-token", TokenType: "Bearer"}
	if !session.IsAuthenticated() {
		t.Fatal("expected New-API access token session to be authenticated")
	}
}

func TestLoginWithTokenSupportsNewAPISystemAccessToken(t *testing.T) {
	const accessToken = "new-api-system-access-token"
	const userID = "935"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertNewAPIAccessTokenHeaders(t, r, accessToken, userID)
		switch r.URL.Path {
		case "/api/status":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 500000}})
		case "/api/user/self":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"id": 935, "quota": 500000, "used_quota": 0}})
		case "/api/log/self/stat":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota": 0}})
		case "/api/user/self/groups":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{}})
		case "/api/pricing":
			writeJSON(w, map[string]any{"success": true, "data": []any{}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	result, err := service.LoginWithToken(server.URL, PlatformNewAPI, userID, accessToken, "", "Bearer")
	if err != nil {
		t.Fatalf("LoginWithToken returned error: %v", err)
	}
	if result.Platform != PlatformNewAPI || result.Session.UserID != userID || result.Session.AccessToken != accessToken {
		t.Fatalf("unexpected New-API token login result: %+v", result)
	}
}

func TestLoginWithTokenAcceptsBearerPrefixedNewAPISystemAccessToken(t *testing.T) {
	const accessToken = "new-api-system-access-token"
	const userID = "935"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertNewAPIAccessTokenHeaders(t, r, accessToken, userID)
		switch r.URL.Path {
		case "/api/status":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 500000}})
		case "/api/user/self":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"id": 935, "quota": 500000, "used_quota": 0}})
		case "/api/log/self/stat":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota": 0}})
		case "/api/user/self/groups":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{}})
		case "/api/pricing":
			writeJSON(w, map[string]any{"success": true, "data": []any{}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	result, err := service.LoginWithToken(server.URL, PlatformNewAPI, userID, "Bearer "+accessToken, "", "Bearer")
	if err != nil {
		t.Fatalf("LoginWithToken returned error for a Bearer-prefixed token: %v", err)
	}
	if result.Session.AccessToken != accessToken || result.Session.TokenType != "Bearer" {
		t.Fatalf("unexpected normalized token session: %+v", result.Session)
	}
}

func TestNewAPIAccessTokenSessionSupportsGroupStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertNewAPIAccessTokenHeaders(t, r, "access-token", "808")
		writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota": 250000}})
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	stats, err := service.FetchNewAPIGroupDailyStats(Session{
		Platform: PlatformNewAPI, BaseURL: server.URL, UserID: "808",
		AccessToken: "access-token", TokenType: "Bearer", QuotaPerUnit: 500000,
	}, []GroupInfo{{Name: "default"}})
	if err != nil {
		t.Fatalf("FetchNewAPIGroupDailyStats returned error: %v", err)
	}
	if len(stats) != 1 || stats[0].TodayActualCost != 0.5 {
		t.Fatalf("unexpected group stats: %+v", stats)
	}
}

func TestNewAPIAccessTokenSessionSupportsTokenCreation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertNewAPIAccessTokenHeaders(t, r, "access-token", "808")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/":
			writeJSON(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/":
			writeJSON(w, map[string]any{"success": true, "data": []any{map[string]any{"id": 123, "name": "created-token"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/123/key":
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"key": "sk-created"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	id, key, err := service.CreateNewAPIToken(Session{
		Platform: PlatformNewAPI, BaseURL: server.URL, UserID: "808",
		AccessToken: "access-token", TokenType: "Bearer",
	}, "created-token", "default")
	if err != nil {
		t.Fatalf("CreateNewAPIToken returned error: %v", err)
	}
	if id != "123" || key != "sk-created" {
		t.Fatalf("unexpected token result: id=%q key=%q", id, key)
	}
}

func assertNewAPIAccessTokenHeaders(t *testing.T, r *http.Request, accessToken, userID string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+accessToken {
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
	if got := r.Header.Get("New-Api-User"); got != userID {
		t.Fatalf("New-Api-User = %q, want %q", got, userID)
	}
}
