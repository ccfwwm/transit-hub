package my_sites

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/authctx"
)

func TestSecurityScopeRejectsUnboundStaleAndForeignKeys(t *testing.T) {
	for _, platform := range []upstream.Platform{upstream.PlatformSub2API, upstream.PlatformNewAPI} {
		t.Run(string(platform), func(t *testing.T) {
			ctx := context.Background()
			db := securityTestDatabase(t)
			repo := NewRepository(db)
			if err := repo.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			currentKey := "fixture-bound-key-0123456789"
			boundKey := currentKey
			writes := 0
			var exports atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != "GET" {
					writes++
				}
				reply := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": v}) }
				key := func(id int) map[string]any {
					return map[string]any{"id": id, "key": boundKey, "group_id": 113, "group": "pro", "allow_ips": upstream.DefaultMonitorIP, "ip_whitelist": []string{upstream.DefaultMonitorIP}}
				}
				switch r.URL.Path {
				case "/api/v1/auth/me":
					reply(map[string]any{"role": "admin"})
				case "/api/v1/admin/accounts/2211":
					reply(map[string]any{"id": 2211, "type": "apikey", "platform": "openai", "credentials": map[string]any{"api_key": "***", "base_url": "https://api.example.test"}})
				case "/api/v1/admin/accounts/data":
					exports.Add(1)
					reply(map[string]any{"accounts": []any{map[string]any{"platform": "openai", "credentials": map[string]any{"api_key": currentKey, "base_url": "https://api.example.test"}}}})
				case "/api/v1/keys", "/api/token/":
					if r.Method == "GET" {
						reply(map[string]any{"items": []any{key(7), key(8), key(9)}, "total": 3})
					} else {
						reply(key(7))
					}
				case "/api/v1/keys/7", "/api/token/7":
					reply(key(7))
				case "/api/token/7/key":
					reply(map[string]any{"key": boundKey})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, "missing", 404)
				}
			}))
			defer server.Close()
			client := upstream.NewPlatformService(upstream.NewHTTPClient(server.Client()))
			session := upstream.Session{Platform: platform, BaseURL: server.URL, AccessToken: "fixture-upstream", UserID: "1"}
			site := &upstream.Site{ID: "s", UserID: "u", AdminAccountID: "w", Platform: platform, Session: &session}
			svc := NewService(repo, client, realConnectLookup{site: site})
			if err := svc.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, State{UserID: "u", AdminAccountID: "w", Session: upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: server.URL, AccessToken: "fixture-admin"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `CREATE TABLE upstream_sites(id text,user_id text,admin_account_id text,platform text,settings jsonb NOT NULL DEFAULT '{}')`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `INSERT INTO upstream_sites(id,user_id,admin_account_id,platform) VALUES('s','u','w',$1)`, platform); err != nil {
				t.Fatal(err)
			}
			group := "113"
			if platform == upstream.PlatformNewAPI {
				group = "pro"
			}
			c := RealConnection{ID: "c", UserID: "u", WorkspaceAdminAccountID: "w", UpstreamSiteID: "s", UpstreamKeyID: "7", UpstreamKey: currentKey, UpstreamGroupID: group, AdminAccountID: "2211", CreatedAt: time.Now().Format(time.RFC3339)}
			if err := repo.SaveRealConnection(ctx, c); err != nil {
				t.Fatal(err)
			}
			foreign := c
			foreign.ID = "foreign"
			foreign.WorkspaceAdminAccountID = "other"
			foreign.UpstreamKeyID = "9"
			if err := repo.SaveRealConnection(ctx, foreign); err != nil {
				t.Fatal(err)
			}
			j := jobs.New(db)
			if err := j.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			p := upstream.NewKeyPolicies(db, client)
			if err := p.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			k, err := NewKeySecurity(db, svc, p, j, base64.StdEncoding.EncodeToString(make([]byte, 32)))
			if err != nil {
				t.Fatal(err)
			}
			items, err := k.targets(ctx, "u", "w", "audit")
			if err != nil || len(items) != 1 || items[0].ID != "s/7" {
				t.Fatal("wrong bound targets", items, err)
			}
			// The exact filtered rule IDs can include several rows using one Key
			// and a rule without an eligible binding.
			shared := c
			shared.ID = "shared"
			shared.AdminAccountID = "2212"
			if err := repo.SaveRealConnection(ctx, shared); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `CREATE TABLE channel_monitor_rules(id text primary key,user_id text,admin_account_id text,connection_id text);
				INSERT INTO channel_monitor_rules VALUES('r1','u','w','c'),('r2','u','w','shared'),('missing','u','w','absent'),('wrong','u','other','foreign');`); err != nil {
				t.Fatal(err)
			}
			svc.SetAdminAccountResolver(realConnectAccounts{id: "w"})
			mux := http.NewServeMux()
			k.Register(mux)
			rotationRequest := func(body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/api/my-sites/key-security/rotate", bytes.NewBufferString(body))
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r.WithContext(authctx.WithUserID(ctx, "u")))
				return w
			}
			preview := rotationRequest(`{"ruleIds":["r1","r2","missing","wrong"],"preview":true}`)
			var plan struct {
				Keys, Skipped int
				Version       string
			}
			if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &plan) != nil || plan.Keys != 1 || plan.Skipped != 2 || plan.Version == "" {
				t.Fatalf("incorrect deduplicated rotation preview: %d %s", preview.Code, preview.Body.String())
			}
			for _, input := range []string{`{"ruleIds":[]}`, `{"connectionIds":[]}`, `{"ruleIds":["r1"],"version":"old"}`} {
				if response := rotationRequest(input); response.Code != 400 {
					t.Fatalf("unexpected rotation submission for %s: %d", input, response.Code)
				}
			}
			manager, err := NewKeyManagement(ctx, db, svc, j)
			if err != nil {
				t.Fatal(err)
			}
			if err = manager.removeIndependentKey(ctx, "u", "w", site, "7"); err != errBoundKeyCleanup {
				t.Fatalf("cleanup failed to protect bound Key: %v", err)
			}
			if err = repo.DeleteRealConnection(ctx, shared.ID, "u", "w"); err != nil {
				t.Fatal(err)
			}
			out, err := k.scanSite(ctx, "u", "w", "s", nil)
			if err != nil {
				t.Fatal(err)
			}
			childID, _ := out.Value.(map[string]any)["childJobId"].(string)
			child, err := j.Get(ctx, "u", "w", childID)
			if err != nil || child.Total != 1 || child.Items[0].ID != "s/7" {
				t.Fatal("inventory admitted unrelated Key", child, err)
			}
			out, err = k.runTarget(ctx, "u", "w", child.Items[0].ID, items[0].Payload)
			if err != nil || out.Value.(map[string]any)["message"] != "白名单一致" || exports.Load() != 0 || writes != 0 {
				t.Fatalf("healthy scan fetched credentials or attempted writes: outcome=%+v exports=%d writes=%d err=%v", out, exports.Load(), writes, err)
			}
			// Historical policy records and a site-wide disable must not grant scope.
			if err := p.Record(ctx, "u", "w", "s", "8", []string{upstream.DefaultMonitorIP}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `UPDATE upstream_sites SET settings='{"keyWhitelistDisabled":true}'`); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"8", "9"} {
				for _, action := range []string{"audit", "whitelist", "rotate"} {
					raw, _ := json.Marshal(securityTarget{SiteID: "s", KeyID: id, GroupID: group, Action: action})
					out, err := k.runTarget(ctx, "u", "w", "s/"+id, raw)
					if err != nil || out.Value.(map[string]any)["skipped"] != true {
						t.Fatal("unbound task not skipped", id, action, err)
					}
				}
			}
			if _, err := p.ApplyForGroup(ctx, "u", "w", *site, "8", group, true); err == nil {
				t.Fatal("group-policy write bypassed binding guard")
			}
			if writes != 0 {
				t.Fatal("unbound Key mutated", writes)
			}
			if _, err := db.Exec(ctx, `UPDATE upstream_sites SET settings='{}'`); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(securityTarget{SiteID: "s", KeyID: "7", GroupID: group, Action: "whitelist"})
			if _, err := k.runTarget(ctx, "u", "w", "s/7", raw); err != nil {
				t.Fatal("live bound Key rejected", err)
			}
			if writes != 1 {
				t.Fatal("bound Key not synchronized", writes)
			}
			currentKey = "fixture-changed-in-self-account"
			out, err = k.runTarget(ctx, "u", "w", "s/7", raw)
			if err != nil || out.Value.(map[string]any)["skipped"] != true || writes != 1 {
				t.Fatal("stale consumer authorized write", fmt.Sprint(err), writes)
			}
			currentKey = boundKey
			boundKey = "fixture-reused-upstream-key-id"
			out, err = k.runTarget(ctx, "u", "w", "s/7", raw)
			if err != nil || out.Value.(map[string]any)["skipped"] != true || writes != 1 {
				t.Fatal("reused upstream ID authorized write", err, writes)
			}
			if err := repo.DeleteRealConnection(ctx, "c", "u", "w"); err != nil {
				t.Fatal(err)
			}
			out, err = k.runTarget(ctx, "u", "w", "s/7", raw)
			if err != nil || out.Value.(map[string]any)["skipped"] != true || writes != 1 {
				t.Fatal("queued task after unbind mutated Key", err, writes)
			}
		})
	}
}

func TestAutomaticKeyAuditUsesWorkspaceInterval(t *testing.T) {
	ctx := context.Background()
	db := securityTestDatabase(t)
	repo := NewRepository(db)
	sites := NewService(repo, nil, nil)
	if err := sites.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	j := jobs.New(db)
	if err := j.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	k, err := NewKeySecurity(db, sites, nil, j, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	p := defaultSecurityPolicy()
	p.AuditIntervalMinutes = 20
	p, err = k.savePolicy(ctx, "u", "w", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `CREATE TABLE upstream_sites(id text,user_id text,admin_account_id text,platform text);
		INSERT INTO upstream_sites VALUES('s','u','w','sub2api');
		INSERT INTO key_security_site_refresh VALUES('u','w','s',now()-interval '6 minutes')`); err != nil {
		t.Fatal(err)
	}
	conn := RealConnection{ID: "c", UserID: "u", WorkspaceAdminAccountID: "w", UpstreamSiteID: "s", UpstreamKeyID: "7", UpstreamKey: "fixture-key", AdminAccountID: "42", CreatedAt: time.Now().Format(time.RFC3339)}
	if err := repo.SaveRealConnection(ctx, conn); err != nil {
		t.Fatal(err)
	}
	k.QueueSiteAudit(ctx, "u", "w", "s")
	if got, err := j.List(ctx, "u", "w"); err != nil || len(got) != 0 {
		t.Fatalf("refresh queued a scan before the interval elapsed: %+v %v", got, err)
	}
	if _, err := k.submitScan(ctx, "u", "w", p.AuditIntervalMinutes); err == nil {
		t.Fatal("scheduled scan ignored the interval")
	}
	if _, err := db.Exec(ctx, `UPDATE key_security_site_refresh SET queued_at=now()-interval '21 minutes'`); err != nil {
		t.Fatal(err)
	}
	k.QueueSiteAudit(ctx, "u", "w", "s")
	k.QueueSiteAudit(ctx, "u", "w", "s")
	if got, err := j.List(ctx, "u", "w"); err != nil || len(got) != 1 || got[0].Kind != "key_security_scan" {
		t.Fatalf("expected exactly one scan when the interval elapsed: %+v %v", got, err)
	}
}
