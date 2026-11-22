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

type monitorConnRepo struct{ realConnectConnRepo }

func (r *monitorConnRepo) SaveMonitorBinding(_ context.Context, v RealConnection) error {
	for i := range r.connections {
		if r.connections[i].ID == v.ID {
			r.connections[i] = v
			return nil
		}
	}
	r.connections = append(r.connections, v)
	return nil
}

type monitorLookup struct{ sites map[string]*upstream.Site }

func (l monitorLookup) GetSite(_ context.Context, id string) (*upstream.Site, error) {
	return l.sites[id], nil
}
func (l monitorLookup) MonitorSites(context.Context, string, string) (map[string]*upstream.Site, error) {
	return l.sites, nil
}

func TestSyncExistingForwardingKeyFindsGroupAndReusesBinding(t *testing.T) {
	for _, mode := range []string{"import", "attach-detached", "known-alias", "manual", "explicit-group", "wrong-key", "wrong-url", "foreign-site", "wrong-workspace", "background-workspace"} {
		t.Run(mode, func(t *testing.T) {
			var base string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != "GET" {
					t.Errorf("import created or modified an upstream resource: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
					return
				}
				switch r.URL.Path {
				case "/api/v1/auth/me":
					w.Write([]byte(`{"data":{"role":"admin"}}`))
				case "/api/v1/admin/accounts":
					accountBase := base + "/supplier/v1"
					if mode == "wrong-url" || mode == "known-alias" {
						accountBase = base + "/different"
					}
					key := "existing-key"
					if mode == "wrong-key" || mode == "explicit-group" {
						key = "different-key"
					}
					json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": []any{map[string]any{"id": 9, "type": "apikey", "platform": "openai", "credentials": map[string]any{"base_url": accountBase, "api_key": key}, "group_ids": []int{1}}}, "total": 1}})
				case "/supplier/api/v1/keys":
					w.Write([]byte(`{"data":{"items":[{"id":77,"key":"existing-key","group_id":7,"group":{"name":"pro"}}],"total":1}}`))
				default:
					t.Errorf("unexpected import endpoint: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			base = server.URL
			platform := "openai"
			site := &upstream.Site{ID: "s", UserID: "u", AdminAccountID: "w", Name: "supplier", BaseURL: base + "/supplier", Platform: upstream.PlatformSub2API, Session: &upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: base + "/supplier", AccessToken: "login"}, Metrics: upstream.Metrics{Groups: []upstream.GroupInfo{{ID: "7", Name: "pro", Platform: &platform}}}}
			if mode == "foreign-site" {
				site.AdminAccountID = "other"
			}
			stateRepo := &realConnectStateRepo{state: &State{UserID: "u", AdminAccountID: "w", Session: upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: base, AccessToken: "admin"}, OwnGroups: []GroupOption{{ID: "1", Name: "own", Platform: "openai"}}}}
			repo := &monitorConnRepo{}
			if mode == "attach-detached" {
				repo.connections = []RealConnection{{ID: "detached", UpstreamSiteID: "s", UpstreamKey: "existing-key", CreatedAt: "2026-11-20T00:00:00Z"}}
			}
			if mode == "known-alias" {
				repo.connections = []RealConnection{{ID: "alias", AdminAccountID: "9", UpstreamSiteID: "s", UpstreamKey: "existing-key", CreatedAt: "2026-11-20T00:00:00Z"}}
			}
			service := NewService(stateRepo, upstream.NewPlatformService(upstream.NewHTTPClient(server.Client())), monitorLookup{map[string]*upstream.Site{"s": site}})
			service.connRepository = repo
			service.SetAdminAccountResolver(realConnectAccounts{id: "w"})
			if mode == "background-workspace" {
				service.SetAdminAccountResolver(realConnectAccounts{id: "different-selected-workspace"})
			}
			req := MonitorBindingRequest{}
			if mode == "manual" {
				req.APIURL = base + "/supplier/v1"
				req.APIKey = "existing-key"
			}
			if mode == "explicit-group" {
				req.APIURL = base + "/supplier/v1"
				req.APIKey = "different-key"
				req.SiteID = "s"
				req.GroupID = "7"
			}
			aid := "w"
			if mode == "wrong-workspace" {
				aid = "other"
			}
			first, e := service.syncMonitorBindings(context.Background(), "u", aid, req, mode != "background-workspace")
			if mode == "wrong-workspace" {
				if e == nil {
					t.Fatal("cross-workspace sync accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if mode == "wrong-key" || mode == "wrong-url" || mode == "foreign-site" {
				if first.Matched != 0 || len(repo.connections) != 0 {
					t.Fatal("incorrect Key/site was bound")
				}
				return
			}
			if first.Matched != 1 || first.Items[0].NeedsTakeover != (mode != "known-alias") || len(repo.connections) != 1 {
				t.Fatal("external Key import failed", first)
			}
			conn := repo.connections[0]
			if upstream.MonitorAPIBase(conn.UpstreamAPIURL) == "" {
				t.Fatal("actual forwarding API address was not retained")
			}
			if mode == "attach-detached" && conn.ID != "detached" {
				t.Fatal("detached binding was duplicated instead of attached")
			}
			if mode != "explicit-group" && (conn.UpstreamKey != "existing-key" || conn.UpstreamKeyID != "77") || conn.UpstreamGroupID != "7" || conn.AdminAccountID != "9" || len(conn.OwnGroupIDs) != 1 {
				t.Fatal("existing forwarding scope changed")
			}
			second, e := service.syncMonitorBindings(context.Background(), "u", "w", req, mode != "background-workspace")
			if e != nil || second.Matched != 1 || second.Items[0].NeedsTakeover || len(repo.connections) != 1 {
				t.Fatal("repeated sync duplicated or retook the binding", e)
			}
			b, _ := json.Marshal(first)
			if strings.Contains(string(b), "existing-key") || strings.Contains(string(b), "login") {
				t.Fatal("secret exposed by synchronization result")
			}
			if len(stateRepo.state.Mappings) != 1 || stateRepo.state.Mappings[0].UpstreamTargets[0].GroupName != "pro" {
				t.Fatal("own group mapping not synchronized")
			}
		})
	}
}
