package my_sites

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"transithub/backend/internal/modules/upstream"
)

type repairConnRepo struct {
	realConnectConnRepo
	fail   bool
	writes int
}

func (r *repairConnRepo) UpdateRealConnectionAccount(_ context.Context, conn RealConnection, id string) error {
	if r.fail {
		return errors.New("write failed")
	}
	r.writes++
	r.getConn.AdminAccountID = id
	return nil
}

func TestRepairMissingConnectionAccount(t *testing.T) {
	for _, mode := range []string{"success", "existing", "list-error", "stale-group", "save-error", "foreign-site"} {
		t.Run(mode, func(t *testing.T) {
			actions := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/auth/me":
					writeJSON(t, w, map[string]any{"data": map[string]any{"role": "admin"}})
				case r.Method == "GET" && r.URL.Path == "/api/v1/admin/accounts":
					if mode == "list-error" {
						w.WriteHeader(503)
						return
					}
					items := []map[string]any{}
					if mode == "existing" {
						items = append(items, map[string]any{"id": 9, "schedulable": false})
					}
					writeJSON(t, w, map[string]any{"data": map[string]any{"items": items, "total": len(items)}})
				case r.URL.Path == "/api/v1/admin/groups/all":
					writeJSON(t, w, map[string]any{"data": []map[string]any{{"id": 1, "name": "PLUS", "platform": "openai"}}})
				case r.Method == "POST" && r.URL.Path == "/api/v1/admin/accounts":
					actions = append(actions, "create")
					var p map[string]any
					if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
						t.Fatal(err)
					}
					if p["expires_at"] != float64(1) || p["auto_pause_on_expired"] != true || p["credentials"].(map[string]any)["api_key"] != "existing-key" || len(p["group_ids"].([]any)) != 1 {
						t.Fatalf("unsafe repair payload: expiry/groups/key changed")
					}
					writeJSON(t, w, map[string]any{"data": map[string]any{"id": 10}})
				case r.URL.Path == "/api/v1/admin/accounts/10/schedulable":
					actions = append(actions, "disable")
					var p map[string]any
					json.NewDecoder(r.Body).Decode(&p)
					if p["schedulable"] != false {
						t.Fatal("repair enabled account")
					}
					writeJSON(t, w, map[string]any{"data": true})
				case r.Method == "PUT" && r.URL.Path == "/api/v1/admin/accounts/10":
					actions = append(actions, "clear-expiry")
					if len(actions) != 3 || actions[1] != "disable" {
						t.Fatal("expiry guard removed before dispatch disabled")
					}
					writeJSON(t, w, map[string]any{"data": true})
				case r.Method == "DELETE" && r.URL.Path == "/api/v1/admin/accounts/10":
					actions = append(actions, "rollback")
					writeJSON(t, w, map[string]any{"data": true})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			state := &State{UserID: "u", AdminAccountID: "w", Session: upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: server.URL, AccessToken: "test"}}
			repo := &repairConnRepo{realConnectConnRepo: realConnectConnRepo{getConn: &RealConnection{ID: "c", UserID: "u", WorkspaceAdminAccountID: "w", AdminAccountID: "9", UpstreamSiteID: "s", UpstreamKey: "existing-key", GroupType: "openai", OwnGroupIDs: []string{"1"}}}, fail: mode == "save-error"}
			if mode == "stale-group" {
				repo.getConn.OwnGroupIDs = []string{"99"}
			}
			site := &upstream.Site{ID: "s", UserID: "u", AdminAccountID: "w", BaseURL: "https://upstream.example"}
			if mode == "foreign-site" {
				site.UserID = "another-user"
			}
			service := NewService(&realConnectStateRepo{state: state}, upstream.NewPlatformService(upstream.NewHTTPClient(server.Client())), realConnectLookup{site: site})
			service.connRepository = repo
			service.SetAdminAccountResolver(realConnectAccounts{id: "w"})
			id, err := service.RepairRealConnectionAccount(context.Background(), "u", "c")
			switch mode {
			case "success":
				if err != nil || id != "10" || repo.writes != 1 || len(actions) != 3 {
					t.Fatalf("repair failed: id=%s err=%v actions=%v", id, err, actions)
				}
			case "existing":
				if err != nil || id != "9" || len(actions) != 0 {
					t.Fatalf("existing account was recreated: %s %v", id, err)
				}
			case "save-error":
				if err == nil || repo.getConn.AdminAccountID != "9" || len(actions) != 4 || actions[3] != "rollback" {
					t.Fatalf("rollback failed: %v %v", err, actions)
				}
			default:
				if err == nil || len(actions) != 0 {
					t.Fatalf("invalid repair mutated remote: %v %v", err, actions)
				}
			}
		})
	}
}
