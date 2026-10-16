package channel_monitor

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transithub/backend/internal/modules/my_sites"
)

type monitorQueryCounter struct{ count atomic.Int64 }

func (q *monitorQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.count.Add(1)
	return ctx
}
func (*monitorQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// This opt-in integration test must run against a disposable database.
func TestMonitorPostgresMigrationClaimsAnd500ChannelPerformance(t *testing.T) {
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MONITOR_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("requires disposable monitor_verify database")
	}
	counter := &monitorQueryCounter{}
	cfg.ConnConfig.Tracer = counter
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS real_connections(id text PRIMARY KEY,user_id text,workspace_admin_account_id text)`); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	for i := 0; i < 2; i++ {
		if err = repo.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
	_, fixture, _, _ := targetFixture()
	base := fixture.conns.connections[0]
	connections := make([]my_sites.RealConnection, 500)
	for i := range connections {
		c := base
		c.ID = fmt.Sprintf("verify-%d", i)
		c.OwnGroupIDs = []string{"a"}
		connections[i] = c
	}
	_, err = db.Exec(ctx, `INSERT INTO real_connections SELECT 'verify-'||i,'user-1','admin-1' FROM generate_series(0,499)i ON CONFLICT DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.EnsureWorkspaceRules(ctx, "user-1", "admin-1"); err != nil {
		t.Fatal(err)
	}
	config := DefaultTestModelConfig("user-1", "admin-1")
	if err = repo.SaveTestModelConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	rules, err := repo.ListRulesForWorkspace(ctx, "user-1", "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		state := targetFor(r, base, "a", fixture.state(), config, nil)
		state.Status = StatusHealthy
		if err = repo.SaveTargetState(ctx, state); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(ctx, `INSERT INTO channel_monitor_results(id,rule_id,connection_id,group_id,status,success,started_at,finished_at,created_at)
 SELECT r.id||'-'||n,r.id,r.id,'a','healthy',true,now(),now(),now()-n*interval '1 minute' FROM channel_monitor_rules r CROSS JOIN generate_series(1,70)n ON CONFLICT DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, fixture.conns, fixture.states, fixture.upstreams, fixture.Service.platform, fakeAccounts{})
	counts := []int64{}
	for _, n := range []int{50, 500} {
		fixture.conns.connections = connections[:n]
		if _, err = service.summaryInGroup(ctx, "user-1", "a", true); err != nil {
			t.Fatal(err)
		}
		if _, err = service.ChannelPage(ctx, "user-1", ChannelPageQuery{GroupID: "a", PageSize: 25}); err != nil {
			t.Fatal(err)
		}
		before := counter.count.Load()
		if _, err = service.summaryInGroup(ctx, "user-1", "a", true); err != nil {
			t.Fatal(err)
		}
		count := counter.count.Load() - before
		counts = append(counts, count)
		summaries, pages := []time.Duration{}, []time.Duration{}
		for i := 0; i < 25; i++ {
			start := time.Now()
			if _, err = service.summaryInGroup(ctx, "user-1", "a", true); err != nil {
				t.Fatal(err)
			}
			summaries = append(summaries, time.Since(start))
			start = time.Now()
			page, e := service.ChannelPage(ctx, "user-1", ChannelPageQuery{GroupID: "a", PageSize: 25})
			if e != nil {
				t.Fatal(e)
			}
			pages = append(pages, time.Since(start))
			if page.Total != n || len(page.Channels) != 25 || len(page.Channels[0].RecentResults) != 60 {
				t.Fatalf("incorrect bounded page n=%d total=%d", n, page.Total)
			}
		}
		sort.Slice(summaries, func(i, j int) bool { return summaries[i] < summaries[j] })
		sort.Slice(pages, func(i, j int) bool { return pages[i] < pages[j] })
		t.Logf("channels=%d summary_queries=%d summary_P95=%s page_P95=%s", n, count, summaries[23], pages[23])
		if summaries[23] >= time.Second || pages[23] >= time.Second {
			t.Fatal("cache-hit P95 exceeded 1 second")
		}
	}
	if counts[0] != counts[1] {
		t.Fatalf("summary query growth: %v", counts)
	}
	task := MediaTask{Key: "claim-once", UserID: "user-1", AdminAccountID: "admin-1", RuleID: "verify-0", Mode: "video", Options: AccountTestOptions{ModelID: "alias", Mode: "video", Seconds: 4}}
	var claims atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, e := repo.ClaimMediaTask(ctx, task, time.Now())
			if e != nil {
				t.Error(e)
			}
			if won {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("duplicate submission claims: %d", claims.Load())
	}
	saved, e := repo.GetMediaTask(ctx, task.Key)
	if e != nil || saved.Options.Seconds != 4 {
		t.Fatal("snapshot not durable", e)
	}
	saved.Status = "pending"
	saved.TaskID = "original-task"
	if err = repo.SaveMediaTask(ctx, *saved); err != nil {
		t.Fatal(err)
	}
	active, e := repo.ActiveMediaTask(ctx, task.RuleID)
	if e != nil || active.TaskID != "original-task" {
		t.Fatal("active task missing", e)
	}
	history, e := repo.TargetHistory(ctx, "verify-0", "", 60)
	if e != nil || len(history) != 0 {
		t.Fatal("old history assigned a new group", e)
	}
}
