package channel_monitor

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReadModelThousandBindingsMillionHistory(t *testing.T) {
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("requires monitor_verify database")
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
	r := NewRepository(db)
	if e = r.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	base := SummaryResponse{Channels: []ChannelStatus{}, Groups: []GroupSummary{{GroupID: "perf", GroupName: "perf", Available: 1000}}, TestModelConfig: DefaultTestModelConfig("perf", "perf")}
	for i := 0; i < 1000; i++ {
		base.Channels = append(base.Channels, ChannelStatus{RuleID: fmt.Sprintf("perf-%04d", i), ConnectionID: fmt.Sprintf("perf-%04d", i), SiteName: "site", UpstreamGroupName: "pro", OwnGroupIDs: []string{"perf"}, OwnGroups: []string{"perf"}, Status: StatusHealthy, Enabled: true, DetectionGroupID: "perf", RecentResults: []Result{}})
	}
	if e = r.publishSnapshot(ctx, "perf", "perf", base); e != nil {
		t.Fatal(e)
	}
	// A million historical rows spread across 1,000 targets. Repeated runs reuse
	// the fixture rather than silently adding another million rows.
	_, e = db.Exec(ctx, `INSERT INTO channel_monitor_results(id,rule_id,connection_id,group_id,status,success,started_at,finished_at,created_at)
 SELECT 'perf-history-'||i,'perf-'||lpad((i%1000)::text,4,'0'),'perf-'||lpad((i%1000)::text,4,'0'),'perf','healthy',true,now(),now(),now()-i*interval '1 second' FROM generate_series(1,1000000)i ON CONFLICT DO NOTHING`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `ANALYZE channel_monitor_views;ANALYZE channel_monitor_results`); e != nil {
		t.Fatal(e)
	}
	durations := []time.Duration{}
	for i := 0; i < 30; i++ {
		start := time.Now()
		page, e := r.readPage(ctx, "perf", "perf", ChannelPageQuery{GroupID: "perf", Page: i%40 + 1, PageSize: 25, Search: "pro"})
		if e != nil {
			t.Fatal(e)
		}
		if page.Total != 1000 || len(page.Channels) != 25 {
			t.Fatalf("wrong page %d/%d", page.Total, len(page.Channels))
		}
		ids, groups := []string{}, []string{}
		for _, ch := range page.Channels {
			ids = append(ids, ch.RuleID)
			groups = append(groups, "perf")
		}
		hist, e := r.PageHistory(ctx, ids, groups)
		if e != nil {
			t.Fatal(e)
		}
		for _, id := range ids {
			if len(hist[id+"|perf"]) != 60 {
				t.Fatal("unbounded or missing history")
			}
		}
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[28]
	t.Logf("1000 bindings / 1,000,000 history rows: page + 25 x 60 history P95=%s", p95)
	if p95 > 500*time.Millisecond {
		t.Fatal("pagination P95 exceeded 500ms")
	}
	page, e := r.readPage(ctx, "other-user", "perf", ChannelPageQuery{GroupID: "perf"})
	if e != nil || page.Total != 0 {
		t.Fatal("cross-workspace read")
	}
	page, e = r.readPage(ctx, "perf", "perf", ChannelPageQuery{GroupID: "perf", Status: StatusFailed})
	if e != nil || page.Total != 0 {
		t.Fatal("status filter not applied")
	}
}
