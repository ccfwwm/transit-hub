package my_sites

import (
	"context"
	"testing"
	"time"
)

func TestMonitorBindingPostgresUpsertAndWorkspaceIsolation(t *testing.T) {
	ctx := context.Background()
	db := securityTestDatabase(t)
	repo := NewRepository(db)
	if e := repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	conn := RealConnection{ID: "binding-verify", UserID: "binding-user", WorkspaceAdminAccountID: "binding-workspace", UpstreamSiteID: "s", UpstreamGroupID: "g1", UpstreamGroupName: "group", UpstreamKeyID: "k", UpstreamKey: "fixture-only", AdminAccountID: "a", OwnGroupIDs: []string{"1"}, GroupType: "openai", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, group := range []string{"g1", "g2"} {
		conn.UpstreamGroupID = group
		conn.UpstreamAPIURL = "https://dc.example.test/" + group
		if e := repo.SaveMonitorBinding(ctx, conn); e != nil {
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
