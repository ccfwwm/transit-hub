package channel_monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/httpjson"
)

type ManualModelCheck struct {
	Inconclusive bool               `json:"inconclusive,omitempty"`
	Model        string             `json:"model"`
	Status       string             `json:"status"`
	Message      string             `json:"message"`
	CheckedAt    *time.Time         `json:"checkedAt,omitempty"`
	LatencyMS    *int               `json:"latencyMs,omitempty"`
	JobID        string             `json:"jobId"`
	StartedAt    time.Time          `json:"startedAt"`
	Options      AccountTestOptions `json:"options"`
	Fingerprint  string             `json:"fingerprint"`
}

func (r *Repository) ensureManualChecks(ctx context.Context) error {
	_, e := r.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS channel_monitor_manual_checks(rule_id text NOT NULL,user_id text NOT NULL,admin_account_id text NOT NULL,model text NOT NULL,payload jsonb NOT NULL,updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(rule_id,model));`)
	if e != nil {
		return e
	}
	_, e = r.db.Exec(ctx, `UPDATE channel_monitor_manual_checks SET payload=payload||'{"status":"failed","inconclusive":true}'::jsonb WHERE payload->>'status'='unknown' AND payload->'options'->>'Mode'='image'`)
	return e
}
func (r *Repository) manualChecks(ctx context.Context, uid, aid, id string) (map[string]ManualModelCheck, error) {
	rows, e := r.db.Query(ctx, `SELECT payload FROM channel_monitor_manual_checks WHERE rule_id=$1 AND user_id=$2 AND admin_account_id=$3`, id, uid, aid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]ManualModelCheck{}
	for rows.Next() {
		var raw []byte
		var v ManualModelCheck
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		out[v.Model] = v
	}
	return out, rows.Err()
}
func (s *Service) registerManualModelChecks(j *jobs.Service) {
	j.Register("manual_model_check", s.manualModelCheck)
}
func (s *Service) manualModelCheck(context.Context, string, string, string, json.RawMessage) (jobs.Outcome, error) {
	return jobs.Outcome{}, fmt.Errorf("主动检测已关闭，请在上游 Key 详情中手动测试")
}

func (s *Service) RecordBoundModelTest(ctx context.Context, result upstream.BoundModelTest) error {
	repo := s.store.(*Repository)
	conn, err := s.conns.GetRealConnection(ctx, result.ConnectionID, result.UserID, result.WorkspaceID)
	if err != nil || conn == nil {
		return err
	}
	rule, err := s.store.EnsureRuleForConnection(ctx, result.UserID, result.WorkspaceID, *conn)
	if err != nil {
		return err
	}
	unlock, err := s.lockRuleAccount(ctx, rule)
	if err != nil {
		return err
	}
	defer unlock()
	conn, err = s.conns.GetRealConnection(ctx, result.ConnectionID, result.UserID, result.WorkspaceID)
	if err != nil || conn == nil {
		return err
	}
	if conn.UpstreamKeyID != result.KeyID || conn.UpstreamSiteID != result.SiteID {
		return nil
	}
	latest, err := s.store.GetRule(ctx, rule.ID)
	if err != nil || latest == nil {
		return err
	}
	options := AccountTestOptions{ModelID: result.Model, Mode: result.Mode, Adapter: result.Adapter}
	check := ManualModelCheck{Model: result.Model, Status: StatusHealthy, Message: "绑定 Key 实际调用成功",
		CheckedAt: &result.CheckedAt, LatencyMS: &result.LatencyMS, JobID: result.ID, Options: options, Fingerprint: configFingerprint(*conn, options)}
	payload, err := json.Marshal(check)
	if err != nil {
		return err
	}
	if _, err = repo.db.Exec(ctx, `INSERT INTO channel_monitor_manual_checks(rule_id,user_id,admin_account_id,model,payload,updated_at)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(rule_id,model) DO UPDATE SET payload=EXCLUDED.payload,updated_at=EXCLUDED.updated_at
		WHERE channel_monitor_manual_checks.updated_at<=EXCLUDED.updated_at`, rule.ID, result.UserID, result.WorkspaceID, result.Model, payload, result.CheckedAt); err != nil {
		return err
	}
	if err = s.applyConfiguredManualCheck(ctx, *latest, check); err != nil {
		return err
	}
	return s.RefreshReadModel(ctx, result.UserID, result.WorkspaceID)
}

// A one-shot check only participates in automation when it tests the model
// explicitly configured for that channel. Other catalog models are inspection.
func (s *Service) applyConfiguredManualCheck(ctx context.Context, rule Rule, v ManualModelCheck) error {
	if !rule.Enabled || rule.ManualPaused || v.Inconclusive || v.CheckedAt == nil || (v.Status != StatusHealthy && v.Status != StatusFailed) {
		return nil
	}
	repo := s.store.(*Repository)
	conn, e := s.conns.GetRealConnection(ctx, rule.ConnectionID, rule.UserID, rule.AdminAccountID)
	if e != nil || conn == nil {
		return e
	}
	if configFingerprint(*conn, v.Options) != v.Fingerprint {
		return nil
	}
	state, e := s.workspaceState(ctx, rule.UserID, rule.AdminAccountID)
	if e != nil {
		return e
	}
	config, e := s.ensureTestModelConfig(ctx, rule.UserID, rule.AdminAccountID)
	if e != nil {
		return e
	}
	all, e := repo.ListTargetStates(ctx, rule.UserID, rule.AdminAccountID)
	if e != nil {
		return e
	}
	if all[rule.ID] == nil {
		all[rule.ID] = map[string]TargetState{}
	}
	ids := uniqueRuleIDs(conn.OwnGroupIDs)
	if len(ids) == 0 {
		ids = []string{""}
	}
	changed := false
	for _, gid := range ids {
		o := effectiveTestOptionsForGroup(rule, *conn, state, config, gid).options()
		if o.ModelID != v.Model || v.Options.Mode != "" && normalizeTestMode(o.Mode) != normalizeTestMode(v.Options.Mode) {
			continue
		}
		t := targetFor(rule, *conn, gid, state, config, all[rule.ID])
		if t.SourceCheckedAt == nil || v.CheckedAt.After(*t.SourceCheckedAt) {
			t.RuleID = rule.ID
			t.GroupID = gid
			t.Fingerprint = configFingerprint(*conn, o)
			t.Model = v.Model
			t.Mode = o.Mode
			t.Status = v.Status
			t.Message = v.Message
			t.CheckedAt = v.CheckedAt
			t.LatencyMS = v.LatencyMS
			t.Source = "manual"
			t.SourceCheckedAt = v.CheckedAt
			if v.Status == StatusFailed {
				t.ConsecutiveFailures++
			} else {
				t.ConsecutiveFailures = 0
			}
			due := time.Now().Add(time.Duration(rule.CheckIntervalMinutes) * time.Minute)
			t.NextCheckAt = &due
			t.Models = nil
			if e = repo.saveModel(ctx, t); e != nil {
				return e
			}
			agg := aggregateModels(rule, gid, []TargetState{t})
			if e = repo.SaveTargetState(ctx, agg); e != nil {
				return e
			}
			all[rule.ID][gid] = agg
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if e = s.reconcileTargetDispatch(ctx, &rule, *conn, state, config, all); e != nil {
		return e
	}
	groups := []TargetState{}
	for _, gid := range ids {
		if t, ok := all[rule.ID][gid]; ok {
			groups = append(groups, t)
		}
	}
	agg := aggregateModels(rule, "", groups)
	rule.LastStatus = agg.Status
	rule.LastMessage = agg.Message
	rule.ConsecutiveFailures = agg.ConsecutiveFailures
	rule.LastCheckedAt = agg.CheckedAt
	rule.LastLatencyMS = agg.LatencyMS
	if rule.LastAppliedSchedulable != nil && !*rule.LastAppliedSchedulable && rule.SchedulableManaged && agg.Status == StatusFailed {
		rule.LastStatus = StatusAutoPaused
	}
	rule.UpdatedAt = time.Now()
	if e = s.store.UpdateRule(ctx, rule); e != nil {
		return e
	}
	return nil
}

func (h *Handler) checkModel(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.scope(r); err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.WriteError(w, http.StatusGone, "主动检测已关闭，请在上游 Key 详情中手动测试")
}
