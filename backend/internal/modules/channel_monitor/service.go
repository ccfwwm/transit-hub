package channel_monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
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
}

type ConnectionStore interface {
	ListRealConnections(ctx context.Context, userID string, adminAccountID string) ([]my_sites.RealConnection, error)
	GetRealConnection(ctx context.Context, id string, userID string, adminAccountID string) (*my_sites.RealConnection, error)
}

type StateStore interface {
	Get(ctx context.Context, userID string, adminAccountID string) (*my_sites.State, error)
}

type UpstreamLookup interface {
	GetSite(ctx context.Context, siteID string) (*upstream.Site, error)
}

type MonitorPlatform interface {
	TestSub2APIAdminAccount(session upstream.Session, accountID string, options AccountTestOptions) (AccountTestResult, error)
	SetSub2APIAdminAccountSchedulable(session upstream.Session, accountID string, schedulable bool) error
	ListSub2APIAdminAccounts(session upstream.Session) ([]AdminAccountStatus, error)
}

type AdminAccountResolver interface {
	RequireCurrentID(ctx context.Context, userID string) (string, error)
}

type Service struct {
	store         Store
	conns         ConnectionStore
	states        StateStore
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
	state, _ := s.states.Get(ctx, userID, adminAccountID)
	accountsByID := s.adminAccountsByID(state)

	response := SummaryResponse{Channels: []ChannelStatus{}, Groups: []GroupSummary{}}
	groupMap := map[string]*GroupSummary{}
	for _, conn := range connections {
		rule := rulesByConnection[conn.ID]
		row := s.channelStatus(ctx, conn, rule, state, accountsByID)
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
	state, err := s.states.Get(ctx, userID, adminAccountID)
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
	state, err := s.states.Get(ctx, userID, adminAccountID)
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
	state, err := s.states.Get(ctx, userID, adminAccountID)
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
	state, err := s.states.Get(ctx, rule.UserID, rule.AdminAccountID)
	if err != nil {
		return Result{}, err
	}
	if state == nil || state.Session.Platform != upstream.PlatformSub2API {
		rule.ConsecutiveFailures = 0
		return finish(StatusUnsupported, false, "当前 admin 平台暂不支持主动监控", nil, "")
	}
	site, err := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
	if err != nil {
		return Result{}, err
	}
	if site == nil {
		return finish(StatusFailed, false, "上游站点不存在", nil, "")
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

	testResult, testErr := s.platform.TestSub2APIAdminAccount(state.Session, conn.AdminAccountID, AccountTestOptions{})
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
	return finish(StatusHealthy, true, message, &latency, testResult.Model)
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
		row.RecentResults = results
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
