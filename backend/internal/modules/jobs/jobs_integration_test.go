package jobs

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDurableJobsIsolationAndRetry(t *testing.T) {
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("requires disposable database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := New(db)
	if e = s.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	s.Register("test", func(ctx context.Context, uid, aid, id string, raw json.RawMessage) (Outcome, error) {
		if uid != "job-user" || aid != "job-workspace" {
			t.Error("owner changed")
		}
		n := calls.Add(1)
		if n > 1 {
			var payload struct {
				Resume bool `json:"resume"`
			}
			if err := json.Unmarshal(raw, &payload); err != nil || !payload.Resume {
				t.Error("resume payload not persisted")
			}
		}
		var next json.RawMessage
		if n == 1 {
			next = json.RawMessage(`{"resume":true}`)
		}
		return Outcome{Value: map[string]int{"calls": int(n)}, Pending: n == 1, Delay: time.Second, NextPayload: next}, nil
	})
	start := time.Now()
	job, e := s.Submit(ctx, "job-user", "job-workspace", "test", []Item{{ID: "one"}})
	if e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("slow job acceptance")
	}
	if _, e = s.Get(ctx, "other-user", "job-workspace", job.ID); e == nil {
		t.Fatal("cross-user job read")
	}
	s.Start(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, e := s.Get(ctx, "job-user", "job-workspace", job.ID)
		if e != nil {
			t.Fatal(e)
		}
		if j.Status == "done" {
			if j.Completed != 1 || calls.Load() != 2 {
				t.Fatal("pending item lost or repeated")
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatal("job not resumed")
	}
	if _, e = s.Retry(ctx, "job-user", "job-workspace", job.ID); e != nil {
		t.Fatal(e)
	}
	time.Sleep(400 * time.Millisecond)
	if calls.Load() != 2 {
		t.Fatal("successful item replayed")
	}
}
