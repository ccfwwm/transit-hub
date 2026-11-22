package my_sites

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
)

func TestKeySecurityCleanupOnlyConfirmedDeletedAccounts(t *testing.T) {
	db := securityTestDatabase(t)
	ctx := context.Background()
	repo := NewRepository(db)
	if e := repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/me" {
			w.Write([]byte(`{"data":{"role":"admin"}}`))
			return
		}
		switch r.URL.Path {
		case "/api/v1/admin/accounts/1":
			http.Error(w, "gone", 404)
		case "/api/v1/admin/accounts/2":
			http.Error(w, "limited", 429)
		case "/api/v1/admin/accounts/3":
			http.Error(w, "forbidden", 403)
		default:
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 4, "platform": "openai", "credentials": map[string]any{"api_key": "fixture-existing-key-012345", "base_url": "https://api.example.test"}}})
		}
	}))
	defer server.Close()
	platform := upstream.NewPlatformService(upstream.NewHTTPClient(server.Client()))
	svc := NewService(repo, platform, nil)
	if e := svc.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	if e := repo.Save(ctx, State{UserID: "u", AdminAccountID: "w", Session: upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: server.URL, AccessToken: "admin"}}); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"1", "2", "3", "4"} {
		if e := repo.SaveRealConnection(ctx, RealConnection{ID: "c" + id, UserID: "u", WorkspaceAdminAccountID: "w", AdminAccountID: id, UpstreamSiteID: "s", UpstreamKeyID: id, CreatedAt: time.Now().Format(time.RFC3339)}); e != nil {
			t.Fatal(e)
		}
	}
	j := jobs.New(db)
	if e := j.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	k, e := NewKeySecurity(db, svc, nil, j, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `CREATE TABLE channel_monitor_rules(id text,user_id text,admin_account_id text,connection_id text);CREATE TABLE channel_monitor_views(rule_id text);CREATE TABLE channel_monitor_snapshots(user_id text,admin_account_id text);INSERT INTO channel_monitor_rules VALUES('r1','u','w','c1');INSERT INTO channel_monitor_views VALUES('r1');INSERT INTO channel_monitor_snapshots VALUES('u','w')`); e != nil {
		t.Fatal(e)
	}
	removed, e := k.pruneDeletedBindings(ctx, "u", "w")
	if e != nil || removed != 1 {
		t.Fatal("cleanup failed", removed, e)
	}
	conns, e := repo.ListRealConnections(ctx, "u", "w")
	if e != nil || len(conns) != 3 {
		t.Fatal("429/403/live account removed", e)
	}
	for _, table := range []string{"channel_monitor_rules", "channel_monitor_views", "channel_monitor_snapshots"} {
		var n int
		if e = db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("stale monitor data remains", table, e)
		}
	}
	if c, e := repo.GetRealConnection(ctx, "c1", "u", "w"); e != nil || c != nil {
		t.Fatal("deleted binding remains", e)
	}
}
