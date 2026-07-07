package my_sites

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"transithub/backend/internal/modules/upstream"
)

type realConnectStateRepo struct {
	state *State
}

func (r *realConnectStateRepo) Get(ctx context.Context, userID string, adminAccountID string) (*State, error) {
	return r.state, nil
}

func (r *realConnectStateRepo) Save(ctx context.Context, state State) error {
	r.state = &state
	return nil
}

type realConnectConnRepo struct {
	conn    *RealConnection
	getConn *RealConnection
	deleted bool
}

func (r *realConnectConnRepo) SaveRealConnection(ctx context.Context, conn RealConnection) error {
	r.conn = &conn
	return nil
}

func (r *realConnectConnRepo) ListRealConnections(ctx context.Context, userID string, adminAccountID string) ([]RealConnection, error) {
	return nil, nil
}

func (r *realConnectConnRepo) GetRealConnection(ctx context.Context, id string, userID string, adminAccountID string) (*RealConnection, error) {
	return r.getConn, nil
}

func (r *realConnectConnRepo) DeleteRealConnection(ctx context.Context, id string, userID string, adminAccountID string) error {
	r.deleted = true
	return nil
}

type realConnectLookup struct {
	site *upstream.Site
}

func (l realConnectLookup) GetSite(ctx context.Context, siteID string) (*upstream.Site, error) {
	return l.site, nil
}

type realConnectAccounts struct {
	id string
}

func (a realConnectAccounts) RequireCurrentID(ctx context.Context, userID string) (string, error) {
	return a.id, nil
}

func TestRealConnectNewAPIUpstreamCreatesSub2APIAdminAccountWhenAdminSessionIsSub2API(t *testing.T) {
	var adminAccountCalled bool
	var adminAccountPayload map[string]any
	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me":
			if got := r.Header.Get("Authorization"); got != "Bearer admin-token" {
				t.Fatalf("unexpected admin auth header %q", got)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"role": "admin"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/accounts":
			adminAccountCalled = true
			if got := r.Header.Get("Authorization"); got != "Bearer admin-token" {
				t.Fatalf("unexpected admin create auth header %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&adminAccountPayload); err != nil {
				t.Fatalf("decode admin account payload: %v", err)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": 1281}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/groups/all":
			writeJSON(t, w, map[string]any{"data": []any{map[string]any{
				"id":              101,
				"name":            "codex-极速福利",
				"platform":        "openai",
				"status":          "active",
				"rate_multiplier": 0.03,
			}}})
		default:
			t.Fatalf("unexpected admin request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer adminServer.Close()

	var createdTokenName string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode token payload: %v", err)
			}
			createdTokenName = payload["name"].(string)
			if payload["group"] != "temporary" {
				t.Fatalf("unexpected token group %v", payload["group"])
			}
			writeJSON(t, w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/":
			writeJSON(t, w, map[string]any{
				"data":  []any{map[string]any{"id": 3099, "name": createdTokenName}},
				"total": 1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/3099/key":
			writeJSON(t, w, map[string]any{"data": map[string]any{"key": "sk-upstream-token"}})
		default:
			t.Fatalf("unexpected upstream request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstreamServer.Close()

	groupPlatform := "openai"
	stateRepo := &realConnectStateRepo{state: &State{
		UserID:         "user-1",
		AdminAccountID: "workspace-1",
		Session: upstream.Session{
			Platform:    upstream.PlatformSub2API,
			BaseURL:     adminServer.URL,
			AccessToken: "admin-token",
			TokenType:   "Bearer",
		},
	}}
	connRepo := &realConnectConnRepo{}
	service := NewService(stateRepo, upstream.NewPlatformService(upstream.NewHTTPClient(adminServer.Client())), realConnectLookup{site: &upstream.Site{
		ID:             "site-newapi",
		UserID:         "user-1",
		AdminAccountID: "workspace-1",
		Name:           "ai.zyyun.xyz",
		BaseURL:        upstreamServer.URL,
		Platform:       upstream.PlatformNewAPI,
		Session: &upstream.Session{
			Platform: upstream.PlatformNewAPI,
			BaseURL:  upstreamServer.URL,
			Cookie:   "session=upstream",
			UserID:   "9",
		},
		Metrics: upstream.Metrics{Groups: []upstream.GroupInfo{{
			ID:                "temporary",
			Name:              "临时GPT低价分组",
			Platform:          &groupPlatform,
			MultiplierDisplay: "0.001x",
		}}},
	}})
	service.connRepository = connRepo
	service.SetAdminAccountResolver(realConnectAccounts{id: "workspace-1"})

	response, err := service.RealConnect(context.Background(), "user-1", RealConnectRequest{
		UpstreamSiteID:    "site-newapi",
		UpstreamGroupID:   "temporary",
		UpstreamGroupName: "临时GPT低价分组",
		OwnGroupIDs:       []string{"101"},
		GroupType:         "openai",
	})
	if err != nil {
		t.Fatalf("RealConnect returned error: %v", err)
	}
	if !adminAccountCalled {
		t.Fatal("expected Sub2API admin account to be created")
	}
	if response.Connection.AdminAccountID != "1281" {
		t.Fatalf("AdminAccountID = %q, want 1281", response.Connection.AdminAccountID)
	}
	if response.Connection.UpstreamKeyID != "3099" {
		t.Fatalf("UpstreamKeyID = %q, want 3099", response.Connection.UpstreamKeyID)
	}
	if response.Connection.UpstreamKey != "sk-upstream-token" {
		t.Fatalf("UpstreamKey = %q, want sk-upstream-token", response.Connection.UpstreamKey)
	}
	credentials, _ := adminAccountPayload["credentials"].(map[string]any)
	if credentials["base_url"] != upstreamServer.URL {
		t.Fatalf("credentials.base_url = %v, want %s", credentials["base_url"], upstreamServer.URL)
	}
	if credentials["api_key"] != "sk-upstream-token" {
		t.Fatalf("credentials.api_key = %v, want upstream token", credentials["api_key"])
	}
	groupIDs, ok := adminAccountPayload["group_ids"].([]any)
	if !ok || len(groupIDs) != 1 || groupIDs[0].(float64) != 101 {
		t.Fatalf("group_ids = %#v, want [101]", adminAccountPayload["group_ids"])
	}
	if platform := strings.TrimSpace(adminAccountPayload["platform"].(string)); platform != "openai" {
		t.Fatalf("platform = %q, want openai", platform)
	}
	if connRepo.conn == nil {
		t.Fatal("expected real connection to be saved")
	}
}

func TestRealDisconnectNewAPIUpstreamDeletesSub2APIAdminAccountAndNewAPIToken(t *testing.T) {
	var adminAccountDeleted bool
	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me":
			writeJSON(t, w, map[string]any{"data": map[string]any{"role": "admin"}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/admin/accounts/1281":
			adminAccountDeleted = true
			if got := r.Header.Get("Authorization"); got != "Bearer admin-token" {
				t.Fatalf("unexpected admin delete auth header %q", got)
			}
			writeJSON(t, w, map[string]any{"success": true})
		default:
			t.Fatalf("unexpected admin request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer adminServer.Close()

	var upstreamTokenDeleted bool
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/token/3099":
			upstreamTokenDeleted = true
			writeJSON(t, w, map[string]any{"success": true})
		default:
			t.Fatalf("unexpected upstream request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstreamServer.Close()

	stateRepo := &realConnectStateRepo{state: &State{
		UserID:         "user-1",
		AdminAccountID: "workspace-1",
		Session: upstream.Session{
			Platform:    upstream.PlatformSub2API,
			BaseURL:     adminServer.URL,
			AccessToken: "admin-token",
			TokenType:   "Bearer",
		},
		Mappings: []GroupMapping{{
			OwnGroup: "codex-极速福利",
			UpstreamTargets: []UpstreamGroupRef{{
				SiteID:    "site-newapi",
				GroupName: "临时GPT低价分组",
			}},
		}},
	}}
	connRepo := &realConnectConnRepo{getConn: &RealConnection{
		ID:                      "conn-1",
		UserID:                  "user-1",
		WorkspaceAdminAccountID: "workspace-1",
		UpstreamSiteID:          "site-newapi",
		UpstreamGroupID:         "temporary",
		UpstreamGroupName:       "临时GPT低价分组",
		UpstreamKeyID:           "3099",
		AdminAccountID:          "1281",
		OwnGroupIDs:             []string{"101"},
		GroupType:               "openai",
	}}
	service := NewService(stateRepo, upstream.NewPlatformService(upstream.NewHTTPClient(adminServer.Client())), realConnectLookup{site: &upstream.Site{
		ID:             "site-newapi",
		UserID:         "user-1",
		AdminAccountID: "workspace-1",
		Name:           "ai.zyyun.xyz",
		BaseURL:        upstreamServer.URL,
		Platform:       upstream.PlatformNewAPI,
		Session: &upstream.Session{
			Platform: upstream.PlatformNewAPI,
			BaseURL:  upstreamServer.URL,
			Cookie:   "session=upstream",
			UserID:   "9",
		},
	}})
	service.connRepository = connRepo
	service.SetAdminAccountResolver(realConnectAccounts{id: "workspace-1"})

	err := service.RealDisconnect(context.Background(), "user-1", RealDisconnectRequest{
		ConnectionID: "conn-1",
		Mode:         "full",
	})
	if err != nil {
		t.Fatalf("RealDisconnect returned error: %v", err)
	}
	if !adminAccountDeleted {
		t.Fatal("expected Sub2API admin account to be deleted")
	}
	if !upstreamTokenDeleted {
		t.Fatal("expected New-API upstream token to be deleted")
	}
	if !connRepo.deleted {
		t.Fatal("expected local connection record to be deleted")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("write json: %v", err)
	}
}
