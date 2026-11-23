package channel_monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

const upstreamStatusSource = "upstream"

func (s *Service) SetModelHealth(h modelHealthReader) { s.health = h }
func (r *Repository) ensureModelTargets(ctx context.Context) error {
	_, e := r.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS channel_monitor_model_states(rule_id text NOT NULL,group_id text NOT NULL,fingerprint text NOT NULL,state_json jsonb NOT NULL,updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(rule_id,group_id,fingerprint));
 INSERT INTO channel_monitor_model_states SELECT rule_id,group_id,state_json->>'fingerprint',state_json,updated_at FROM channel_monitor_group_states WHERE coalesce(state_json->>'fingerprint','')<>'' AND coalesce(jsonb_typeof(state_json->'models'),'null')<>'array' ON CONFLICT DO NOTHING;`)
	return e
}
func (r *Repository) modelStates(ctx context.Context, id string) (map[string]TargetState, error) {
	rows, e := r.db.Query(ctx, `SELECT state_json FROM channel_monitor_model_states WHERE rule_id=$1`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]TargetState{}
	for rows.Next() {
		var b []byte
		var t TargetState
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &t); e != nil {
			return nil, e
		}
		out[t.GroupID+"|"+t.Fingerprint] = t
	}
	return out, rows.Err()
}
func (r *Repository) saveModel(ctx context.Context, t TargetState) error {
	b, e := json.Marshal(t)
	if e != nil {
		return e
	}
	_, e = r.db.Exec(ctx, `INSERT INTO channel_monitor_model_states VALUES($1,$2,$3,$4,now()) ON CONFLICT(rule_id,group_id,fingerprint) DO UPDATE SET state_json=EXCLUDED.state_json,updated_at=now()`, t.RuleID, t.GroupID, t.Fingerprint, b)
	return e
}
func selectedModelConfigs(rule Rule, conn my_sites.RealConnection, state *my_sites.State, config TestModelConfig, gid string) []TestModelGroupConfig {
	if rule.TestModelID == "" {
		out := []TestModelGroupConfig{}
		for _, v := range config.GroupModels {
			if v.GroupID == gid {
				out = append(out, v)
			}
		}
		if len(out) > 0 {
			// Dispatch follows the first configured model for the group.
			return out[:1]
		}
	}
	v := effectiveTestOptionsForGroup(rule, conn, state, config, gid)
	return []TestModelGroupConfig{{GroupID: gid, ModelID: v.ModelID, Mode: v.Mode, Prompt: v.Prompt, Size: v.Size, Seconds: v.Seconds, Adapter: v.Adapter, PresetModel: v.PresetModel, IntervalMinutes: rule.CheckIntervalMinutes}}
}
func aggregateModels(rule Rule, gid string, models []TargetState) TargetState {
	t := TargetState{RuleID: rule.ID, GroupID: gid, Status: StatusUnknown, Message: "等待模型检测", Models: models}
	if len(models) == 0 {
		return t
	}
	t = models[0]
	t.Models = models
	allFailed, anyHealthy, anyPending, balanceBlocked := true, false, false, false
	for _, m := range models {
		if m.Status != StatusFailed {
			allFailed = false
		}
		if m.Status == StatusHealthy {
			anyHealthy = true
			t = m
			t.Models = models
		}
		if m.Status == StatusGenerating {
			anyPending = true
		}
		if m.Status == StatusBalancePaused {
			balanceBlocked = true
		}
		if m.NextCheckAt != nil && (t.NextCheckAt == nil || m.NextCheckAt.Before(*t.NextCheckAt)) {
			v := *m.NextCheckAt
			t.NextCheckAt = &v
		}
	}
	if balanceBlocked {
		t.Status = StatusBalancePaused
		t.ConsecutiveFailures = 0
		t.Message = "余额低于阈值"
	} else if anyHealthy {
		t.Status = StatusHealthy
		for _, m := range models {
			if m.Status != StatusHealthy {
				t.Message = "部分模型可用；展开查看各模型状态"
				break
			}
		}
	} else if anyPending {
		t.Status = StatusGenerating
		t.Message = "媒体任务生成中"
	} else if !allFailed {
		t.Status = StatusUnknown
		t.ConsecutiveFailures = 0
		t.Message = "部分模型状态待确认；保持现有调度"
	} else {
		t.Status = StatusFailed
		minimum := models[0].ConsecutiveFailures
		for _, m := range models {
			if m.ConsecutiveFailures < minimum {
				minimum = m.ConsecutiveFailures
			}
		}
		t.ConsecutiveFailures = minimum
	}
	return t
}
func (s *Service) runRule(ctx context.Context, rule Rule, reason string) (Result, error) {
	repo, ok := s.store.(*Repository)
	if !ok {
		return s.runRuleSingle(ctx, rule, reason)
	}
	conn, e := s.conns.GetRealConnection(ctx, rule.ConnectionID, rule.UserID, rule.AdminAccountID)
	if e != nil {
		return Result{}, e
	}
	if conn == nil {
		return Result{}, fmt.Errorf("绑定不存在")
	}
	unlock, e := s.lockRuleAccount(ctx, rule)
	if e != nil {
		return Result{}, e
	}
	defer unlock()
	if latest, err := s.store.GetRule(ctx, rule.ID); err != nil {
		return Result{}, err
	} else if latest != nil {
		gid := rule.MonitorGroupID
		rule = *latest
		rule.MonitorGroupID = gid
	}
	state, e := s.workspaceState(ctx, rule.UserID, rule.AdminAccountID)
	if e != nil {
		return Result{}, e
	}
	config, e := s.ensureTestModelConfig(ctx, rule.UserID, rule.AdminAccountID)
	if e != nil {
		return Result{}, e
	}
	site, e := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
	if e != nil {
		return Result{}, e
	}
	if site == nil {
		return Result{}, fmt.Errorf("上游不存在")
	}
	if rule.Enabled && !rule.ManualPaused {
		fresh, _ := s.refreshSiteBalanceIfStale(ctx, site, config.BalanceRefreshIntervalMinutes)
		if fresh != nil {
			site = fresh
		}
	}
	stored, e := repo.modelStates(ctx, rule.ID)
	if e != nil {
		return Result{}, e
	}
	all, e := repo.ListTargetStates(ctx, rule.UserID, rule.AdminAccountID)
	if e != nil {
		return Result{}, e
	}
	if all[rule.ID] == nil {
		all[rule.ID] = map[string]TargetState{}
	}
	groups := uniqueRuleIDs(conn.OwnGroupIDs)
	if len(groups) == 0 {
		groups = []string{""}
	}
	if rule.MonitorGroupID != "" {
		if !containsString(groups, rule.MonitorGroupID) {
			return Result{}, fmt.Errorf("分组不属于当前绑定")
		}
		groups = []string{rule.MonitorGroupID}
	}
	results := []Result{}
	next := time.Now().Add(time.Duration(rule.CheckIntervalMinutes) * time.Minute)
	for _, gid := range groups {
		models := []TargetState{}
		for _, selected := range selectedModelConfigs(rule, *conn, state, config, gid) {
			o := AccountTestOptions{ModelID: selected.ModelID, Mode: normalizeTestMode(selected.Mode), Prompt: selected.Prompt, Size: selected.Size, Seconds: selected.Seconds, Adapter: selected.Adapter, PresetModel: selected.PresetModel}
			fp := configFingerprint(*conn, o)
			target, exists := stored[gid+"|"+fp]
			if !exists {
				target = TargetState{RuleID: rule.ID, GroupID: gid, Fingerprint: fp, Model: o.ModelID, Mode: o.Mode, Status: StatusUnknown}
			}
			interval := selected.IntervalMinutes
			if interval <= 0 {
				interval = rule.CheckIntervalMinutes
			}
			if interval <= 0 {
				switch o.Mode {
				case "video":
					interval = 30
				case "image", "audio":
					interval = 10
				default:
					interval = 2
				}
			}
			sameSource := target.Source != "active"
			if !sameSource {
				target.SourceCheckedAt, target.LatencyMS = nil, nil
				target.ConsecutiveFailures = 0
			}
			if sameSource && target.NextCheckAt != nil && time.Now().Before(*target.NextCheckAt) && reason != "manual" {
				models = append(models, target)
				continue
			}
			started := time.Now()
			check := AccountTestResult{}
			observation := (*time.Time)(nil)
			source := upstreamStatusSource
			skipSample := false
			switch {
			case rule.ManualPaused:
				target.Status = StatusManualPaused
				target.Message = "手动停止中，自动恢复已暂停"
				skipSample = true
			case !rule.Enabled && reason != "manual":
				models = append(models, target)
				continue
			case o.ModelID == "":
				target.Status = StatusConfigError
				target.Message = "未配置检测模型"
				target.ConsecutiveFailures = 0
				skipSample = true
			case convertedBalance(site) != nil && *convertedBalance(site) < rule.BalanceThreshold:
				target.Status = StatusBalancePaused
				target.Message = "余额低于阈值"
				target.ConsecutiveFailures = 0
			default:
				passive, found := upstream.ModelHealth{}, false
				if s.health != nil {
					passive, found = s.health.MatchConfigured(ctx, site.ID, conn.UpstreamGroupID, conn.UpstreamGroupName, o.ModelID)
				}
				if found {
					source = passive.Source
					observation = passive.CheckedAt
					check = AccountTestResult{Success: passive.Status == StatusHealthy, Model: o.ModelID, Message: "上游已报告模型状态"}
					if passive.LatencyMS != nil {
						check.LatencyMS = *passive.LatencyMS
					}
					if target.SourceCheckedAt != nil && observation != nil && observation.Before(*target.SourceCheckedAt) {
						skipSample = true
						source = target.Source
						observation = target.SourceCheckedAt
					}
					if target.Source == source && target.SourceCheckedAt != nil && observation != nil && !observation.After(*target.SourceCheckedAt) {
						skipSample = true
					}
				}
				if !found {
					check.Inconclusive = true
					check.Message = "上游未提供该模型的有效状态"
					target.LatencyMS = nil
				}
				if !skipSample {
					switch {
					case check.Inconclusive:
						target.Status = StatusUnknown
						target.Message = check.Message
						target.ConsecutiveFailures = 0
					case check.Success:
						target.Status = StatusHealthy
						target.Message = check.Message
						target.ConsecutiveFailures = 0
						lat := check.LatencyMS
						target.LatencyMS = &lat
					default:
						target.Status = StatusFailed
						target.Message = check.Message
						target.ConsecutiveFailures++
					}
				}
			}
			now := time.Now()
			due := now.Add(time.Duration(interval) * time.Minute)
			target.NextCheckAt = &due
			if due.Before(next) {
				next = due
			}
			target.Source = source
			if observation != nil {
				target.SourceCheckedAt = observation
			}
			if !skipSample {
				target.CheckedAt = &now
			}
			if e = repo.saveModel(ctx, target); e != nil {
				return Result{}, e
			}
			models = append(models, target)
			result := Result{ID: newResultID(), RuleID: rule.ID, ConnectionID: conn.ID, GroupID: gid, TaskID: check.TaskID, Model: o.ModelID, Status: target.Status, Success: target.Status == StatusHealthy, Message: target.Message, Action: reason, StartedAt: started, FinishedAt: now, CreatedAt: now, LatencyMS: target.LatencyMS}
			if !skipSample && target.Status != StatusGenerating {
				if e = repo.AddResult(ctx, result); e != nil {
					return Result{}, e
				}
			}
			results = append(results, result)
		}
		agg := aggregateModels(rule, gid, models)
		if e = repo.SaveTargetState(ctx, agg); e != nil {
			return Result{}, e
		}
		all[rule.ID][gid] = agg
	}
	if e = s.reconcileTargetDispatch(ctx, &rule, *conn, state, config, all); e != nil {
		return Result{}, e
	}
	groupsState := []TargetState{}
	for _, gid := range uniqueRuleIDs(conn.OwnGroupIDs) {
		if t, ok := all[rule.ID][gid]; ok {
			groupsState = append(groupsState, t)
		}
	}
	if len(groupsState) == 0 {
		for _, t := range all[rule.ID] {
			groupsState = append(groupsState, t)
		}
	}
	agg := aggregateModels(rule, "", groupsState)
	rule.LastStatus = agg.Status
	if rule.LastAppliedSchedulable != nil && !*rule.LastAppliedSchedulable && rule.SchedulableManaged && agg.Status == StatusFailed {
		rule.LastStatus = StatusAutoPaused
	}
	rule.LastMessage = agg.Message
	rule.ConsecutiveFailures = agg.ConsecutiveFailures
	rule.LastLatencyMS = agg.LatencyMS
	now := time.Now()
	rule.LastCheckedAt = &now
	rule.NextCheckAt = &next
	rule.UpdatedAt = now
	if e = s.store.UpdateRule(ctx, rule); e != nil {
		return Result{}, e
	}
	return Result{RuleID: rule.ID, ConnectionID: conn.ID, Status: agg.Status, Success: agg.Status == StatusHealthy, Message: agg.Message, GroupResults: results}, nil
}
