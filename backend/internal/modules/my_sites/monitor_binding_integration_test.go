package my_sites

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMonitorBindingPostgresUpsertAndWorkspaceIsolation(t *testing.T) {
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MONITOR_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("requires disposable monitor_verify database")
	}
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	repo := NewRepository(db)
	if e = repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	conn := RealConnection{ID: "binding-verify", UserID: "binding-user", WorkspaceAdminAccountID: "binding-workspace", UpstreamSiteID: "s", UpstreamGroupID: "g1", UpstreamGroupName: "group", UpstreamKeyID: "k", UpstreamKey: "fixture-only", AdminAccountID: "a", OwnGroupIDs: []string{"1"}, GroupType: "openai", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	defer db.Exec(ctx, `DELETE FROM real_connections WHERE id='binding-verify'`)
	for _, group := range []string{"g1", "g2"} {
		conn.UpstreamGroupID = group
		conn.UpstreamAPIURL = "https://dc.example.test/" + group
		if e = repo.SaveMonitorBinding(ctx, conn); e != nil {
			t.Fatal(e)
		}
	}
	foreign := conn
	foreign.WorkspaceAdminAccountID = "other"
	foreign.UpstreamGroupID = "foreign"
	if repo.SaveMonitorBinding(ctx, foreign) == nil {
		t.Fatal("cross-workspace overwrite accepted")
	}
	saved, e := repo.GetRealConnection(ctx, conn.ID, conn.UserID, conn.WorkspaceAdminAccountID)
	if e != nil || saved == nil || saved.UpstreamGroupID != "g2" || saved.UpstreamAPIURL != conn.UpstreamAPIURL || saved.UpstreamKey != conn.UpstreamKey || len(saved.OwnGroupIDs) != 1 {
		t.Fatal("upsert lost scope or forwarding credentials", e)
	}
}
