package channel_monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	media "transithub/backend/internal/modules/monitor_media"
	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

func (o effectiveTestConfig) options() AccountTestOptions {
	return AccountTestOptions{ModelID: o.ModelID, Mode: o.Mode, Prompt: o.Prompt, Size: o.Size, Seconds: o.Seconds, Adapter: o.Adapter, PresetModel: o.PresetModel}
}
func configFingerprint(conn my_sites.RealConnection, o AccountTestOptions) string {
	raw, _ := json.Marshal([]any{conn.UpstreamSiteID, conn.UpstreamKey, conn.GroupType, o})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func targetFor(rule Rule, conn my_sites.RealConnection, groupID string, state *my_sites.State, config TestModelConfig, stored map[string]TargetState) TargetState {
	o := effectiveTestOptionsForGroup(rule, conn, state, config, groupID).options()
	fp := configFingerprint(conn, o)
	target, ok := stored[groupID]
	if !ok || target.Fingerprint != fp {
		target = TargetState{RuleID: rule.ID, GroupID: groupID, Fingerprint: fp, Status: StatusUnknown, Message: "配置已变更，等待检测", Model: o.ModelID, Mode: o.Mode}
	}
	return target
}

func (s *Service) lockRuleAccount(ctx context.Context, rule Rule) (func(), error) {
	conn, err := s.conns.GetRealConnection(ctx, rule.ConnectionID, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return nil, err
	}
	accountID := rule.ConnectionID
	if conn != nil && conn.AdminAccountID != "" {
		accountID = conn.AdminAccountID
	}
	key := workspaceKey(rule.UserID, rule.AdminAccountID) + "|" + accountID
	lock, _ := s.runLocks.LoadOrStore(key, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	var once sync.Once
	return func() { once.Do(mu.Unlock) }, nil
}

func (s *Service) runRule(ctx context.Context, rule Rule, reason string) (Result, error) {
	targets, ok := s.store.(TargetStore)
	if !ok {
		return s.runRuleLegacy(ctx, rule, reason)
	}
	conn, err := s.conns.GetRealConnection(ctx, rule.ConnectionID, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return Result{}, err
	}
	if conn == nil {
		return Result{}, requestError("admin.channelMonitor.errors.notFound")
	}
	// Serialize checks sharing an upstream dispatch account. Media work itself
	// runs on the independent queue; this lock only protects state transitions.
	unlock, err := s.lockRuleAccount(ctx, rule)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	latest, err := s.store.GetRule(ctx, rule.ID)
	if err != nil {
		return Result{}, err
	}
	if latest != nil {
		groupID := rule.MonitorGroupID
		rule = *latest
		rule.MonitorGroupID = groupID
	}
	state, err := s.workspaceState(ctx, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return Result{}, err
	}
	config, err := s.ensureTestModelConfig(ctx, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return Result{}, err
	}
	all, err := targets.ListTargetStates(ctx, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return Result{}, err
	}
	groups := uniqueRuleIDs(conn.OwnGroupIDs)
	if len(groups) == 0 {
		groups = []string{""}
	}
	if rule.MonitorGroupID != "" {
		if !containsString(groups, rule.MonitorGroupID) {
			return Result{}, requestError("admin.channelMonitor.errors.request")
		}
		groups = []string{rule.MonitorGroupID}
	}
	site, err := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
	if err != nil {
		return Result{}, err
	}
	if site == nil {
		return Result{}, requestError("admin.channelMonitor.errors.notFound")
	}
	balanceErr := error(nil)
	if rule.Enabled && !rule.ManualPaused {
		var fresh *upstream.Site
		fresh, balanceErr = s.refreshSiteBalanceIfStale(ctx, site, config.BalanceRefreshIntervalMinutes)
		if fresh != nil {
			site = fresh
		}
	}
	results := []Result{}
	dedup := map[string]struct {
		result AccountTestResult
		err    error
	}{}
	next := time.Now().Add(time.Duration(rule.CheckIntervalMinutes) * time.Minute)
	for _, groupID := range groups {
		target := targetFor(rule, *conn, groupID, state, config, all[rule.ID])
		options := effectiveTestOptionsForGroup(rule, *conn, state, config, groupID).options()
		started := time.Now()
		result := Result{ID: newResultID(), RuleID: rule.ID, ConnectionID: conn.ID, GroupID: groupID, Action: reason, Model: options.ModelID, StartedAt: started, CreatedAt: started}
		check := AccountTestResult{}
		checkErr := error(nil)
		switch {
		case rule.ManualPaused:
			target.Status = StatusManualPaused
			target.Message = "手动停止中，自动恢复已暂停"
		case !rule.Enabled && reason != "manual":
			target.Status = StatusUnknown
			target.Message = "监控规则已停用"
		case options.ModelID == "":
			target.Status = StatusUnsupported
			target.Message = "当前分组未配置检测模型"
		case balanceErr != nil:
			checkErr = fmt.Errorf("余额刷新失败")
		case convertedBalance(site) != nil && *convertedBalance(site) < rule.BalanceThreshold:
			target.Status = StatusBalancePaused
			target.Message = "余额低于阈值，停止调度"
			target.ConsecutiveFailures = 0
		default:
			if options.Mode == "image" || options.Mode == "video" {
				if _, _, err = media.Resolve(media.Options{Model: options.ModelID, Mode: options.Mode, Prompt: options.Prompt, Size: options.Size, Seconds: options.Seconds, Adapter: options.Adapter, PresetModel: options.PresetModel}); err != nil {
					target.Status = StatusConfigError
					target.Message = err.Error()
					target.ConsecutiveFailures = 0
					break
				}
				check, checkErr = s.mediaCheck(ctx, rule, *conn, *site, target, options)
				target.MediaTaskKey = rule.ID + "|" + target.Fingerprint
				if check.MediaTaskKey != "" {
					target.MediaTaskKey = check.MediaTaskKey
				}
			} else {
				cached, found := dedup[target.Fingerprint]
				if found {
					check, checkErr = cached.result, cached.err
				} else {
					check, checkErr = s.platform.ProbeUpstreamConnection(ctx, *site, *conn, options)
					dedup[target.Fingerprint] = struct {
						result AccountTestResult
						err    error
					}{result: check, err: checkErr}
				}
			}
			if check.Reused {
				// A repeated manual click within the check interval must not add
				// another failure or history point for the same terminal task.
				result.Status = target.Status
				result.Success = target.Status == StatusHealthy
				result.Message = target.Message
				result.LatencyMS = target.LatencyMS
			} else if check.Pending || check.Uncertain {
				target.Status = StatusGenerating
				target.Message = check.Message
				target.ConsecutiveFailures = 0
			} else if checkErr == nil && check.Success {
				target.Status = StatusHealthy
				target.Message = check.Message
				target.ConsecutiveFailures = 0
				latency := check.LatencyMS
				target.LatencyMS = &latency
			} else {
				target.Status = StatusFailed
				target.Message = check.Message
				target.ConsecutiveFailures++
				if checkErr != nil {
					target.Message = "上游检测失败：" + checkErr.Error()
				}
			}
		}
		if balanceErr != nil && !rule.ManualPaused && rule.Enabled {
			target.Status = StatusFailed
			target.Message = "余额刷新失败"
			target.ConsecutiveFailures++
		}
		now := time.Now()
		target.CheckedAt = &now
		target.NextCheckAt = &next
		if target.Status == StatusGenerating {
			poll := now.Add(10 * time.Second)
			target.NextCheckAt = &poll
			next = poll
		}
		result.Status = target.Status
		result.Success = target.Status == StatusHealthy
		result.Message = target.Message
		result.LatencyMS = target.LatencyMS
		result.TaskID = check.TaskID
		result.FinishedAt = now
		if err = targets.SaveTargetState(ctx, target); err != nil {
			return Result{}, err
		}
		if all[rule.ID] == nil {
			all[rule.ID] = map[string]TargetState{}
		}
		all[rule.ID][groupID] = target
		// Polling has its own durable task state. The timeline records actual
		// terminal checks rather than ten-second progress duplicates.
		if target.Status != StatusGenerating && !check.Reused {
			if err = s.store.AddResult(ctx, result); err != nil {
				return Result{}, err
			}
		}
		results = append(results, result)
	}
	if err = s.reconcileTargetDispatch(ctx, &rule, *conn, state, config, all); err != nil {
		return Result{}, err
	}
	if len(results) == 0 {
		return Result{}, nil
	}
	aggregate := results[0]
	for _, v := range results {
		if v.Success {
			aggregate = v
			break
		}
		if v.Status == StatusGenerating {
			aggregate = v
		}
	}
	rule.LastStatus = aggregate.Status
	rule.LastMessage = aggregate.Message
	rule.LastLatencyMS = aggregate.LatencyMS
	now := time.Now()
	rule.LastCheckedAt = &now
	rule.NextCheckAt = &next
	rule.UpdatedAt = now
	if rule.ManualPaused {
		rule.LastStatus = StatusManualPaused
	}
	if rule.LastAppliedSchedulable != nil && !*rule.LastAppliedSchedulable && rule.SchedulableManaged && aggregate.Status == StatusFailed {
		rule.LastStatus = StatusAutoPaused
	}
	if err = s.store.UpdateRule(ctx, rule); err != nil {
		return Result{}, err
	}
	aggregate.GroupResults = results
	return aggregate, nil
}

func (s *Service) reconcileTargetDispatch(ctx context.Context, rule *Rule, conn my_sites.RealConnection, state *my_sites.State, config TestModelConfig, all map[string]map[string]TargetState) error {
	if state == nil || state.Session.Platform != upstream.PlatformSub2API || conn.AdminAccountID == "" {
		return nil
	}
	connections, err := s.conns.ListRealConnections(ctx, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return err
	}
	rules, err := s.store.ListRulesForWorkspace(ctx, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return err
	}
	byConn := map[string]Rule{}
	for _, r := range rules {
		byConn[r.ConnectionID] = r
	}
	byConn[conn.ID] = *rule
	anyHealthy, allFailed, balanceBlocked, manualBlocked := false, true, false, false
	for _, c := range connections {
		if c.AdminAccountID != conn.AdminAccountID {
			continue
		}
		r, exists := byConn[c.ID]
		if !exists {
			allFailed = false
			continue
		}
		if r.ManualPaused || (!r.SchedulableManaged && r.AutoEnableBlocked) || r.SchedulableConflict {
			manualBlocked = true
		}
		ids := uniqueRuleIDs(c.OwnGroupIDs)
		if len(ids) == 0 {
			ids = []string{""}
		}
		for _, id := range ids {
			t := targetFor(r, c, id, state, config, all[r.ID])
			if t.Status == StatusHealthy {
				anyHealthy = true
			}
			if t.Status == StatusBalancePaused {
				balanceBlocked = true
			}
			if t.Status != StatusFailed || t.ConsecutiveFailures < r.FailureThreshold {
				allFailed = false
			}
		}
	}
	if manualBlocked {
		return nil
	}
	accounts := s.adminAccountsByID(state)
	account, exists := accounts[conn.AdminAccountID]
	if !exists || account.Schedulable == nil {
		return nil
	}
	if rule.SchedulableManaged && rule.LastAppliedSchedulable != nil && *rule.LastAppliedSchedulable != *account.Schedulable {
		rule.SchedulableConflict = true
		return nil
	}
	desired := *account.Schedulable
	if balanceBlocked || allFailed {
		desired = false
	} else if anyHealthy && ((rule.SchedulableManaged && rule.AutoEnableBlocked) || isMonitorDispatchPaused(rule.LastStatus)) {
		desired = true
	}
	if desired == *account.Schedulable {
		return nil
	}
	if err = s.platform.SetSub2APIAdminAccountSchedulable(state.Session, conn.AdminAccountID, desired); err != nil {
		return err
	}
	if !rule.SchedulableManaged {
		rule.OriginalSchedulable = cloneBool(account.Schedulable)
	}
	rule.SchedulableManaged = true
	rule.LastAppliedSchedulable = schedulablePtr(desired)
	rule.DesiredSchedulable = schedulablePtr(desired)
	rule.SchedulableConflict = false
	if desired {
		rule.AutoEnableBlocked = false
	}
	for _, c := range connections {
		if c.AdminAccountID != conn.AdminAccountID || c.ID == conn.ID {
			continue
		}
		other, ok := byConn[c.ID]
		if !ok {
			continue
		}
		other.SchedulableManaged = true
		other.LastAppliedSchedulable = schedulablePtr(desired)
		other.DesiredSchedulable = schedulablePtr(desired)
		if desired {
			other.AutoEnableBlocked = false
		}
		if err = s.store.UpdateRule(ctx, other); err != nil {
			return err
		}
	}
	s.invalidateAccountCache(rule.UserID, rule.AdminAccountID)
	return nil
}
