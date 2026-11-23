package channel_monitor

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"

	"transithub/backend/internal/modules/upstream"
)

func TestImageTaskMigrationRetiresOnlyUnrecoverableImages(t *testing.T) {
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
	defer db.Exec(ctx, `DELETE FROM channel_monitor_media_tasks WHERE rule_id='image-migration'`)
	_, e = db.Exec(ctx, `INSERT INTO channel_monitor_media_tasks(key,user_id,admin_account_id,rule_id,model_id,mode,status,task_id,created_at,updated_at) VALUES
 ('image-migration-unknown','u','w','image-migration','gpt-image-2.5','image','unknown','',now()-interval '2 hours',now()-interval '2 hours'),
 ('image-migration-timeout','u','w','image-migration','gemini-nano-banana-2.1','image','timeout','',now()-interval '2 hours',now()-interval '2 hours'),
 ('image-migration-accepted','u','w','image-migration','gpt-image-2.5','image','pending','accepted',now()-interval '2 minutes',now()),
 ('image-migration-video','u','w','image-migration','video','video','unknown','',now()-interval '2 hours',now()) ON CONFLICT(key) DO NOTHING`)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = repo.ensureTargetSchema(ctx); e != nil {
			t.Fatal(e)
		}
	}
	for suffix, want := range map[string]string{"unknown": "failed", "timeout": "failed", "accepted": "pending", "video": "unknown"} {
		var got string
		if e = db.QueryRow(ctx, `SELECT status FROM channel_monitor_media_tasks WHERE key=$1`, "image-migration-"+suffix).Scan(&got); e != nil || got != want {
			t.Fatal(suffix, got, e)
		}
	}
}

func TestAcceptedImageRetainsOriginalTaskWhileConfigurationChanges(t *testing.T) {
	ctx, s, store, p := targetFixture()
	o := AccountTestOptions{ModelID: "gpt-image-2.5", Mode: "image", Adapter: "sub2api_async_image", APIURL: "https://original.example", OneShot: true}
	store.tasks["old"] = MediaTask{Key: "old", RuleID: "conn-1", ModelID: o.ModelID, Mode: "image", Status: "pending", TaskID: "accepted", Options: o, CreatedAt: time.Now().Add(-time.Minute), UpdatedAt: time.Now().Add(-time.Minute)}
	polled := make(chan AccountTestOptions, 1)
	p.probe = func(o AccountTestOptions) (AccountTestResult, error) {
		polled <- o
		return AccountTestResult{Pending: true}, nil
	}
	changed := o
	changed.Adapter = "openai_image"
	result, e := s.mediaCheck(ctx, store.mustRule("conn-1"), s.conns.connections[0], upstream.Site{}, TargetState{Fingerprint: "new"}, changed)
	if e != nil || !result.Pending || result.Uncertain || store.claims != 0 {
		t.Fatal(result, e)
	}
	select {
	case actual := <-polled:
		if actual.TaskID != "accepted" || actual.APIURL != o.APIURL || actual.Adapter != o.Adapter {
			t.Fatal("task protocol was replaced")
		}
	case <-time.After(time.Second):
		t.Fatal("accepted task not polled")
	}
}
