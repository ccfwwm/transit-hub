package my_sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"transithub/backend/internal/modules/upstream"
)

type mappingOptionsAdminSessionProvider struct {
	session  upstream.Session
	identity string
	ok       bool
}

func (p mappingOptionsAdminSessionProvider) CurrentAdminSession(ctx context.Context, userID string, adminAccountID string) (upstream.Session, string, bool, error) {
	return p.session, p.identity, p.ok, nil
}

func TestMappingOptionsRestoresExpiredMySiteSessionFromDashboardSession(t *testing.T) {
	expired := time.Now().Add(-time.Hour).UnixMilli()
	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/refresh":
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(t, w, map[string]any{"error": "invalid refresh token"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me" && auth == "Bearer stale-token":
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(t, w, map[string]any{"error": "expired access token"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me" && auth == "Bearer fresh-token":
			writeJSON(t, w, map[string]any{"data": map[string]any{"role": "admin"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/groups/all" && auth == "Bearer fresh-token":
			writeJSON(t, w, map[string]any{"data": []map[string]any{
				{"id": 101, "name": "codex-极速福利", "platform": "openai", "status": "active", "rate_multiplier": 0.03},
			}})
		default:
			t.Fatalf("unexpected admin request %s %s auth=%q", r.Method, r.URL.Path, auth)
		}
	}))
	defer adminServer.Close()

	repo := &realConnectStateRepo{state: &State{
		UserID:         "user-1",
		AdminAccountID: "account-1",
		BaseURL:        adminServer.URL,
		Email:          "old@example.com",
		Session: upstream.Session{
			Platform:     upstream.PlatformSub2API,
			BaseURL:      adminServer.URL,
			AccessToken:  "stale-token",
			RefreshToken: "bad-refresh-token",
			TokenType:    "Bearer",
			ExpiresAt:    &expired,
		},
		Mappings: []GroupMapping{},
	}}
	service := NewService(repo, upstream.NewPlatformService(upstream.NewHTTPClient(adminServer.Client())), nil)
	service.SetAdminAccountResolver(realConnectAccounts{id: "account-1"})
	service.SetAdminSessionProvider(mappingOptionsAdminSessionProvider{
		session: upstream.Session{
			Platform:    upstream.PlatformSub2API,
			BaseURL:     adminServer.URL,
			AccessToken: "fresh-token",
			TokenType:   "Bearer",
		},
		identity: "fresh@example.com",
		ok:       true,
	})

	response, err := service.MappingOptions(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("expected mapping options to recover from dashboard session, got err: %v", err)
	}
	if len(response.OwnGroups) != 1 || response.OwnGroups[0].GroupName != "codex-极速福利" {
		t.Fatalf("unexpected own groups: %+v", response.OwnGroups)
	}
	if repo.state == nil || repo.state.Session.AccessToken != "fresh-token" || repo.state.Email != "fresh@example.com" {
		t.Fatalf("expected my_site state to be restored from dashboard session, got %+v", repo.state)
	}
}
