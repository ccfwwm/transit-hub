package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGroupKeyPolicyIsolationAndReadback(t *testing.T) {
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("disposable database required")
	}
	ctx := context.Background()
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("key_policy_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, `CREATE SCHEMA `+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(ctx, `CREATE TABLE upstream_sites(id text PRIMARY KEY,user_id text NOT NULL,admin_account_id text NOT NULL,settings jsonb NOT NULL DEFAULT '{}');INSERT INTO upstream_sites(id,user_id,admin_account_id) VALUES('s','u','w')`); e != nil {
		t.Fatal(e)
	}
	record := map[string]any{"id": float64(7), "name": "bound", "remain_quota": float64(123), "expired_time": float64(456), "group": "pro", "model_limits": "one,two", "model_limits_enabled": true, "allow_ips": "206.82.2.46\n10.0.0.9"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/token/7" && r.URL.Path != "/api/token/" {
			t.Errorf("unexpected key path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "PUT" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			record = body
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": record})
	}))
	defer server.Close()
	p := NewKeyPolicies(db, NewPlatformService(NewHTTPClient(server.Client())))
	if e = p.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	a, e := p.Group(ctx, "u", "w", "s", "a")
	if e != nil || !a.Inherited || a.AllowedIPs[0] != DefaultMonitorIP {
		t.Fatal(a, e)
	}
	sitePolicy, e := p.SaveGroup(ctx, "u", "w", "s", "", GroupKeyPolicy{KeyPolicy: KeyPolicy{AllowedIPs: []string{"192.0.2.2"}}})
	if e != nil || sitePolicy.Inherited || sitePolicy.Version != 1 {
		t.Fatal(sitePolicy, e)
	}
	inherited, e := p.Group(ctx, "u", "w", "s", "a")
	if e != nil || !inherited.Inherited || inherited.Version != 0 || !reflect.DeepEqual(inherited.AllowedIPs, []string{"192.0.2.2"}) {
		t.Fatal("group failed to inherit site policy", inherited, e)
	}
	a.Inherited = false
	a.AllowedIPs = []string{"2001:db8::1", "192.0.2.0/24"}
	a, e = p.SaveGroup(ctx, "u", "w", "s", "a", a)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.SaveGroup(ctx, "u", "w", "s", "", GroupKeyPolicy{Inherited: true, KeyPolicy: KeyPolicy{Version: sitePolicy.Version}}); e != nil {
		t.Fatal(e)
	}
	if _, e = p.SaveGroup(ctx, "u", "w", "s", "a", GroupKeyPolicy{KeyPolicy: KeyPolicy{Version: 0}, Inherited: true}); e == nil {
		t.Fatal("stale update cleared override")
	}
	for _, scope := range [][4]string{{"u", "w", "s", "b"}, {"u", "other", "s", "a"}, {"other", "w", "s", "a"}} {
		v, e := p.Group(ctx, scope[0], scope[1], scope[2], scope[3])
		if e != nil || !v.Inherited || v.AllowedIPs[0] != DefaultMonitorIP {
			t.Fatal("policy leaked", v, e)
		}
	}
	if e = p.Record(ctx, "u", "w", "s", "7", []string{DefaultMonitorIP}); e != nil {
		t.Fatal(e)
	}
	site := Site{ID: "s", UserID: "u", AdminAccountID: "w", Session: &Session{Platform: PlatformNewAPI, BaseURL: server.URL}}
	got, e := p.ApplyForGroup(ctx, "u", "w", site, "7", "a", false)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, []string{"10.0.0.9", "192.0.2.0/24", "2001:db8::1"}) {
		t.Fatal("custom IP lost or global IP retained", got)
	}
	// Explicit replacement removes addresses read upstream, instead of silently
	// merging them back after the user deletes them from the editor.
	a.AllowedIPs = []string{DefaultMonitorIP}
	a, e = p.SaveGroup(ctx, "u", "w", "s", "a", a)
	if e != nil {
		t.Fatal(e)
	}
	got, e = p.ApplyForGroup(ctx, "u", "w", site, "7", "a", true)
	if e != nil || !reflect.DeepEqual(got, []string{DefaultMonitorIP}) {
		t.Fatal("explicit overwrite retained deleted upstream addresses", got, e)
	}
	var observed []byte
	if e = db.QueryRow(ctx, `SELECT observed_ips FROM upstream_key_policy_bindings WHERE user_id='u' AND admin_account_id='w' AND site_id='s' AND key_id='7'`).Scan(&observed); e != nil {
		t.Fatal(e)
	}
	var actual []string
	if json.Unmarshal(observed, &actual) != nil || !reflect.DeepEqual(actual, got) {
		t.Fatal("overwrite readback was not recorded")
	}
	for k, want := range map[string]any{"remain_quota": float64(123), "expired_time": float64(456), "group": "pro", "model_limits": "one,two", "model_limits_enabled": true} {
		if !reflect.DeepEqual(record[k], want) {
			t.Fatal("Key setting overwritten", k)
		}
	}
	a.Inherited = true
	a, e = p.SaveGroup(ctx, "u", "w", "s", "a", a)
	if e != nil || !a.Inherited || a.AllowedIPs[0] != DefaultMonitorIP {
		t.Fatal(a, e)
	}
	if e = p.SetWhitelistEnabled(ctx, "u", "w", "s", "7", false); e != nil {
		t.Fatal(e)
	}
	got, e = p.ApplyForGroup(ctx, "u", "w", site, "7", "a", false)
	if e != nil || len(got) != 0 || record["allow_ips"] != "" {
		t.Fatalf("disabled Key regained IP restrictions: %v %v", got, e)
	}
	if exact, e := p.VerifyExact(ctx, "u", "w", site, "7", "a"); e != nil || !exact {
		t.Fatalf("explicitly disabled whitelist treated as drift: %v", e)
	}
	if e = p.CopyWhitelistSwitch(ctx, "u", "w", "s", "7", "8"); e != nil {
		t.Fatal(e)
	}
	if enabled, e := p.WhitelistEnabled(ctx, "u", "w", "s", "8"); e != nil || enabled {
		t.Fatal("rotation lost disabled override", e)
	}
	if enabled, e := p.WhitelistEnabled(ctx, "u", "w", "s", "9"); e != nil || !enabled {
		t.Fatal("Key override leaked to another Key", e)
	}
	if _, e = db.Exec(ctx, `UPDATE upstream_sites SET settings='{"keyWhitelistDisabled":true}' WHERE id='s'`); e != nil {
		t.Fatal(e)
	}
	if enabled, e := p.WhitelistEnabled(ctx, "u", "w", "s", "9"); e != nil || enabled {
		t.Fatal("site default ignored", e)
	}
	if e = p.SetWhitelistEnabled(ctx, "u", "w", "s", "9", true); e != nil {
		t.Fatal(e)
	}
	if enabled, e := p.WhitelistEnabled(ctx, "u", "w", "s", "9"); e != nil || !enabled {
		t.Fatal("explicit account override ignored", e)
	}
	if _, e = p.ApplyForGroup(ctx, "u", "other", site, "7", "a", false); e == nil {
		t.Fatal("cross-workspace Key mutation accepted")
	}
}
