package channel_monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

type Store interface {
	EnsureSchema(ctx context.Context) error
	EnsureRulesForExistingConnections(ctx context.Context) error
	EnsureRuleForConnection(ctx context.Context, userID, adminAccountID string, conn my_sites.RealConnection) (Rule, error)
	ListRulesForWorkspace(ctx context.Context, userID, adminAccountID string) ([]Rule, error)
	GetRule(ctx context.Context, id string) (*Rule, error)
	UpdateRule(ctx context.Context, rule Rule) error
	AddResult(ctx context.Context, result Result) error
	ListRecentResults(ctx context.Context, ruleID string, limit int) ([]Result, error)
	ListDueRules(ctx context.Context, limit int) ([]Rule, error)
	GetRateRule(ctx context.Context, userID, adminAccountID string) (*RateRule, error)
	SaveRateRule(ctx context.Context, rule RateRule) error
	AddRateApplyResult(ctx context.Context, result RateApplyResult) error
	GetLastRateApplyResult(ctx context.Context, userID, adminAccountID string) (*RateApplyResult, error)
	GetTestModelConfig(ctx context.Context, userID, adminAccountID string) (*TestModelConfig, error)
	SaveTestModelConfig(ctx context.Context, config TestModelConfig) error
}

type ConnectionStore interface {
	ListRealConnections(ctx context.Context, userID string, adminAccountID string) ([]my_sites.RealConnection, error)
	GetRealConnection(ctx context.Context, id string, userID string, adminAccountID string) (*my_sites.RealConnection, error)
}

type StateStore interface {
	Get(ctx context.Context, userID string, adminAccountID string) (*my_sites.State, error)
}

type SessionProvider interface {
	RequireSession(ctx context.Context, userID string, adminAccountID string) (upstream.Session, error)
}

type UpstreamLookup interface {
	GetSite(ctx context.Context, siteID string) (*upstream.Site, error)
	RefreshSite(ctx context.Context, siteID string) (*upstream.Site, error)
}

type MonitorPlatform interface {
	TestSub2APIAdminAccount(session upstream.Session, accountID string, options AccountTestOptions) (AccountTestResult, error)
	SetSub2APIAdminAccountSchedulable(session upstream.Session, accountID string, schedulable bool) error
	ListSub2APIAdminAccounts(session upstream.Session) ([]AdminAccountStatus, error)
	UpdateSub2APIAdminAccountPriority(session upstream.Session, accountID string, priority int) error
}

type AdminAccountResolver interface {
	RequireCurrentID(ctx context.Context, userID string) (string, error)
}

type Service struct {
	store         Store
	conns         ConnectionStore
	states        StateStore
	sessions      SessionProvider
	upstreams     UpstreamLookup
	platform      MonitorPlatform
	accounts      AdminAccountResolver
	stopScheduler chan struct{}
}

func NewService(store Store, conns ConnectionStore, states StateStore, upstreams UpstreamLookup, platform MonitorPlatform, accounts AdminAccountResolver) *Service {
	return &Service{
		store:     store,
		conns:     conns,
		states:    states,
		upstreams: upstreams,
		platform:  platform,
		accounts:  accounts,
	}
}

func (s *Service) SetSessionProvider(provider SessionProvider) {
	s.sessions = provider
}

func (s *Service) EnsureSchema(ctx context.Context) error {
	if err := s.store.EnsureSchema(ctx); err != nil {
		return err
	}
	return s.store.EnsureRulesForExistingConnections(ctx)
}

func (s *Service) Summary(ctx context.Context, userID string) (SummaryResponse, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return SummaryResponse{}, err
	}
	connections, err := s.conns.ListRealConnections(ctx, userID, adminAccountID)
	if err != nil {
		return SummaryResponse{}, err
	}
	rulesByConnection := make(map[string]Rule, len(connections))
	for _, conn := range connections {
		rule, err := s.store.EnsureRuleForConnection(ctx, userID, adminAccountID, conn)
		if err != nil {
			return SummaryResponse{}, err
		}
		rulesByConnection[conn.ID] = rule
	}
	state := s.summaryState(ctx, userID, adminAccountID)
	accountsByID := s.adminAccountsByID(state)
	rateRule, err := s.ensureRateRule(ctx, userID, adminAccountID)
	if err != nil {
		return SummaryResponse{}, err
	}
	rateRows, rateSummary := s.buildRatePlan(ctx, connections, rulesByConnection, state, accountsByID, rateRule)
	rateRowsByConnection := make(map[string]RatePlanRow, len(rateRows))
	for _, row := range rateRows {
		rateRowsByConnection[row.ConnectionID] = row
	}
	lastRateResult, _ := s.store.GetLastRateApplyResult(ctx, userID, adminAccountID)
	testModelConfig, err := s.ensureTestModelConfig(ctx, userID, adminAccountID)
	if err != nil {
		return SummaryResponse{}, err
	}

	response := SummaryResponse{
		Channels:        []ChannelStatus{},
		Groups:          []GroupSummary{},
		RateRule:        RateRuleView{Rule: rateRule, Summary: rateSummary, Rows: rateRows, LastResult: lastRateResult},
		TestModelConfig: testModelConfig,
	}
	groupMap := map[string]*GroupSummary{}
	for _, conn := range connections {
		rule := rulesByConnection[conn.ID]
		row := s.channelStatus(ctx, conn, rule, state, accountsByID)
		if rateRow, ok := rateRowsByConnection[conn.ID]; ok {
			applyRatePlanToChannel(&row, rateRow)
		}
		response.Channels = append(response.Channels, row)
		applyStats(&response.Stats, row)
		ownGroups := row.OwnGroups
		if len(ownGroups) == 0 {
			ownGroups = []string{"未分组"}
		}
		for _, groupName := range ownGroups {
			key := groupName + "|" + row.GroupType
			group := groupMap[key]
			if group == nil {
				group = &GroupSummary{GroupName: groupName, Platform: row.GroupType}
				groupMap[key] = group
			}
			group.Total++
			if isAvailable(row) {
				group.Available++
			}
			switch row.Status {
			case StatusFailed, StatusAutoPaused:
				group.Failed++
			case StatusBalancePaused:
				group.BalancePaused++
			case StatusManualPaused:
				group.ManualPaused++
			}
			if !row.Enabled {
				group.MonitorPaused++
			}
			if row.Schedulable != nil && !*row.Schedulable {
				group.DispatchPaused++
			}
			if row.LastCheckedAt != nil && (group.LastCheckedAt == nil || row.LastCheckedAt.After(*group.LastCheckedAt)) {
				group.LastCheckedAt = row.LastCheckedAt
			}
		}
	}
	for _, group := range groupMap {
		response.Groups = append(response.Groups, *group)
	}
	sort.Slice(response.Groups, func(i, j int) bool {
		if response.Groups[i].GroupName == response.Groups[j].GroupName {
			return response.Groups[i].Platform < response.Groups[j].Platform
		}
		return response.Groups[i].GroupName < response.Groups[j].GroupName
	})
	sort.Slice(response.Channels, func(i, j int) bool {
		if response.Channels[i].Status == response.Channels[j].Status {
			return response.Channels[i].SiteName < response.Channels[j].SiteName
		}
		return statusRank(response.Channels[i].Status) < statusRank(response.Channels[j].Status)
	})
	return response, nil
}

func (s *Service) RunRule(ctx context.Context, ruleID string, reason string) (Result, error) {
	rule, err := s.store.GetRule(ctx, strings.TrimSpace(ruleID))
	if err != nil {
		return Result{}, err
	}
	if rule == nil {
		return Result{}, requestError("admin.channelMonitor.errors.notFound")
	}
	return s.runRule(ctx, *rule, reason)
}

func (s *Service) RunRuleForUser(ctx context.Context, userID, ruleID string, reason string) (Result, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	rule, err := s.requireRule(ctx, ruleID, userID, adminAccountID)
	if err != nil {
		return Result{}, err
	}
	return s.runRule(ctx, rule, reason)
}

func (s *Service) PauseRule(ctx context.Context, userID, ruleID string) (Rule, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return Rule{}, err
	}
	rule, err := s.requireRule(ctx, ruleID, userID, adminAccountID)
	if err != nil {
		return Rule{}, err
	}
	state, err := s.workspaceState(ctx, userID, adminAccountID)
	if err != nil {
		return Rule{}, err
	}
	conn, err := s.conns.GetRealConnection(ctx, rule.ConnectionID, userID, adminAccountID)
	if err != nil {
		return Rule{}, err
	}
	if state != nil && conn != nil && state.Session.Platform == upstream.PlatformSub2API && strings.TrimSpace(conn.AdminAccountID) != "" {
		if err := s.platform.SetSub2APIAdminAccountSchedulable(state.Session, conn.AdminAccountID, false); err != nil {
			return Rule{}, err
		}
		rule.DesiredSchedulable = schedulablePtr(false)
	}
	now := time.Now()
	next := now.Add(time.Duration(rule.CheckIntervalMinutes) * time.Minute)
	rule.ManualPaused = true
	rule.LastStatus = StatusManualPaused
	rule.LastMessage = "手动停止"
	rule.LastCheckedAt = &now
	rule.NextCheckAt = &next
	rule.UpdatedAt = now
	if err := s.store.UpdateRule(ctx, rule); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func (s *Service) ResumeRule(ctx context.Context, userID, ruleID string) (Result, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	rule, err := s.requireRule(ctx, ruleID, userID, adminAccountID)
	if err != nil {
		return Result{}, err
	}
	state, err := s.workspaceState(ctx, userID, adminAccountID)
	if err != nil {
		return Result{}, err
	}
	conn, err := s.conns.GetRealConnection(ctx, rule.ConnectionID, userID, adminAccountID)
	if err != nil {
		return Result{}, err
	}
	if state != nil && conn != nil && state.Session.Platform == upstream.PlatformSub2API && strings.TrimSpace(conn.AdminAccountID) != "" {
		if err := s.platform.SetSub2APIAdminAccountSchedulable(state.Session, conn.AdminAccountID, true); err != nil {
			return Result{}, err
		}
		rule.DesiredSchedulable = schedulablePtr(true)
	}
	rule.ManualPaused = false
	rule.LastStatus = StatusManualPaused
	rule.LastMessage = "手动启动检测"
	if err := s.store.UpdateRule(ctx, rule); err != nil {
		return Result{}, err
	}
	return s.runRule(ctx, rule, "manual")
}

func (s *Service) UpdateRule(ctx context.Context, userID, ruleID string, req UpdateRuleRequest) (Rule, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return Rule{}, err
	}
	rule, err := s.requireRule(ctx, ruleID, userID, adminAccountID)
	if err != nil {
		return Rule{}, err
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.CheckIntervalMinutes != nil {
		rule.CheckIntervalMinutes = clampInt(*req.CheckIntervalMinutes, 1, 24*60)
	}
	if req.FailureThreshold != nil {
		rule.FailureThreshold = clampInt(*req.FailureThreshold, 1, 100)
	}
	if req.BalanceThreshold != nil {
		if *req.BalanceThreshold < 0 {
			rule.BalanceThreshold = 0
		} else {
			rule.BalanceThreshold = *req.BalanceThreshold
		}
	}
	now := time.Now()
	next := now
	rule.NextCheckAt = &next
	rule.UpdatedAt = now
	return rule, s.store.UpdateRule(ctx, rule)
}

func (s *Service) BulkUpdateRules(ctx context.Context, userID string, req BulkUpdateRuleRequest) ([]Rule, error) {
	ruleIDs := uniqueRuleIDs(req.RuleIDs)
	if len(ruleIDs) == 0 {
		return nil, requestError("admin.channelMonitor.errors.request")
	}
	updated := make([]Rule, 0, len(ruleIDs))
	for _, ruleID := range ruleIDs {
		rule, err := s.UpdateRule(ctx, userID, ruleID, UpdateRuleRequest{
			Enabled:              req.Enabled,
			CheckIntervalMinutes: req.CheckIntervalMinutes,
			FailureThreshold:     req.FailureThreshold,
			BalanceThreshold:     req.BalanceThreshold,
		})
		if err != nil {
			return nil, err
		}
		updated = append(updated, rule)
	}
	return updated, nil
}

func (s *Service) SetRuleSchedulable(ctx context.Context, userID, ruleID string, schedulable bool) error {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return err
	}
	rule, err := s.requireRule(ctx, ruleID, userID, adminAccountID)
	if err != nil {
		return err
	}
	state, err := s.workspaceState(ctx, userID, adminAccountID)
	if err != nil {
		return err
	}
	conn, err := s.conns.GetRealConnection(ctx, rule.ConnectionID, userID, adminAccountID)
	if err != nil {
		return err
	}
	if state == nil || state.Session.Platform != upstream.PlatformSub2API || conn == nil || strings.TrimSpace(conn.AdminAccountID) == "" {
		return requestError("admin.channelMonitor.errors.unsupported")
	}
	if err := s.platform.SetSub2APIAdminAccountSchedulable(state.Session, conn.AdminAccountID, schedulable); err != nil {
		return err
	}
	now := time.Now()
	rule.DesiredSchedulable = schedulablePtr(schedulable)
	rule.LastMessage = "手动停用分组调度"
	if schedulable {
		rule.LastMessage = "手动开启分组调度"
	}
	rule.UpdatedAt = now
	return s.store.UpdateRule(ctx, rule)
}

func (s *Service) SetRulePriority(ctx context.Context, userID, ruleID string, priority int) error {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return err
	}
	rule, err := s.requireRule(ctx, ruleID, userID, adminAccountID)
	if err != nil {
		return err
	}
	state, err := s.workspaceState(ctx, userID, adminAccountID)
	if err != nil {
		return err
	}
	conn, err := s.conns.GetRealConnection(ctx, rule.ConnectionID, userID, adminAccountID)
	if err != nil {
		return err
	}
	if state == nil || state.Session.Platform != upstream.PlatformSub2API || conn == nil || strings.TrimSpace(conn.AdminAccountID) == "" {
		return requestError("admin.channelMonitor.errors.unsupported")
	}
	priority = clampInt(priority, 0, 999)
	if err := s.platform.UpdateSub2APIAdminAccountPriority(state.Session, conn.AdminAccountID, priority); err != nil {
		return err
	}
	now := time.Now()
	rule.LastMessage = fmt.Sprintf("手动设置优先级为 %d", priority)
	rule.UpdatedAt = now
	return s.store.UpdateRule(ctx, rule)
}

func (s *Service) BulkSetSchedulable(ctx context.Context, userID string, req BulkSchedulableRequest) error {
	ruleIDs := uniqueRuleIDs(req.RuleIDs)
	if len(ruleIDs) == 0 {
		return requestError("admin.channelMonitor.errors.request")
	}
	for _, ruleID := range ruleIDs {
		if err := s.SetRuleSchedulable(ctx, userID, ruleID, req.Schedulable); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) BulkRunRules(ctx context.Context, userID string, req BulkRunRequest) ([]Result, error) {
	ruleIDs := uniqueRuleIDs(req.RuleIDs)
	if len(ruleIDs) == 0 {
		return nil, requestError("admin.channelMonitor.errors.request")
	}
	results := make([]Result, 0, len(ruleIDs))
	for _, ruleID := range ruleIDs {
		result, err := s.RunRuleForUser(ctx, userID, ruleID, "manual")
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Service) RateRuleView(ctx context.Context, userID string) (RateRuleView, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return RateRuleView{}, err
	}
	rule, err := s.ensureRateRule(ctx, userID, adminAccountID)
	if err != nil {
		return RateRuleView{}, err
	}
	connections, rulesByConnection, state, accountsByID, err := s.rateRuleContext(ctx, userID, adminAccountID, false)
	if err != nil {
		return RateRuleView{}, err
	}
	rows, summary := s.buildRatePlan(ctx, connections, rulesByConnection, state, accountsByID, rule)
	last, _ := s.store.GetLastRateApplyResult(ctx, userID, adminAccountID)
	return RateRuleView{Rule: rule, Summary: summary, Rows: rows, LastResult: last}, nil
}

func (s *Service) UpdateRateRule(ctx context.Context, userID string, req UpdateRateRuleRequest) (RateRule, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return RateRule{}, err
	}
	rule, err := s.ensureRateRule(ctx, userID, adminAccountID)
	if err != nil {
		return RateRule{}, err
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.AutoApplyOnCheck != nil {
		rule.AutoApplyOnCheck = *req.AutoApplyOnCheck
	}
	if req.UpdatePriority != nil {
		rule.UpdatePriority = *req.UpdatePriority
	}
	if req.StopWhenMissingRate != nil {
		rule.StopWhenMissingRate = *req.StopWhenMissingRate
	}
	rule.UpdatedAt = time.Now()
	if err := s.store.SaveRateRule(ctx, rule); err != nil {
		return RateRule{}, err
	}
	return rule, nil
}

func (s *Service) UpdateTestModelConfig(ctx context.Context, userID string, req UpdateTestModelConfigRequest) (TestModelConfig, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return TestModelConfig{}, err
	}
	config, err := s.ensureTestModelConfig(ctx, userID, adminAccountID)
	if err != nil {
		return TestModelConfig{}, err
	}
	if req.OpenAIModelID != nil {
		config.OpenAIModelID = defaultIfBlank(*req.OpenAIModelID, DefaultOpenAITestModel)
	}
	if req.AnthropicModelID != nil {
		config.AnthropicModelID = defaultIfBlank(*req.AnthropicModelID, DefaultAnthropicTestModel)
	}
	if req.BalanceRefreshIntervalMinutes != nil {
		config.BalanceRefreshIntervalMinutes = clampInt(*req.BalanceRefreshIntervalMinutes, 1, 24*60)
	}
	config.UpdatedAt = time.Now()
	if err := s.store.SaveTestModelConfig(ctx, config); err != nil {
		return TestModelConfig{}, err
	}
	return config, nil
}

func (s *Service) PreviewRateRule(ctx context.Context, userID string) (RateRuleView, error) {
	return s.RateRuleView(ctx, userID)
}

func (s *Service) ApplyRateRule(ctx context.Context, userID string, action string) (RateApplyResult, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return RateApplyResult{}, err
	}
	return s.applyRateRuleForWorkspace(ctx, userID, adminAccountID, action)
}

func (s *Service) applyRateRuleForWorkspace(ctx context.Context, userID, adminAccountID, action string) (RateApplyResult, error) {
	rule, err := s.ensureRateRule(ctx, userID, adminAccountID)
	if err != nil {
		return RateApplyResult{}, err
	}
	connections, rulesByConnection, state, accountsByID, err := s.rateRuleContext(ctx, userID, adminAccountID, true)
	if err != nil {
		return RateApplyResult{}, err
	}
	rows, _ := s.buildRatePlan(ctx, connections, rulesByConnection, state, accountsByID, rule)
	now := time.Now()
	result := RateApplyResult{
		ID:             newResultID(),
		UserID:         userID,
		AdminAccountID: adminAccountID,
		Action:         strings.TrimSpace(action),
		Success:        true,
		Message:        "倍率规则已应用",
		Total:          len(rows),
		Rows:           rows,
		CreatedAt:      now,
	}
	if result.Action == "" {
		result.Action = "manual"
	}
	if !rule.Enabled {
		result.Message = "倍率规则未启用"
		for _, row := range rows {
			if row.RateGateStatus == RateGateSkipped {
				result.SkippedCount++
			}
		}
		_ = s.store.AddRateApplyResult(ctx, result)
		return result, nil
	}
	if state == nil || state.Session.Platform != upstream.PlatformSub2API {
		return RateApplyResult{}, requestError("admin.channelMonitor.errors.unsupported")
	}

	for _, row := range rows {
		if !row.Supported || row.RateGateStatus == RateGateSkipped {
			result.SkippedCount++
			continue
		}
		target := row.SuggestedSchedulable
		current := row.CurrentSchedulable
		if current == nil || *current != target {
			if err := s.platform.SetSub2APIAdminAccountSchedulable(state.Session, row.AdminAccountID, target); err != nil {
				result.Success = false
				result.Message = err.Error()
				continue
			}
			if target {
				result.EnabledCount++
			} else {
				result.DisabledCount++
			}
			if monitorRule, ok := rulesByConnection[row.ConnectionID]; ok {
				monitorRule.DesiredSchedulable = schedulablePtr(target)
				monitorRule.LastMessage = row.RateGateMessage
				monitorRule.UpdatedAt = now
				_ = s.store.UpdateRule(ctx, monitorRule)
			}
		}
		if rule.UpdatePriority && target && row.SuggestedPriority != nil {
			if row.CurrentPriority == nil || *row.CurrentPriority != *row.SuggestedPriority {
				if err := s.platform.UpdateSub2APIAdminAccountPriority(state.Session, row.AdminAccountID, *row.SuggestedPriority); err != nil {
					result.Success = false
					result.Message = err.Error()
					continue
				}
				result.PriorityUpdated++
			}
		}
	}
	rule.LastAppliedAt = &now
	rule.UpdatedAt = now
	_ = s.store.SaveRateRule(ctx, rule)
	if err := s.store.AddRateApplyResult(ctx, result); err != nil {
		return RateApplyResult{}, err
	}
	return result, nil
}

func (s *Service) RunDue(ctx context.Context, limit int) int {
	if limit <= 0 {
		limit = 20
	}
	if err := s.store.EnsureRulesForExistingConnections(ctx); err != nil {
		log.Printf("[channel-monitor] ensure rules failed: %v", err)
	}
	rules, err := s.store.ListDueRules(ctx, limit)
	if err != nil {
		log.Printf("[channel-monitor] list due rules failed: %v", err)
		return 0
	}
	checked := 0
	for _, rule := range rules {
		if _, err := s.runRule(ctx, rule, "scheduled"); err != nil {
			log.Printf("[channel-monitor] run rule failed rule_id=%s err=%v", rule.ID, err)
			continue
		}
		checked++
	}
	return checked
}

func (s *Service) StartScheduler(ctx context.Context) {
	if s.stopScheduler != nil {
		return
	}
	s.stopScheduler = make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopScheduler:
				return
			case <-ticker.C:
				s.RunDue(context.Background(), 20)
			}
		}
	}()
}

func (s *Service) StopScheduler() {
	if s.stopScheduler != nil {
		close(s.stopScheduler)
		s.stopScheduler = nil
	}
}

func (s *Service) runRule(ctx context.Context, rule Rule, reason string) (Result, error) {
	started := time.Now()
	result := Result{
		ID:           newResultID(),
		RuleID:       rule.ID,
		ConnectionID: rule.ConnectionID,
		StartedAt:    started,
		CreatedAt:    started,
		Action:       reason,
	}
	finish := func(status string, success bool, message string, latency *int, model string) (Result, error) {
		now := time.Now()
		next := now.Add(time.Duration(rule.CheckIntervalMinutes) * time.Minute)
		result.Status = status
		result.Success = success
		result.Message = message
		result.LatencyMS = latency
		result.Model = model
		result.FinishedAt = now
		rule.LastStatus = status
		rule.LastMessage = message
		rule.LastLatencyMS = latency
		rule.LastCheckedAt = &now
		rule.NextCheckAt = &next
		rule.UpdatedAt = now
		if err := s.store.UpdateRule(ctx, rule); err != nil {
			return Result{}, err
		}
		if err := s.store.AddResult(ctx, result); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	if !rule.Enabled && reason != "manual" {
		return finish(StatusUnknown, true, "监控规则已停用", nil, "")
	}
	if rule.ManualPaused {
		return finish(StatusManualPaused, true, "手动停止中，自动恢复已暂停", nil, "")
	}
	conn, err := s.conns.GetRealConnection(ctx, rule.ConnectionID, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return Result{}, err
	}
	if conn == nil {
		return finish(StatusFailed, false, "真实对接记录不存在", nil, "")
	}
	state, err := s.workspaceState(ctx, rule.UserID, rule.AdminAccountID)
	if err != nil {
		rule.ConsecutiveFailures = 0
		return finish(StatusUnknown, true, "admin 登录会话失效，已跳过本次检测："+err.Error(), nil, "")
	}
	if state == nil || state.Session.Platform != upstream.PlatformSub2API {
		rule.ConsecutiveFailures = 0
		return finish(StatusUnsupported, false, "当前 admin 平台暂不支持主动监控", nil, "")
	}
	testModelConfig, err := s.ensureTestModelConfig(ctx, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return Result{}, err
	}
	site, err := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
	if err != nil {
		return Result{}, err
	}
	if site == nil {
		return finish(StatusFailed, false, "上游站点不存在", nil, "")
	}
	if refreshedSite, refreshErr := s.refreshSiteBalanceIfStale(ctx, site, testModelConfig.BalanceRefreshIntervalMinutes); refreshErr != nil {
		message := "余额刷新失败：" + refreshErr.Error()
		rule.ConsecutiveFailures++
		if rule.ConsecutiveFailures >= rule.FailureThreshold {
			if strings.TrimSpace(conn.AdminAccountID) != "" {
				if err := s.platform.SetSub2APIAdminAccountSchedulable(state.Session, conn.AdminAccountID, false); err != nil {
					return finish(StatusFailed, false, message+"；自动停止失败："+err.Error(), nil, "")
				}
				rule.DesiredSchedulable = schedulablePtr(false)
			}
			rule.ConsecutiveFailures = 0
			return finish(StatusAutoPaused, false, fmt.Sprintf("%s；连续失败达到 %d 次，已自动停止", message, rule.FailureThreshold), nil, "")
		}
		return finish(StatusFailed, false, message, nil, "")
	} else if refreshedSite != nil {
		site = refreshedSite
	}
	if balance := convertedBalance(site); balance != nil && *balance < rule.BalanceThreshold {
		rule.ConsecutiveFailures = 0
		if strings.TrimSpace(conn.AdminAccountID) != "" {
			if err := s.platform.SetSub2APIAdminAccountSchedulable(state.Session, conn.AdminAccountID, false); err != nil {
				return finish(StatusBalancePaused, false, fmt.Sprintf("余额 %.2f 低于阈值 %.2f，停用失败：%v", *balance, rule.BalanceThreshold, err), nil, "")
			}
			rule.DesiredSchedulable = schedulablePtr(false)
		}
		return finish(StatusBalancePaused, false, fmt.Sprintf("余额 %.2f 低于阈值 %.2f，已自动停止", *balance, rule.BalanceThreshold), nil, "")
	}

	testModel := testModelForGroupType(conn.GroupType, testModelConfig)
	testResult, testErr := s.platform.TestSub2APIAdminAccount(state.Session, conn.AdminAccountID, AccountTestOptions{ModelID: testModel})
	if strings.TrimSpace(testResult.Model) == "" {
		testResult.Model = testModel
	}
	if testErr != nil || !testResult.Success {
		message := "账号测试失败"
		if testErr != nil {
			message = testErr.Error()
		} else if strings.TrimSpace(testResult.Message) != "" {
			message = testResult.Message
		}
		rule.ConsecutiveFailures++
		if rule.ConsecutiveFailures >= rule.FailureThreshold {
			if strings.TrimSpace(conn.AdminAccountID) != "" {
				if err := s.platform.SetSub2APIAdminAccountSchedulable(state.Session, conn.AdminAccountID, false); err != nil {
					return finish(StatusFailed, false, message+"；自动停止失败："+err.Error(), nil, testResult.Model)
				}
				rule.DesiredSchedulable = schedulablePtr(false)
			}
			rule.ConsecutiveFailures = 0
			return finish(StatusAutoPaused, false, fmt.Sprintf("%s；连续失败达到 %d 次，已自动停止", message, rule.FailureThreshold), nil, testResult.Model)
		}
		return finish(StatusFailed, false, message, nil, testResult.Model)
	}

	latency := testResult.LatencyMS
	if rule.LastStatus == StatusAutoPaused || rule.LastStatus == StatusBalancePaused {
		if strings.TrimSpace(conn.AdminAccountID) != "" {
			if err := s.platform.SetSub2APIAdminAccountSchedulable(state.Session, conn.AdminAccountID, true); err != nil {
				return finish(StatusFailed, false, "检测已恢复，但自动启用失败："+err.Error(), &latency, testResult.Model)
			}
			rule.DesiredSchedulable = schedulablePtr(true)
		}
	}
	rule.ConsecutiveFailures = 0
	message := strings.TrimSpace(testResult.Message)
	if message == "" {
		message = "账号测试通过"
	}
	result, err = finish(StatusHealthy, true, message, &latency, testResult.Model)
	if err == nil {
		s.applyRateRuleAfterCheck(ctx, rule.UserID, rule.AdminAccountID)
	}
	return result, err
}

func (s *Service) applyRateRuleAfterCheck(ctx context.Context, userID, adminAccountID string) {
	rule, err := s.ensureRateRule(ctx, userID, adminAccountID)
	if err != nil || !rule.Enabled || !rule.AutoApplyOnCheck {
		return
	}
	if _, err := s.applyRateRuleForWorkspace(ctx, userID, adminAccountID, "check"); err != nil {
		log.Printf("[channel-monitor] apply rate rule after check failed user_id=%s admin_account_id=%s err=%v", userID, adminAccountID, err)
	}
}

func (s *Service) requireRule(ctx context.Context, id, userID, adminAccountID string) (Rule, error) {
	rule, err := s.store.GetRule(ctx, strings.TrimSpace(id))
	if err != nil {
		return Rule{}, err
	}
	if rule == nil || rule.UserID != userID || rule.AdminAccountID != adminAccountID {
		return Rule{}, requestError("admin.channelMonitor.errors.notFound")
	}
	return *rule, nil
}

func (s *Service) currentAdminAccountID(ctx context.Context, userID string) (string, error) {
	if s.accounts == nil {
		return "", requestError("admin.adminAccounts.errors.noCurrentAccount")
	}
	return s.accounts.RequireCurrentID(ctx, userID)
}

func (s *Service) summaryState(ctx context.Context, userID, adminAccountID string) *my_sites.State {
	state, err := s.workspaceState(ctx, userID, adminAccountID)
	if err == nil {
		return state
	}
	log.Printf("[channel-monitor] admin session unavailable user_id=%s admin_account_id=%s err=%v", userID, adminAccountID, err)
	state, _ = s.states.Get(ctx, userID, adminAccountID)
	return state
}

func (s *Service) workspaceState(ctx context.Context, userID, adminAccountID string) (*my_sites.State, error) {
	state, err := s.states.Get(ctx, userID, adminAccountID)
	if err != nil {
		return nil, err
	}
	if s.sessions == nil {
		return state, nil
	}
	session, err := s.sessions.RequireSession(ctx, userID, adminAccountID)
	if err != nil {
		return state, err
	}
	if state == nil {
		state = &my_sites.State{
			UserID:         userID,
			AdminAccountID: adminAccountID,
			Mappings:       []my_sites.GroupMapping{},
		}
	}
	state.UserID = userID
	state.AdminAccountID = adminAccountID
	state.Session = session
	return state, nil
}

func (s *Service) channelStatus(ctx context.Context, conn my_sites.RealConnection, rule Rule, state *my_sites.State, accountsByID map[string]AdminAccountStatus) ChannelStatus {
	row := ChannelStatus{
		RuleID:               rule.ID,
		ConnectionID:         conn.ID,
		Enabled:              rule.Enabled,
		ManualPaused:         rule.ManualPaused,
		Status:               normalizedStatus(rule.LastStatus),
		UpstreamGroupID:      conn.UpstreamGroupID,
		UpstreamGroupName:    conn.UpstreamGroupName,
		GroupType:            conn.GroupType,
		AdminAccountID:       conn.AdminAccountID,
		AdminAccountName:     conn.AdminAccountName,
		OwnGroups:            ownGroupsForConnection(state, conn),
		CheckIntervalMinutes: rule.CheckIntervalMinutes,
		FailureThreshold:     rule.FailureThreshold,
		BalanceThreshold:     rule.BalanceThreshold,
		ConsecutiveFailures:  rule.ConsecutiveFailures,
		LastMessage:          rule.LastMessage,
		LastLatencyMS:        rule.LastLatencyMS,
		LastCheckedAt:        rule.LastCheckedAt,
		NextCheckAt:          rule.NextCheckAt,
		Supported:            state != nil && state.Session.Platform == upstream.PlatformSub2API,
		RecentResults:        []Result{},
	}
	if account, ok := accountsByID[strings.TrimSpace(conn.AdminAccountID)]; ok {
		row.Schedulable = account.Schedulable
		if strings.TrimSpace(row.AdminAccountName) == "" {
			row.AdminAccountName = account.Name
		}
	}
	if rule.DesiredSchedulable != nil {
		row.Schedulable = rule.DesiredSchedulable
	}
	if !row.Supported {
		row.Status = StatusUnsupported
	}
	site, err := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
	if err == nil && site != nil {
		row.SiteID = site.ID
		row.SiteName = site.Name
		row.SitePlatform = string(site.Platform)
		row.Balance = convertedBalance(site)
	}
	results, err := s.store.ListRecentResults(ctx, rule.ID, 60)
	if err == nil {
		if results != nil {
			row.RecentResults = results
		}
		row.RecentTotal = len(results)
		for _, result := range results {
			if result.Success {
				row.RecentSuccess++
			}
		}
		if row.RecentTotal > 0 {
			row.UptimePercent = float64(row.RecentSuccess) / float64(row.RecentTotal) * 100
		}
	}
	return row
}

func (s *Service) adminAccountsByID(state *my_sites.State) map[string]AdminAccountStatus {
	accountsByID := map[string]AdminAccountStatus{}
	if state == nil || state.Session.Platform != upstream.PlatformSub2API {
		return accountsByID
	}
	accounts, err := s.platform.ListSub2APIAdminAccounts(state.Session)
	if err != nil {
		log.Printf("[channel-monitor] list admin accounts failed: %v", err)
		return accountsByID
	}
	for _, account := range accounts {
		id := strings.TrimSpace(account.ID)
		if id == "" {
			continue
		}
		accountsByID[id] = account
	}
	return accountsByID
}

func (s *Service) ensureRateRule(ctx context.Context, userID, adminAccountID string) (RateRule, error) {
	rule, err := s.store.GetRateRule(ctx, userID, adminAccountID)
	if err != nil {
		return RateRule{}, err
	}
	if rule != nil {
		return *rule, nil
	}
	defaultRule := DefaultRateRule(userID, adminAccountID)
	if err := s.store.SaveRateRule(ctx, defaultRule); err != nil {
		return RateRule{}, err
	}
	return defaultRule, nil
}

func (s *Service) ensureTestModelConfig(ctx context.Context, userID, adminAccountID string) (TestModelConfig, error) {
	config, err := s.store.GetTestModelConfig(ctx, userID, adminAccountID)
	if err != nil {
		return TestModelConfig{}, err
	}
	if config != nil {
		config.OpenAIModelID = defaultIfBlank(config.OpenAIModelID, DefaultOpenAITestModel)
		config.AnthropicModelID = defaultIfBlank(config.AnthropicModelID, DefaultAnthropicTestModel)
		config.BalanceRefreshIntervalMinutes = clampInt(config.BalanceRefreshIntervalMinutes, 1, 24*60)
		return *config, nil
	}
	defaultConfig := DefaultTestModelConfig(userID, adminAccountID)
	if err := s.store.SaveTestModelConfig(ctx, defaultConfig); err != nil {
		return TestModelConfig{}, err
	}
	return defaultConfig, nil
}

func (s *Service) rateRuleContext(ctx context.Context, userID, adminAccountID string, requireSession bool) ([]my_sites.RealConnection, map[string]Rule, *my_sites.State, map[string]AdminAccountStatus, error) {
	connections, err := s.conns.ListRealConnections(ctx, userID, adminAccountID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	rulesByConnection := make(map[string]Rule, len(connections))
	for _, conn := range connections {
		rule, err := s.store.EnsureRuleForConnection(ctx, userID, adminAccountID, conn)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		rulesByConnection[conn.ID] = rule
	}
	state, err := s.workspaceState(ctx, userID, adminAccountID)
	if err != nil {
		if requireSession {
			return nil, nil, nil, nil, err
		}
		log.Printf("[channel-monitor] rate rule context using stale state user_id=%s admin_account_id=%s err=%v", userID, adminAccountID, err)
		state, _ = s.states.Get(ctx, userID, adminAccountID)
	}
	return connections, rulesByConnection, state, s.adminAccountsByID(state), nil
}

func (s *Service) buildRatePlan(ctx context.Context, connections []my_sites.RealConnection, rulesByConnection map[string]Rule, state *my_sites.State, accountsByID map[string]AdminAccountStatus, rateRule RateRule) ([]RatePlanRow, RateApplySummary) {
	rows := make([]RatePlanRow, 0, len(connections))
	for _, conn := range connections {
		monitorRule := rulesByConnection[conn.ID]
		account := accountsByID[strings.TrimSpace(conn.AdminAccountID)]
		row := s.buildRatePlanRow(ctx, conn, monitorRule, state, account, rateRule)
		rows = append(rows, row)
	}
	assignRecommendedPriorities(rows)
	summary := summarizeRatePlan(rows)
	return rows, summary
}

func (s *Service) buildRatePlanRow(ctx context.Context, conn my_sites.RealConnection, rule Rule, state *my_sites.State, account AdminAccountStatus, rateRule RateRule) RatePlanRow {
	ownGroups := ownGroupsForConnection(state, conn)
	row := RatePlanRow{
		RuleID:                rule.ID,
		ConnectionID:          conn.ID,
		AdminAccountID:        conn.AdminAccountID,
		AdminAccountName:      firstNonBlank(conn.AdminAccountName, account.Name),
		UpstreamGroupName:     conn.UpstreamGroupName,
		OwnGroups:             ownGroups,
		AccountRateMultiplier: account.RateMultiplier,
		AccountPriority:       account.Priority,
		CurrentPriority:       account.Priority,
		CurrentSchedulable:    account.Schedulable,
		Supported:             state != nil && state.Session.Platform == upstream.PlatformSub2API && strings.TrimSpace(conn.AdminAccountID) != "",
		SuggestedSchedulable:  true,
	}
	if rule.DesiredSchedulable != nil {
		row.CurrentSchedulable = rule.DesiredSchedulable
	}
	site, err := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
	if err == nil && site != nil {
		row.SiteName = site.Name
		row.UpstreamMultiplier = upstreamGroupMultiplier(site, conn)
		row.UpstreamEffectiveMultiplier = effectiveMultiplier(row.UpstreamMultiplier, site.RechargeRate)
	}
	if row.UpstreamEffectiveMultiplier == nil && account.RateMultiplier != nil {
		row.UpstreamEffectiveMultiplier = account.RateMultiplier
	}
	if row.UpstreamMultiplier == nil && account.RateMultiplier != nil {
		row.UpstreamMultiplier = account.RateMultiplier
	}
	if !row.Supported {
		row.RateGateStatus = RateGateSkipped
		row.SuggestedSchedulable = false
		row.RateGateMessage = "当前平台暂不支持倍率规则"
		return row
	}
	if shouldProtectPausedRule(rule) {
		row.RateGateStatus = RateGateSkipped
		row.SuggestedSchedulable = false
		row.RateGateMessage = "当前渠道处于故障、余额或手动停用状态，倍率规则不自动开启"
		return row
	}
	if len(ownGroups) == 0 {
		row.RateGateStatus = RateGateMissing
		row.SuggestedSchedulable = !rateRule.StopWhenMissingRate
		row.RateGateMessage = "缺少自有分组"
		return row
	}
	if row.UpstreamEffectiveMultiplier == nil {
		row.RateGateStatus = RateGateMissing
		row.SuggestedSchedulable = !rateRule.StopWhenMissingRate
		row.RateGateMessage = "缺少上游实际倍率"
		return row
	}

	ownRateByName := ownGroupRateByName(state)
	blocked := false
	missingOwnRate := false
	var minOwn *float64
	for _, groupName := range ownGroups {
		ownRate := ownRateByName[groupName]
		decision := RateGroupDecision{GroupName: groupName, OwnMultiplier: ownRate}
		if ownRate == nil {
			missingOwnRate = true
			decision.Message = "缺少自有分组倍率"
		} else {
			if minOwn == nil || *ownRate < *minOwn {
				value := *ownRate
				minOwn = &value
			}
			decision.Allowed = *row.UpstreamEffectiveMultiplier < *ownRate
			if decision.Allowed {
				decision.Message = "上游倍率低于自有分组倍率"
			} else {
				blocked = true
				decision.Message = "上游倍率大于或等于自有分组倍率"
			}
		}
		row.GroupDecisions = append(row.GroupDecisions, decision)
	}
	row.OwnGroupMultiplier = minOwn
	if blocked {
		row.RateGateStatus = RateGateBlocked
		row.SuggestedSchedulable = false
		row.RateGateMessage = "上游实际倍率大于或等于命中分组倍率，建议停用"
		return row
	}
	if missingOwnRate {
		row.RateGateStatus = RateGateMissing
		row.SuggestedSchedulable = !rateRule.StopWhenMissingRate
		row.RateGateMessage = "缺少部分自有分组倍率"
		return row
	}
	row.RateGateStatus = RateGateAllowed
	row.SuggestedSchedulable = true
	row.RateGateMessage = "上游实际倍率低于自有分组倍率，允许调用"
	return row
}

func assignRecommendedPriorities(rows []RatePlanRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		leftOK := left.RateGateStatus == RateGateAllowed && left.UpstreamEffectiveMultiplier != nil
		rightOK := right.RateGateStatus == RateGateAllowed && right.UpstreamEffectiveMultiplier != nil
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && rightOK && !ratesEqual(*left.UpstreamEffectiveMultiplier, *right.UpstreamEffectiveMultiplier) {
			return *left.UpstreamEffectiveMultiplier < *right.UpstreamEffectiveMultiplier
		}
		return left.AdminAccountName < right.AdminAccountName
	})
	priority := 0
	var lastRate *float64
	for i := range rows {
		if rows[i].RateGateStatus != RateGateAllowed || rows[i].UpstreamEffectiveMultiplier == nil {
			continue
		}
		if lastRate == nil || !ratesEqual(*rows[i].UpstreamEffectiveMultiplier, *lastRate) {
			priority++
			value := *rows[i].UpstreamEffectiveMultiplier
			lastRate = &value
		}
		priorityValue := priority
		rows[i].SuggestedPriority = &priorityValue
	}
}

func summarizeRatePlan(rows []RatePlanRow) RateApplySummary {
	var summary RateApplySummary
	for _, row := range rows {
		summary.Total++
		switch row.RateGateStatus {
		case RateGateAllowed:
			summary.Allowed++
		case RateGateBlocked:
			summary.Blocked++
		case RateGateMissing:
			summary.Missing++
		case RateGateSkipped:
			summary.Skipped++
		}
		if row.CurrentSchedulable == nil || *row.CurrentSchedulable != row.SuggestedSchedulable {
			if row.SuggestedSchedulable {
				summary.WouldEnable++
			} else {
				summary.WouldDisable++
			}
		}
		if row.SuggestedPriority != nil && (row.CurrentPriority == nil || *row.CurrentPriority != *row.SuggestedPriority) {
			summary.PriorityChanges++
		}
	}
	return summary
}

func applyRatePlanToChannel(channel *ChannelStatus, row RatePlanRow) {
	channel.AccountRateMultiplier = row.AccountRateMultiplier
	channel.AccountPriority = row.AccountPriority
	channel.UpstreamMultiplier = row.UpstreamMultiplier
	channel.UpstreamEffectiveMultiplier = row.UpstreamEffectiveMultiplier
	channel.OwnGroupMultiplier = row.OwnGroupMultiplier
	channel.RecommendedPriority = row.SuggestedPriority
	channel.RateGateStatus = row.RateGateStatus
	channel.RateGateMessage = row.RateGateMessage
}

func ownGroupRateByName(state *my_sites.State) map[string]*float64 {
	rates := map[string]*float64{}
	if state == nil {
		return rates
	}
	for _, group := range state.OwnGroups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		value := group.Multiplier
		rates[name] = &value
	}
	return rates
}

func upstreamGroupMultiplier(site *upstream.Site, conn my_sites.RealConnection) *float64 {
	if site == nil {
		return nil
	}
	for _, group := range site.Metrics.Groups {
		if strings.TrimSpace(group.ID) == strings.TrimSpace(conn.UpstreamGroupID) || strings.TrimSpace(group.Name) == strings.TrimSpace(conn.UpstreamGroupName) {
			if group.Multiplier == nil {
				return nil
			}
			value := *group.Multiplier
			return &value
		}
	}
	return nil
}

func effectiveMultiplier(multiplier *float64, rechargeRate float64) *float64 {
	if multiplier == nil {
		return nil
	}
	value := *multiplier
	if rechargeRate > 0 {
		value *= rechargeRate
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

func (s *Service) refreshSiteBalanceIfStale(ctx context.Context, site *upstream.Site, intervalMinutes int) (*upstream.Site, error) {
	if site == nil || s.upstreams == nil {
		return site, nil
	}
	intervalMinutes = clampInt(intervalMinutes, 1, 24*60)
	if !isSiteBalanceStale(site, intervalMinutes) {
		return site, nil
	}
	return s.upstreams.RefreshSite(ctx, site.ID)
}

func isSiteBalanceStale(site *upstream.Site, intervalMinutes int) bool {
	if site == nil || site.LastSyncedAt == nil {
		return true
	}
	lastSynced := time.UnixMilli(*site.LastSyncedAt)
	if lastSynced.After(time.Now()) {
		return false
	}
	return time.Since(lastSynced) >= time.Duration(clampInt(intervalMinutes, 1, 24*60))*time.Minute
}

func shouldProtectPausedRule(rule Rule) bool {
	if rule.ManualPaused {
		return true
	}
	switch rule.LastStatus {
	case StatusFailed, StatusAutoPaused, StatusBalancePaused, StatusManualPaused, StatusUnsupported:
		return true
	default:
		return false
	}
}

func ratesEqual(left, right float64) bool {
	return math.Abs(left-right) < 1e-9
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultIfBlank(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func testModelForGroupType(groupType string, config TestModelConfig) string {
	switch strings.ToLower(strings.TrimSpace(groupType)) {
	case "anthropic", "claude":
		return defaultIfBlank(config.AnthropicModelID, DefaultAnthropicTestModel)
	default:
		return defaultIfBlank(config.OpenAIModelID, DefaultOpenAITestModel)
	}
}

func convertedBalance(site *upstream.Site) *float64 {
	if site == nil || site.Metrics.Balance.Value == nil || site.RechargeRate <= 0 {
		return nil
	}
	value := *site.Metrics.Balance.Value * site.RechargeRate
	return &value
}

func ownGroupsForConnection(state *my_sites.State, conn my_sites.RealConnection) []string {
	if state == nil {
		return append([]string(nil), conn.OwnGroupIDs...)
	}
	groups := make([]string, 0)
	target := my_sites.UpstreamGroupRef{SiteID: conn.UpstreamSiteID, GroupName: conn.UpstreamGroupName}
	for _, mapping := range state.Mappings {
		for _, candidate := range mapping.UpstreamTargets {
			if candidate == target {
				groups = append(groups, mapping.OwnGroup)
				break
			}
		}
	}
	if len(groups) > 0 {
		return groups
	}
	return append([]string(nil), conn.OwnGroupIDs...)
}

func applyStats(stats *SummaryStats, row ChannelStatus) {
	stats.Total++
	if isAvailable(row) {
		stats.Available++
	}
	if !row.Enabled {
		stats.MonitorPaused++
	}
	if row.Schedulable != nil && !*row.Schedulable {
		stats.DispatchPaused++
	}
	switch row.Status {
	case StatusFailed, StatusAutoPaused:
		stats.Failed++
	case StatusBalancePaused:
		stats.BalancePaused++
	case StatusManualPaused:
		stats.ManualPaused++
	case StatusUnsupported:
		stats.Unsupported++
	}
}

func isAvailable(row ChannelStatus) bool {
	if !row.Enabled || !row.Supported {
		return false
	}
	if row.Schedulable != nil && !*row.Schedulable {
		return false
	}
	switch row.Status {
	case StatusFailed, StatusAutoPaused, StatusBalancePaused, StatusManualPaused, StatusUnsupported:
		return false
	default:
		return true
	}
}

func uniqueRuleIDs(ruleIDs []string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(ruleIDs))
	for _, ruleID := range ruleIDs {
		id := strings.TrimSpace(ruleID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func schedulablePtr(value bool) *bool { return &value }

func normalizedStatus(status string) string {
	if strings.TrimSpace(status) == "" {
		return StatusUnknown
	}
	return status
}

func statusRank(status string) int {
	switch status {
	case StatusFailed, StatusAutoPaused:
		return 0
	case StatusBalancePaused:
		return 1
	case StatusManualPaused:
		return 2
	case StatusUnsupported:
		return 3
	case StatusHealthy:
		return 4
	default:
		return 5
	}
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func newResultID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return hex.EncodeToString(bytes)
}

type requestError string

func (e requestError) Error() string { return string(e) }
