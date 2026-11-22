package channel_monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"transithub/backend/internal/modules/jobs"
	media "transithub/backend/internal/modules/monitor_media"
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
func (s *Service) manualModelCheck(ctx context.Context, uid, aid, id string, raw json.RawMessage) (jobs.Outcome, error) {
	var request struct{ RuleID, Model string }
	if e := json.Unmarshal(raw, &request); e != nil {
		return jobs.Outcome{}, e
	}
	rule, e := s.requireRule(ctx, request.RuleID, uid, aid)
	if e != nil {
		return jobs.Outcome{}, e
	}
	unlock, e := s.lockRuleAccount(ctx, rule)
	if e != nil {
		return jobs.Outcome{}, e
	}
	defer unlock()
	repo := s.store.(*Repository)
	values, e := repo.manualChecks(ctx, uid, aid, rule.ID)
	if e != nil {
		return jobs.Outcome{}, e
	}
	v, ok := values[request.Model]
	if !ok {
		return jobs.Outcome{Pending: true, Delay: time.Second}, nil
	}
	if v.Status != "generating" {
		return jobs.Outcome{Value: v}, s.applyConfiguredManualCheck(ctx, rule, v)
	}
	conn, e := s.conns.GetRealConnection(ctx, rule.ConnectionID, uid, aid)
	if e != nil || conn == nil {
		return jobs.Outcome{}, fmt.Errorf("绑定不存在")
	}
	if configFingerprint(*conn, v.Options) != v.Fingerprint {
		return jobs.Outcome{}, fmt.Errorf("Key 绑定已变更，请重新检查")
	}
	site, e := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
	if e != nil || site == nil {
		return jobs.Outcome{}, fmt.Errorf("上游不存在")
	}
	// Mark text submissions before the external request. A crash/retry cannot
	// blindly replay a request whose acceptance was not recorded.
	var check AccountTestResult
	if v.Options.Mode == "text" {
		if v.Message == "submitting" {
			v.Status = StatusUnknown
			v.Message = "上次请求受理待确认，未重复调用"
		} else {
			v.Message = "submitting"
			b, _ := json.Marshal(v)
			if _, e = repo.db.Exec(ctx, `UPDATE channel_monitor_manual_checks SET payload=$4,updated_at=now() WHERE rule_id=$1 AND user_id=$2 AND admin_account_id=$3 AND model=$5`, rule.ID, uid, aid, b, v.Model); e != nil {
				return jobs.Outcome{}, e
			}
			check, e = s.platform.ProbeUpstreamConnection(ctx, *site, *conn, v.Options)
		}
	} else {
		target := TargetState{RuleID: rule.ID, GroupID: "", Fingerprint: v.Fingerprint, Model: v.Model, Mode: v.Options.Mode, CheckedAt: &v.StartedAt}
		oneRule := rule
		oneRule.CheckIntervalMinutes = 1
		check, e = s.mediaCheck(ctx, oneRule, *conn, *site, target, v.Options)
	}
	if check.Pending {
		return jobs.Outcome{Value: map[string]string{"status": "generating"}, Pending: true, Delay: 10 * time.Second}, nil
	}
	if v.Status == "generating" {
		v.Status = StatusUnknown
		v.Message = check.Message
		if e != nil {
			v.Message = "检测请求异常，结果待确认"
		} else if check.Inconclusive && (strings.Contains(check.Message, "HTTP 403") || strings.Contains(check.Message, "HTTP 401")) {
			v.Status = StatusConfigError
			v.Inconclusive = true
		} else if check.Uncertain || check.Inconclusive {
		} else if check.Success {
			v.Status = StatusHealthy
		} else {
			v.Status = StatusFailed
		}
		if v.Options.Mode == "image" && v.Status == StatusUnknown {
			v.Status = StatusFailed
			v.Inconclusive = check.Inconclusive
			if e != nil {
				v.Message = "图片检测失败：请求异常，未取得有效图片"
			}
		}
	}
	now := time.Now().UTC()
	v.CheckedAt = &now
	if check.LatencyMS > 0 {
		v.LatencyMS = &check.LatencyMS
	}
	if v.Message == "" {
		v.Message = map[bool]string{true: "实际调用通过", false: "实际调用未通过"}[check.Success]
	}
	b, _ := json.Marshal(v)
	_, e = repo.db.Exec(ctx, `UPDATE channel_monitor_manual_checks SET payload=$4,updated_at=now() WHERE rule_id=$1 AND user_id=$2 AND admin_account_id=$3 AND model=$5`, rule.ID, uid, aid, b, v.Model)
	if e == nil {
		e = s.applyConfiguredManualCheck(ctx, rule, v)
	}
	return jobs.Outcome{Value: v}, e
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
		if o.ModelID != v.Model {
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
	return s.RefreshReadModel(ctx, rule.UserID, rule.AdminAccountID)
}

func (h *Handler) checkModel(w http.ResponseWriter, r *http.Request) {
	uid, aid, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	rule, e := h.service.requireRule(r.Context(), r.PathValue("id"), uid, aid)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	var req struct {
		Model       string `json:"model"`
		Mode        string `json:"mode"`
		Adapter     string `json:"adapter"`
		PresetModel string `json:"presetModel"`
		Size        string `json:"size"`
		Seconds     int    `json:"seconds"`
	}
	if httpjson.Decode(r, &req) != nil || strings.TrimSpace(req.Model) == "" || len(req.Model) > 300 {
		httpjson.WriteError(w, 400, "请选择模型")
		return
	}
	if req.Mode != "text" && req.Mode != "image" && req.Mode != "audio" && req.Mode != "video" {
		httpjson.WriteError(w, 400, "请选择检测类型")
		return
	}
	o := AccountTestOptions{ModelID: req.Model, Mode: req.Mode, Adapter: req.Adapter, PresetModel: req.PresetModel, Size: req.Size, Seconds: req.Seconds, OneShot: true}
	if o.Mode != "text" {
		if _, _, e = media.Resolve(media.Options{Model: o.ModelID, Mode: o.Mode, Adapter: o.Adapter, PresetModel: o.PresetModel, Size: o.Size, Seconds: o.Seconds}); e != nil {
			httpjson.WriteError(w, 400, e.Error())
			return
		}
	}
	conn, e := h.service.conns.GetRealConnection(r.Context(), rule.ConnectionID, uid, aid)
	if e != nil || conn == nil {
		httpjson.WriteError(w, 404, "绑定不存在")
		return
	}
	repo, ok := h.service.store.(*Repository)
	if !ok || h.service.jobs == nil {
		httpjson.WriteError(w, 503, "任务服务未就绪")
		return
	}
	tx, e := repo.db.Begin(r.Context())
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "manual-check|"+rule.ID+"|"+req.Model); e != nil {
		writeMonitorError(w, e)
		return
	}
	var previous []byte
	var old ManualModelCheck
	if tx.QueryRow(r.Context(), `SELECT payload FROM channel_monitor_manual_checks WHERE rule_id=$1 AND model=$2`, rule.ID, req.Model).Scan(&previous) == nil && json.Unmarshal(previous, &old) == nil {
		if old.Status == "generating" || time.Since(old.StartedAt) < time.Minute {
			job, err := h.service.jobs.Get(r.Context(), uid, aid, old.JobID)
			if err == nil && (job.Status == "running" || time.Since(old.StartedAt) < time.Minute) {
				httpjson.Write(w, 202, job)
				return
			}
		}
	}
	v := ManualModelCheck{Model: req.Model, Status: "generating", Options: o, Fingerprint: configFingerprint(*conn, o), StartedAt: time.Now().UTC()}
	raw, _ := json.Marshal(struct{ RuleID, Model string }{rule.ID, req.Model})
	job, e := h.service.jobs.Submit(r.Context(), uid, aid, "manual_model_check", []jobs.Item{{ID: rule.ID, Payload: raw}})
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	v.JobID = job.ID
	b, _ := json.Marshal(v)
	_, e = tx.Exec(r.Context(), `INSERT INTO channel_monitor_manual_checks(rule_id,user_id,admin_account_id,model,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(rule_id,model) DO UPDATE SET payload=EXCLUDED.payload,updated_at=now()`, rule.ID, uid, aid, req.Model, b)
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	httpjson.Write(w, 202, job)
}
