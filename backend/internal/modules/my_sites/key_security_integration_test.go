package my_sites

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
)

func securityTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("disposable monitor_verify database required")
	}
	ctx := context.Background()
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("security_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	return db
}

func TestKeySecurityRotationResumesAndPreservesConsumer(t *testing.T) {
	for _, platform := range []upstream.Platform{upstream.PlatformSub2API, upstream.PlatformNewAPI} {
		t.Run(string(platform), func(t *testing.T) {
			db := securityTestDatabase(t)
			ctx := context.Background()
			repo := NewRepository(db)
			if e := repo.EnsureSchema(ctx); e != nil {
				t.Fatal(e)
			}
			old := map[string]any{"id": float64(7), "name": "original", "key": "fixture-old-secret-0123456789", "group_id": float64(113), "status": "inactive", "quota": float64(100), "quota_used": float64(23), "rate_limit_1d": float64(5), "expires_at": "2027-01-02T03:04:05Z", "ip_whitelist": []any{"198.51.100.4"}}
			if platform == upstream.PlatformNewAPI {
				old = map[string]any{"id": float64(7), "name": "original", "key": "fixture-old-secret-0123456789", "group": "pro", "status": float64(2), "remain_quota": float64(123), "unlimited_quota": false, "expired_time": float64(1800000000), "model_limits": "a,b", "model_limits_enabled": true, "cross_group_retry": false, "allow_ips": "198.51.100.4"}
			}
			inventory := map[string]map[string]any{"7": old}
			creates, deletes, updates := 0, 0, 0
			failAccountWrite, failDelete := true, true
			currentKey := "fixture-old-secret-0123456789"
			accountName := "legacy account"
			targetName := "foxrouter-图片分组-0.15x-006"
			ordinal := 6
			if platform == upstream.PlatformNewAPI {
				targetName = "foxrouter-图片分组-0.15x-001"
				ordinal = 1
			}
			rate := 0.15
			credentials := map[string]any{"api_key": currentKey, "base_url": "https://dc.example.test", "pool_mode": true, "tier_id": "keep"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				reply := func(d any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": d}) }
				path := r.URL.Path
				if path == "/api/v1/auth/me" {
					reply(map[string]any{"role": "admin"})
					return
				}
				if path == "/api/v1/admin/accounts/data" {
					copy := map[string]any{}
					for k, v := range credentials {
						copy[k] = v
					}
					copy["api_key"] = currentKey
					reply(map[string]any{"accounts": []any{map[string]any{"platform": "deepseek", "credentials": copy}}})
					return
				}
				if path == "/api/v1/admin/accounts/2211" {
					if r.Method == "PUT" {
						if failAccountWrite {
							http.Error(w, "temporary", 503)
							return
						}
						var m map[string]any
						_ = json.NewDecoder(r.Body).Decode(&m)
						if len(m) != 1 {
							t.Error("changed non-credential settings")
						}
						if name, ok := m["name"].(string); ok {
							accountName = name
							reply(nil)
							return
						}
						c := m["credentials"].(map[string]any)
						if c["base_url"] != credentials["base_url"] || c["pool_mode"] != true || c["tier_id"] != "keep" {
							t.Error("lost credentials")
						}
						currentKey = c["api_key"].(string)
						updates++
					}
					reply(map[string]any{"id": float64(2211), "name": accountName, "type": "apikey", "platform": "deepseek", "group_ids": []int{9}, "credentials": map[string]any{"api_key": "***", "base_url": credentials["base_url"]}, "schedulable": false, "priority": 17})
					return
				}
				if path == "/api/v1/admin/accounts" {
					reply(map[string]any{"items": []map[string]any{{"id": 2211, "name": accountName}}, "total": 1})
					return
				}
				isCollection := path == "/api/v1/keys" || path == "/api/token/"
				if isCollection && r.Method == "GET" {
					items := []any{}
					for _, m := range inventory {
						items = append(items, m)
					}
					reply(map[string]any{"items": items, "total": len(items)})
					return
				}
				if isCollection && r.Method == "POST" {
					creates++
					var m map[string]any
					_ = json.NewDecoder(r.Body).Decode(&m)
					m["id"] = float64(8)
					m["key"] = "fixture-new-api-secret-0123456789"
					if platform == upstream.PlatformSub2API {
						m["key"] = m["custom_key"]
						delete(m, "custom_key")
					}
					inventory["8"] = m
					reply(m)
					return
				}
				id := "7"
				if strings.Contains(path, "/8") {
					id = "8"
				}
				if strings.HasSuffix(path, "/key") {
					reply(map[string]any{"key": inventory[id]["key"]})
					return
				}
				if isCollection && r.Method == "PUT" {
					var m map[string]any
					_ = json.NewDecoder(r.Body).Decode(&m)
					if m["id"] == float64(8) {
						id = "8"
					}
					for k, v := range m {
						inventory[id][k] = v
					}
					reply(inventory[id])
					return
				}
				if strings.HasPrefix(path, "/api/v1/keys/") || strings.HasPrefix(path, "/api/token/") {
					if r.Method == "DELETE" {
						if failDelete {
							http.Error(w, "retry", 503)
							return
						}
						deletes++
						delete(inventory, id)
						reply(nil)
						return
					}
					m, ok := inventory[id]
					if !ok {
						http.Error(w, "missing", 404)
						return
					}
					if r.Method == "PUT" {
						var body map[string]any
						_ = json.NewDecoder(r.Body).Decode(&body)
						for k, v := range body {
							m[k] = v
						}
					}
					reply(m)
					return
				}
				t.Errorf("unexpected path %s %s", r.Method, path)
				http.Error(w, "missing", 404)
			}))
			defer server.Close()
			client := upstream.NewPlatformService(upstream.NewHTTPClient(server.Client()))
			session := upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: server.URL, AccessToken: "fixture-admin"}
			if e := repo.Save(ctx, State{UserID: "u", AdminAccountID: "w", Session: session, OwnGroups: []GroupOption{{ID: "9", Name: "图片分组"}}}); e != nil {
				t.Fatal(e)
			}
			upSession := upstream.Session{Platform: platform, BaseURL: server.URL, AccessToken: "fixture-upstream", UserID: "1"}
			site := &upstream.Site{ID: "s", UserID: "u", AdminAccountID: "w", BaseURL: "https://api.foxrouter.com", Platform: platform, Session: &upSession}
			svc := NewService(repo, client, realConnectLookup{site: site})
			if e := svc.EnsureSchema(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e := db.Exec(ctx, `CREATE TABLE upstream_sites(id text,user_id text,admin_account_id text,settings jsonb NOT NULL DEFAULT '{}');INSERT INTO upstream_sites(id,user_id,admin_account_id) VALUES('s','u','w')`); e != nil {
				t.Fatal(e)
			}
			p := upstream.NewKeyPolicies(db, client)
			if e := p.EnsureSchema(ctx); e != nil {
				t.Fatal(e)
			}
			svc.SetKeyPolicies(p)
			group := "113"
			if platform == upstream.PlatformNewAPI {
				group = "pro"
			}
			site.Metrics.Groups = []upstream.GroupInfo{{ID: group, Multiplier: &rate}}
			c := RealConnection{ID: "c", UserID: "u", WorkspaceAdminAccountID: "w", UpstreamSiteID: "s", UpstreamGroupID: group, UpstreamKeyID: "7", UpstreamKey: "fixture-old-secret-0123456789", AdminAccountID: "2211", OwnGroupIDs: []string{"9"}, CreatedAt: time.Now().Format(time.RFC3339), UpstreamAPIURL: "https://dc.example.test"}
			if e := repo.SaveRealConnection(ctx, c); e != nil {
				t.Fatal(e)
			}
			j := jobs.New(db)
			if e := j.EnsureSchema(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e := NewKeyManagement(ctx, db, svc, j); e != nil {
				t.Fatal(e)
			}
			if platform == upstream.PlatformSub2API {
				if _, e := db.Exec(ctx, `INSERT INTO managed_binding_names VALUES('u','w','c','9','foxrouter',6,'old-applied-name')`); e != nil {
					t.Fatal(e)
				}
			}
			enc := base64.StdEncoding.EncodeToString(make([]byte, 32))
			k, e := NewKeySecurity(db, svc, p, j, enc)
			if e != nil {
				t.Fatal(e)
			}
			target := securityTarget{SiteID: "s", KeyID: "7", GroupID: group, Action: "audit"}
			raw, _ := json.Marshal(target)
			// Whitelist drift repairs immediately but failed consumer update never revokes.
			if _, e = k.runTarget(ctx, "u", "w", "s/7", raw); e == nil {
				t.Fatal("failed account write reported success")
			}
			if inventory["7"] == nil || creates != 1 || deletes != 0 {
				t.Fatal("old revoked before consumer verified")
			}
			var phase string
			var encrypted string
			_ = db.QueryRow(ctx, `SELECT phase,new_secret FROM key_security_rotations`).Scan(&phase, &encrypted)
			if phase != "verified" || !strings.HasPrefix(encrypted, "v1:") || strings.Contains(encrypted, currentKey) {
				t.Fatal("recovery state invalid", phase)
			}
			// Reconstruct service to simulate restart; successful cutover + failed delete.
			k, e = NewKeySecurity(db, svc, p, j, enc)
			if e != nil {
				t.Fatal(e)
			}
			failAccountWrite = false
			if _, e = k.runTarget(ctx, "u", "w", "s/7", raw); e == nil {
				t.Fatal("failed revocation reported success")
			}
			if creates != 1 || updates != 1 || deletes != 0 || inventory["7"] == nil {
				t.Fatal("cutover repeated or premature revoke")
			}
			saved, e := repo.GetRealConnection(ctx, "c", "u", "w")
			if e != nil || saved.UpstreamKeyID != "8" || saved.UpstreamKey != currentKey || saved.UpstreamAPIURL != c.UpstreamAPIURL {
				t.Fatal("local binding not switched safely", e)
			}
			failDelete = false
			out, e := k.runTarget(ctx, "u", "w", "s/7", raw)
			if e != nil {
				t.Fatal(e)
			}
			if creates != 1 || updates != 1 || deletes != 1 || inventory["7"] != nil {
				t.Fatal("retry created extra key or failed revocation")
			}
			result, _ := json.Marshal(out.Value)
			if strings.Contains(string(result), "secret") || strings.Contains(string(result), currentKey) {
				t.Fatal("job exposed key")
			}
			if _, e = k.runTarget(ctx, "u", "w", "s/7", raw); e != nil {
				t.Fatal("completed task retry failed", e)
			}
			if creates != 1 || deletes != 1 {
				t.Fatal("completed retry mutated key")
			}
			newRecord := inventory["8"]
			if newRecord["name"] != targetName || accountName != targetName {
				t.Fatalf("unexpected names upstream=%q forwarding=%q", newRecord["name"], accountName)
			}
			var applied string
			var number int
			if e := db.QueryRow(ctx, `SELECT applied_name,ordinal FROM managed_binding_names WHERE connection_id='c'`).Scan(&applied, &number); e != nil || applied != targetName || number != ordinal {
				t.Fatalf("unexpected naming reservation %q %d %v", applied, number, e)
			}
			saved, e = repo.GetRealConnection(ctx, "c", "u", "w")
			if e != nil || saved.AdminAccountName != targetName {
				t.Fatal("local forwarding name was not updated", e)
			}
			if platform == upstream.PlatformSub2API {
				if newRecord["status"] != "inactive" || newRecord["quota"] != float64(77) || newRecord["rate_limit_1d"] != float64(5) {
					t.Fatal("old restrictions lost", newRecord)
				}
			} else {
				if newRecord["status"] != float64(2) || newRecord["remain_quota"] != float64(123) || newRecord["model_limits"] != "a,b" {
					t.Fatal("token restrictions lost", newRecord)
				}
			}
			// Exact audit is read-only for keys already matching the policy.
			target.KeyID = "8"
			raw, _ = json.Marshal(target)
			if _, e = k.runTarget(ctx, "u", "w", "s/8", raw); e != nil {
				t.Fatal(e)
			}
			if creates != 1 {
				t.Fatal("matching whitelist caused rotation")
			}
			if _, e = k.runTarget(ctx, "foreign", "w", "s/8", raw); e == nil {
				t.Fatal("cross-workspace write accepted")
			}
		})
	}
}

func TestKeySecuritySchedulerClaimsMissedRotationOnlyOnce(t *testing.T) {
	db := securityTestDatabase(t)
	ctx := context.Background()
	repo := NewRepository(db)
	if e := repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	j := jobs.New(db)
	if e := j.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	svc := NewService(repo, nil, nil)
	k, e := NewKeySecurity(db, svc, nil, j, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if e != nil {
		t.Fatal(e)
	}
	c := RealConnection{ID: "c", UserID: "u", WorkspaceAdminAccountID: "w", UpstreamSiteID: "s", UpstreamKeyID: "7", UpstreamKey: "fixture-bound-secret", AdminAccountID: "2211", CreatedAt: time.Now().Format(time.RFC3339)}
	if e = repo.SaveRealConnection(ctx, c); e != nil {
		t.Fatal(e)
	}
	p := defaultSecurityPolicy()
	if _, e = k.savePolicy(ctx, "u", "w", p); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(ctx, `UPDATE key_security_policies SET next_rotation=now()-interval '50 hours'`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(ctx, `UPDATE key_security_policies SET last_discovery=now()`)
	if e != nil {
		t.Fatal(e)
	}
	k.tickScope(ctx, "u", "w")
	k.tickScope(ctx, "u", "w")
	var count int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM operation_jobs`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate missed-schedule batches", count)
	}
	saved, e := k.policy(ctx, "u", "w")
	if e != nil || !saved.NextRotation.After(time.Now()) {
		t.Fatal("schedule did not advance", e)
	}
}
