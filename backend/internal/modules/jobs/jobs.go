// Package jobs runs durable, workspace-scoped operations with fixed targets.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Outcome struct {
	NextPayload json.RawMessage
	Value       any
	Pending     bool
	Delay       time.Duration
}
type Handler func(context.Context, string, string, string, json.RawMessage) (Outcome, error)
type Item struct {
	ID      string          `json:"id"`
	Status  string          `json:"status"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
	Payload json.RawMessage `json:"-"`
}
type Job struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Total     int       `json:"total"`
	Completed int       `json:"completed"`
	Failed    int       `json:"failed"`
	Items     []Item    `json:"items"`
	CreatedAt time.Time `json:"createdAt"`
}
type Service struct {
	db       *pgxpool.Pool
	handlers map[string]Handler
	once     sync.Once
}

func New(db *pgxpool.Pool) *Service                { return &Service{db: db, handlers: map[string]Handler{}} }
func (s *Service) Register(kind string, h Handler) { s.handlers[kind] = h }
func (s *Service) EnsureSchema(ctx context.Context) error {
	_, e := s.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS operation_jobs(id text PRIMARY KEY,user_id text NOT NULL,admin_account_id text NOT NULL,kind text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
 CREATE INDEX IF NOT EXISTS idx_operation_jobs_workspace ON operation_jobs(user_id,admin_account_id,created_at DESC);
 CREATE TABLE IF NOT EXISTS operation_items(job_id text NOT NULL REFERENCES operation_jobs(id) ON DELETE CASCADE,id text NOT NULL,status text NOT NULL DEFAULT 'queued',payload jsonb NOT NULL,result jsonb NOT NULL DEFAULT 'null',error text NOT NULL DEFAULT '',next_run timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(job_id,id));
 CREATE INDEX IF NOT EXISTS idx_operation_items_due ON operation_items(next_run) WHERE status IN('queued','waiting');`)
	return e
}
func (s *Service) Submit(ctx context.Context, uid, aid, kind string, items []Item) (Job, error) {
	if s.handlers[kind] == nil || len(items) == 0 || len(items) > 10000 {
		return Job{}, fmt.Errorf("invalid operation or targets")
	}
	buf := make([]byte, 16)
	if _, e := rand.Read(buf); e != nil {
		return Job{}, e
	}
	id := "job_" + hex.EncodeToString(buf)
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return Job{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `INSERT INTO operation_jobs(id,user_id,admin_account_id,kind) VALUES($1,$2,$3,$4)`, id, uid, aid, kind); e != nil {
		return Job{}, e
	}
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		if item.ID == "" {
			return Job{}, fmt.Errorf("missing target")
		}
		if item.Payload == nil {
			item.Payload = json.RawMessage(`{}`)
		}
		rows = append(rows, []any{id, item.ID, []byte(item.Payload)})
	}
	if _, e = tx.CopyFrom(ctx, pgx.Identifier{"operation_items"}, []string{"job_id", "id", "payload"}, pgx.CopyFromRows(rows)); e != nil {
		return Job{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Job{}, e
	}
	return s.Get(ctx, uid, aid, id)
}
func (s *Service) Get(ctx context.Context, uid, aid, id string) (Job, error) {
	j := Job{Items: []Item{}}
	err := s.db.QueryRow(ctx, `SELECT id,kind,created_at FROM operation_jobs WHERE id=$1 AND user_id=$2 AND admin_account_id=$3`, id, uid, aid).Scan(&j.ID, &j.Kind, &j.CreatedAt)
	if err != nil {
		return j, err
	}
	rows, err := s.db.Query(ctx, `SELECT id,status,result,error FROM operation_items WHERE job_id=$1 ORDER BY id`, id)
	if err != nil {
		return j, err
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		if err = rows.Scan(&it.ID, &it.Status, &it.Result, &it.Error); err != nil {
			return j, err
		}
		j.Items = append(j.Items, it)
		j.Total++
		if it.Status == "done" {
			j.Completed++
		}
		if it.Status == "failed" || it.Status == "cancelled" {
			j.Failed++
		}
	}
	j.Status = "running"
	if j.Completed+j.Failed == j.Total {
		j.Status = "done"
		if j.Failed > 0 {
			j.Status = "partial_failure"
		}
	}
	return j, rows.Err()
}

func (s *Service) List(ctx context.Context, uid, aid string) ([]Job, error) {
	rows, e := s.db.Query(ctx, `SELECT id FROM operation_jobs WHERE user_id=$1 AND admin_account_id=$2 ORDER BY created_at DESC LIMIT 12`, uid, aid)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Job{}
	for _, id := range ids {
		j, e := s.Get(ctx, uid, aid, id)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, nil
}
func (s *Service) Retry(ctx context.Context, uid, aid, id string) (Job, error) {
	if _, e := s.Get(ctx, uid, aid, id); e != nil {
		return Job{}, e
	}
	_, e := s.db.Exec(ctx, `UPDATE operation_items SET status='queued',error='',next_run=now() WHERE job_id=$1 AND status='failed'`, id)
	if e != nil {
		return Job{}, e
	}
	return s.Get(ctx, uid, aid, id)
}
func (s *Service) Cancel(ctx context.Context, uid, aid, id string) error {
	if _, e := s.Get(ctx, uid, aid, id); e != nil {
		return e
	}
	// Waiting items may own accepted media tasks. Their handler must finish or
	// reconcile those tasks and clean up credentials before they can terminate.
	_, e := s.db.Exec(ctx, `UPDATE operation_items SET status='cancelled',updated_at=now() WHERE job_id=$1 AND status='queued'`, id)
	return e
}
func (s *Service) Start(ctx context.Context) {
	s.once.Do(func() {
		// A crashed mutation is never replayed blindly. Read back or explicitly retry
		// an interrupted item; media generation is independently durable/idempotent.
		resumable := []string{"health_preview", "health_refresh", "health_probe", "model_catalog", "manual_model_check", "manual_upstream_model", "key_security", "key_security_bindings", "managed_key_binding", "membership_priority", "upstream_api_sync"}
		_, _ = s.db.Exec(ctx, `UPDATE operation_items i SET status=CASE WHEN j.kind=ANY($1::text[]) THEN 'waiting' ELSE 'failed' END,error=CASE WHEN j.kind=ANY($1::text[]) THEN '' ELSE '服务重启，操作结果待核对；核对后可重试' END,updated_at=now() FROM operation_jobs j WHERE i.job_id=j.id AND i.status='running'`, resumable)
		for i := 0; i < 4; i++ {
			go func() {
				timer := time.NewTicker(250 * time.Millisecond)
				defer timer.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-timer.C:
						s.step(ctx)
					}
				}
			}()
		}
	})
}
func (s *Service) step(ctx context.Context) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	var jobID, id, uid, aid, kind string
	var payload []byte
	e = tx.QueryRow(ctx, `SELECT i.job_id,i.id,j.user_id,j.admin_account_id,j.kind,i.payload FROM operation_items i JOIN operation_jobs j ON j.id=i.job_id WHERE i.status IN('queued','waiting') AND i.next_run<=now() ORDER BY i.next_run FOR UPDATE OF i SKIP LOCKED LIMIT 1`).Scan(&jobID, &id, &uid, &aid, &kind, &payload)
	if e != nil {
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE operation_items SET status='running',updated_at=now() WHERE job_id=$1 AND id=$2`, jobID, id); e != nil {
		return
	}
	if e = tx.Commit(ctx); e != nil {
		return
	}
	h := s.handlers[kind]
	if h == nil {
		return
	}
	run, cancel := context.WithTimeout(ctx, 4*time.Minute)
	out, err := h(run, uid, aid, id, payload)
	cancel()
	status, message, delay := "done", "", out.Delay
	if delay < time.Second {
		delay = 2 * time.Second
	}
	if out.Pending {
		status = "waiting"
	}
	if err != nil {
		status = "failed"
		message = err.Error()
	}
	raw, e := json.Marshal(out.Value)
	if e != nil {
		raw = []byte(`null`)
		status = "failed"
		message = "无法编码操作结果"
	}
	_, _ = s.db.Exec(ctx, `UPDATE operation_items SET status=$3,result=$4,error=$5,next_run=now()+$6::interval,updated_at=now(),payload=coalesce($7::jsonb,payload) WHERE job_id=$1 AND id=$2`, jobID, id, status, raw, message, fmt.Sprintf("%f seconds", delay.Seconds()), []byte(out.NextPayload))
}

// Keep pgx imported here so callers can distinguish a missing/cross-workspace job.
var ErrNotFound = pgx.ErrNoRows
