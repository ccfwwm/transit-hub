package channel_monitor

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type TargetStore interface {
	ListTargetStates(context.Context, string, string) (map[string]map[string]TargetState, error)
	SaveTargetState(context.Context, TargetState) error
}

func (r *Repository) EnsureWorkspaceRules(ctx context.Context, userID, adminID string) error {
	_, err := r.db.Exec(ctx, `INSERT INTO channel_monitor_rules(id,user_id,admin_account_id,connection_id,next_check_at)
 SELECT id,user_id,workspace_admin_account_id,id,now() FROM real_connections rc WHERE user_id=$1 AND workspace_admin_account_id=$2
 AND NOT EXISTS(SELECT 1 FROM channel_monitor_rules r WHERE r.id=rc.id) ON CONFLICT(id) DO NOTHING`, userID, adminID)
	return err
}

func (r *Repository) ensureTargetSchema(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS channel_monitor_group_states (
 rule_id text NOT NULL, group_id text NOT NULL, state_json jsonb NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(rule_id,group_id));
 CREATE INDEX IF NOT EXISTS idx_channel_monitor_results_target_created ON channel_monitor_results(rule_id,group_id,created_at DESC);
 ALTER TABLE channel_monitor_media_tasks ADD COLUMN IF NOT EXISTS options_json jsonb NOT NULL DEFAULT '{}'::jsonb;
 CREATE INDEX IF NOT EXISTS idx_channel_monitor_media_tasks_active ON channel_monitor_media_tasks(rule_id,status);`)
	return err
}

func (r *Repository) ListTargetStates(ctx context.Context, userID, adminID string) (map[string]map[string]TargetState, error) {
	rows, err := r.db.Query(ctx, `SELECT s.state_json FROM channel_monitor_group_states s JOIN channel_monitor_rules r ON r.id=s.rule_id WHERE r.user_id=$1 AND r.admin_account_id=$2`, userID, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]map[string]TargetState{}
	for rows.Next() {
		var raw []byte
		var state TargetState
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &state); err != nil {
			return nil, err
		}
		if result[state.RuleID] == nil {
			result[state.RuleID] = map[string]TargetState{}
		}
		result[state.RuleID][state.GroupID] = state
	}
	return result, rows.Err()
}

func (r *Repository) SaveTargetState(ctx context.Context, state TargetState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO channel_monitor_group_states(rule_id,group_id,state_json) VALUES($1,$2,$3) ON CONFLICT(rule_id,group_id) DO UPDATE SET state_json=EXCLUDED.state_json,updated_at=now()`, state.RuleID, state.GroupID, raw)
	return err
}

// ClaimMediaTask registers before the HTTP POST. Only one caller can submit,
// including concurrent processes and requests after a restart.
func (r *Repository) ClaimMediaTask(ctx context.Context, task MediaTask, eligibleBefore time.Time) (bool, error) {
	raw, err := json.Marshal(task.Options)
	if err != nil {
		return false, err
	}
	result, err := r.db.Exec(ctx, `INSERT INTO channel_monitor_media_tasks(key,user_id,admin_account_id,rule_id,group_id,model_id,mode,config_fingerprint,task_id,status,message,created_at,updated_at,options_json)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,'','submitting','',now(),now(),$10) ON CONFLICT(key) DO UPDATE SET task_id='',status='submitting',message='',created_at=now(),updated_at=now(),options_json=EXCLUDED.options_json
 WHERE channel_monitor_media_tasks.status IN ('success','failed') AND channel_monitor_media_tasks.updated_at < $9`, task.Key, task.UserID, task.AdminAccountID, task.RuleID, task.GroupID, task.ModelID, task.Mode, task.ConfigFingerprint, eligibleBefore, raw)
	return result.RowsAffected() == 1, err
}

func (r *Repository) ActiveMediaTask(ctx context.Context, ruleID string) (*MediaTask, error) {
	var key string
	err := r.db.QueryRow(ctx, `SELECT key FROM channel_monitor_media_tasks WHERE rule_id=$1 AND status IN ('submitting','pending','unknown','timeout') ORDER BY created_at LIMIT 1`, ruleID).Scan(&key)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetMediaTask(ctx, key)
}

func (r *Repository) ListPendingMediaTasks(ctx context.Context) ([]MediaTask, error) {
	rows, err := r.db.Query(ctx, `SELECT key FROM channel_monitor_media_tasks WHERE status='pending' AND updated_at < now()-interval '10 seconds' ORDER BY updated_at LIMIT 500`)
	if err != nil {
		return nil, err
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	tasks := []MediaTask{}
	for _, key := range keys {
		task, e := r.GetMediaTask(ctx, key)
		if e != nil {
			return nil, e
		}
		if task != nil {
			tasks = append(tasks, *task)
		}
	}
	return tasks, nil
}

func (r *Repository) TargetHistory(ctx context.Context, ruleID, groupID string, limit int) ([]Result, error) {
	rows, err := r.db.Query(ctx, `SELECT id,rule_id,connection_id,group_id,task_id,status,success,message,latency_ms,model,action,started_at,finished_at,created_at FROM channel_monitor_results WHERE rule_id=$1 AND group_id=$2 ORDER BY created_at DESC LIMIT $3`, ruleID, groupID, clampInt(limit, 1, 60))
	if err != nil {
		return nil, err
	}
	return scanResults(rows)
}

// Lateral index scans bound history work to 60 points per displayed target.
func (r *Repository) PageHistory(ctx context.Context, ruleIDs, groupIDs []string) (map[string][]Result, error) {
	rows, err := r.db.Query(ctx, `SELECT h.id,h.rule_id,h.connection_id,h.group_id,h.task_id,h.status,h.success,'' AS message,h.latency_ms,h.model,h.action,h.started_at,h.finished_at,h.created_at
 FROM unnest($1::text[],$2::text[]) AS target(rule_id,group_id) CROSS JOIN LATERAL (
 SELECT * FROM channel_monitor_results WHERE rule_id=target.rule_id AND group_id=target.group_id ORDER BY created_at DESC LIMIT 60) h`, ruleIDs, groupIDs)
	if err != nil {
		return nil, err
	}
	results, err := scanResults(rows)
	if err != nil {
		return nil, err
	}
	out := map[string][]Result{}
	for _, v := range results {
		out[v.RuleID+"|"+v.GroupID] = append(out[v.RuleID+"|"+v.GroupID], v)
	}
	return out, nil
}
