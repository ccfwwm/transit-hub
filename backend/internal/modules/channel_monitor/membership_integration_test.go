package channel_monitor

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/my_sites"
)

func TestMembershipOutboxDeduplicatesAndRetriesPersistedWrites(t *testing.T) {
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
	if e = my_sites.NewRepository(db).EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	repo := NewRepository(db)
	if e = repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	j := jobs.New(db)
	if e = j.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	fixture, old := priorityFixture()
	s := NewService(repo, fixture.conns, fixture.states, fixture.upstreams, fixture.platform, fakeAccounts{})
	s.SetOperations(j, nil)
	// These records are confined to a disposable database.
	db.Exec(ctx, `DELETE FROM operation_jobs;DELETE FROM channel_monitor_priority_membership;DELETE FROM channel_monitor_rules`)
	for _, c := range fixture.conns.connections {
		r, e := repo.EnsureRuleForConnection(ctx, "user-1", "admin-1", c)
		if e != nil {
			t.Fatal(e)
		}
		r.UpstreamMultiplierOverride = old.mustRule(c.ID).UpstreamMultiplierOverride
		if e = repo.UpdateRuleRateSettings(ctx, r.ID, UpdateRuleRequest{UpstreamMultiplierOverride: r.UpstreamMultiplierOverride}); e != nil {
			t.Fatal(e)
		}
		if e = repo.UpdateRule(ctx, r); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		if e = s.reconcileMembershipPriority(ctx, "user-1", "admin-1"); e != nil {
			t.Fatal(e)
		}
	}
	var count int
	db.QueryRow(ctx, `SELECT count(*) FROM operation_jobs WHERE kind='membership_priority'`).Scan(&count)
	if count != 2 {
		t.Fatalf("expected one job per group, got %d", count)
	}
	// A failed remote write stays pending and cannot acknowledge membership.
	s.platform = failingPriorityPlatform{fixture.platform}
	v, e := s.runMembershipPriority(ctx, "user-1", "admin-1", "own-1", nil)
	if e != nil || !v.Pending {
		t.Fatalf("write failure not retried: %+v %v", v, e)
	}
	var applied string
	db.QueryRow(ctx, `SELECT applied FROM channel_monitor_priority_membership WHERE group_id='own-1'`).Scan(&applied)
	if applied != "" {
		t.Fatal("failed write acknowledged")
	}
	s.platform = fixture.platform
	v, e = s.runMembershipPriority(ctx, "user-1", "admin-1", "own-1", nil)
	if e != nil || v.Pending {
		t.Fatalf("successful retry failed: %+v %v", v, e)
	}
	var done bool
	db.QueryRow(ctx, `SELECT fingerprint=applied FROM channel_monitor_priority_membership WHERE group_id='own-1'`).Scan(&done)
	if !done {
		t.Fatal("successful writes not persisted")
	}
	// Model update touches only the dedicated setting, not monitor/priority state.
	c := fixture.conns.connections[0]
	c.UserID = "user-1"
	c.WorkspaceAdminAccountID = "admin-1"
	if e = s.SetPrimaryModel(ctx, c, "chosen-model"); e != nil {
		t.Fatal(e)
	}
	allow := true
	if e = s.ConfigureBinding(ctx, c, nil, &allow); e != nil {
		t.Fatal(e)
	}
	rule, e := repo.GetRule(ctx, c.ID)
	if e != nil || rule.TestModelID != "chosen-model" || !rule.PriorityManaged || !rule.AllowWhenUpstreamRateGteOwn {
		t.Fatalf("model update damaged rule: %+v %v", rule, e)
	}
	// Removal invalidates the saved generation without spawning duplicate jobs.
	fixture.conns.connections = fixture.conns.connections[:1]
	if e = s.reconcileMembershipPriority(ctx, "user-1", "admin-1"); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(ctx, `SELECT fingerprint<>applied FROM channel_monitor_priority_membership WHERE group_id='own-1'`).Scan(&done)
	if !done {
		t.Fatal("removal not queued")
	}
}
