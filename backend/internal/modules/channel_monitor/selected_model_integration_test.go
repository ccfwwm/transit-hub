package channel_monitor

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConfiguredManualFailureStopsAndRecoveryEnables(t *testing.T) {
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("disposable database required")
	}
	ctx := context.Background()
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS real_connections(id text PRIMARY KEY,user_id text,workspace_admin_account_id text)`); e != nil {
		t.Fatal(e)
	}
	repo := NewRepository(db)
	if e = repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `DELETE FROM channel_monitor_model_states WHERE rule_id='selected-integration';DELETE FROM channel_monitor_group_states WHERE rule_id='selected-integration';DELETE FROM channel_monitor_rules WHERE id='selected-integration'`); e != nil {
		t.Fatal(e)
	}
	_, fixture, _, platform := targetFixture()
	fixture.conns.connections[0].ID = "selected-integration"
	fixture.conns.connections[0].OwnGroupIDs = []string{"a"}
	conn := fixture.conns.connections[0]
	if _, e = db.Exec(ctx, `INSERT INTO real_connections(id,user_id,workspace_admin_account_id) VALUES($1,'user-1','admin-1') ON CONFLICT DO NOTHING`, conn.ID); e != nil {
		t.Fatal(e)
	}
	if e = repo.EnsureWorkspaceRules(ctx, "user-1", "admin-1"); e != nil {
		t.Fatal(e)
	}
	config := DefaultTestModelConfig("user-1", "admin-1")
	config.StatusSource = "upstream"
	config.GroupModels = []TestModelGroupConfig{{GroupID: "a", ModelID: "selected", Mode: "text"}}
	if e = repo.SaveTestModelConfig(ctx, config); e != nil {
		t.Fatal(e)
	}
	s := NewService(repo, fixture.conns, fixture.states, fixture.upstreams, platform, fakeAccounts{})
	rule, e := repo.GetRule(ctx, conn.ID)
	if e != nil || rule == nil {
		t.Fatal(e)
	}
	rule.Enabled = true
	rule.FailureThreshold = 2
	if e = repo.UpdateRule(ctx, *rule); e != nil {
		t.Fatal(e)
	}
	o := AccountTestOptions{OneShot: true, ModelID: "selected", Mode: "text"}
	now := time.Now().UTC()
	v := ManualModelCheck{Model: "selected", Status: StatusFailed, CheckedAt: &now, Options: o, Fingerprint: configFingerprint(conn, o)}
	if e = s.applyConfiguredManualCheck(ctx, *rule, v); e != nil {
		t.Fatal(e)
	}
	if len(platform.schedulableCalls) != 0 {
		t.Fatal("disabled before failure threshold")
	}
	rule, _ = repo.GetRule(ctx, conn.ID)
	next := now.Add(time.Second)
	v.CheckedAt = &next
	if e = s.applyConfiguredManualCheck(ctx, *rule, v); e != nil {
		t.Fatal(e)
	}
	if len(platform.schedulableCalls) != 1 || platform.schedulableCalls[0].Schedulable {
		t.Fatal("configured model failures did not stop dispatch")
	}
	rule, _ = repo.GetRule(ctx, conn.ID)
	v.Status = StatusHealthy
	next = next.Add(time.Second)
	v.CheckedAt = &next
	if e = s.applyConfiguredManualCheck(ctx, *rule, v); e != nil {
		t.Fatal(e)
	}
	if len(platform.schedulableCalls) != 2 || !platform.schedulableCalls[1].Schedulable {
		t.Fatal("configured model recovery did not restore dispatch")
	}
	rule, _ = repo.GetRule(ctx, conn.ID)
	rule.ManualPaused = true
	if e = repo.UpdateRule(ctx, *rule); e != nil {
		t.Fatal(e)
	}
	next = next.Add(time.Second)
	v.CheckedAt = &next
	if e = s.applyConfiguredManualCheck(ctx, *rule, v); e != nil {
		t.Fatal(e)
	}
	if len(platform.schedulableCalls) != 2 {
		t.Fatal("manual pause overridden")
	}
}
