package my_sites

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
)

func TestKeySecurityResponseReusesMetadataAndIsolatesUpstreamSessions(t *testing.T) {
	db := securityTestDatabase(t)
	ctx := context.Background()
	repo := NewRepository(db)
	if e := repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	session := upstream.Session{AccessToken: "w"}
	site := &upstream.Site{ID: "w", UserID: "u", AdminAccountID: "w", Session: &session}
	svc := NewService(repo, nil, realConnectLookup{site: site})
	j := jobs.New(db)
	if e := j.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	p := upstream.NewKeyPolicies(db, nil)
	if e := p.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	k, e := NewKeySecurity(db, svc, p, j, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `CREATE TABLE upstream_sites(id text,user_id text,admin_account_id text,base_url text,session jsonb,settings jsonb NOT NULL DEFAULT '{}')`); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"w", "foreign"} {
		raw, _ := json.Marshal(upstream.Session{AccessToken: id})
		if _, e = db.Exec(ctx, `INSERT INTO upstream_sites(id,user_id,admin_account_id,base_url,session) VALUES($1,'u',$1,'https://example.test',$2)`, id, raw); e != nil {
			t.Fatal(e)
		}
		if _, e = k.savePolicy(ctx, "u", id, defaultSecurityPolicy()); e != nil {
			t.Fatal(e)
		}
	}
	if e = repo.SaveRealConnection(ctx, RealConnection{ID: "c", UserID: "u", WorkspaceAdminAccountID: "w", UpstreamSiteID: "w", UpstreamKeyID: "7", UpstreamKey: "fixture-secret", AdminAccountID: "2211", CreatedAt: time.Now().Format(time.RFC3339)}); e != nil {
		t.Fatal(e)
	}
	v := upstream.KeySecurityObservation{BaseURL: "https://example.test", KeyID: "7", GroupID: "pro", AllowedIPs: []string{upstream.DefaultMonitorIP}, AuthFingerprint: upstream.KeyObservationAuth("w", "", "")}
	k.ObserveKeys([]upstream.KeySecurityObservation{v})
	var n int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM operation_jobs`).Scan(&n); e != nil || n != 0 {
		t.Fatal("consistent response added requests", n, e)
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM upstream_key_policy_bindings WHERE admin_account_id='foreign'`).Scan(&n); e != nil || n != 0 {
		t.Fatal("same origin leaked into foreign account", n, e)
	}
	v.AllowedIPs = []string{"198.51.100.4"}
	// Merely appearing in a logged-in account's response grants no authority.
	unbound := v
	unbound.KeyID = "unrelated-key"
	k.ObserveKeys([]upstream.KeySecurityObservation{unbound})
	if e = db.QueryRow(ctx, `SELECT count(*) FROM operation_jobs`).Scan(&n); e != nil || n != 0 {
		t.Fatal("unbound response scheduled a mutation", n, e)
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM upstream_key_policy_bindings WHERE key_id='unrelated-key'`).Scan(&n); e != nil || n != 0 {
		t.Fatal("unbound response acquired policy ownership", n, e)
	}
	k.ObserveKeys([]upstream.KeySecurityObservation{v})
	k.ObserveKeys([]upstream.KeySecurityObservation{v})
	if e = db.QueryRow(ctx, `SELECT count(*) FROM operation_jobs WHERE admin_account_id='w'`).Scan(&n); e != nil || n != 1 {
		t.Fatal("drift did not submit exactly one job", n, e)
	}
	v.AuthFingerprint = upstream.KeyObservationAuth("unauthorized-session", "", "")
	v.KeyID = "8"
	k.ObserveKeys([]upstream.KeySecurityObservation{v})
	if e = db.QueryRow(ctx, `SELECT count(*) FROM operation_jobs`).Scan(&n); e != nil || n != 1 {
		t.Fatal("foreign authentication scheduled a mutation", n, e)
	}
}
