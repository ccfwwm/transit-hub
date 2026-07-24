package channel_monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

func TestSummaryCreatesDefaultRulesAndGroupCounts(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)

	summary, err := service.Summary(ctx, "user-1")
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	if len(repo.rules) != 1 {
		t.Fatalf("expected one default rule, got %d", len(repo.rules))
	}
	rule := repo.rules["conn-1"]
	if rule.CheckIntervalMinutes != DefaultCheckIntervalMinutes {
		t.Fatalf("expected default interval %d, got %d", DefaultCheckIntervalMinutes, rule.CheckIntervalMinutes)
	}
	if rule.FailureThreshold != DefaultFailureThreshold {
		t.Fatalf("expected default failure threshold %d, got %d", DefaultFailureThreshold, rule.FailureThreshold)
	}
	if rule.BalanceThreshold != DefaultBalanceThreshold {
		t.Fatalf("expected default balance threshold %.1f, got %.1f", DefaultBalanceThreshold, rule.BalanceThreshold)
	}

	if summary.Stats.Total != 1 || summary.Stats.Available != 1 {
		t.Fatalf("expected 1 total and 1 available, got %+v", summary.Stats)
	}
	if len(summary.Groups) != 1 {
		t.Fatalf("expected one group summary, got %d", len(summary.Groups))
	}
	group := summary.Groups[0]
	if group.GroupName != "PLUS" || group.Total != 1 || group.Available != 1 {
		t.Fatalf("unexpected group summary: %+v", group)
	}
}

func TestSummaryKeepsRecentResultsAsEmptySliceWhenStoreReturnsNil(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.returnNilResults = true
	service := newTestService(repo)

	summary, err := service.Summary(ctx, "user-1")
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if len(summary.Channels) != 1 {
		t.Fatalf("expected one channel, got %d", len(summary.Channels))
	}
	if summary.Channels[0].RecentResults == nil {
		t.Fatalf("expected recentResults to stay an empty slice when store returns nil")
	}
}

func TestRunRuleSuccessWritesHealthyResultWithoutPausing(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")

	result, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}

	if !result.Success || result.Status != StatusHealthy {
		t.Fatalf("expected healthy success result, got %+v", result)
	}
	updated := repo.mustRule("conn-1")
	if updated.LastStatus != StatusHealthy || updated.ConsecutiveFailures != 0 {
		t.Fatalf("expected healthy rule, got %+v", updated)
	}
	if len(service.platform.schedulableCalls) != 0 {
		t.Fatalf("expected no schedulable changes, got %+v", service.platform.schedulableCalls)
	}
}

func TestRunRuleUsesSessionProviderSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.SetSessionProvider(fakeSessionProvider{session: upstream.Session{
		Platform:    upstream.PlatformSub2API,
		BaseURL:     "https://admin.example.com",
		AccessToken: "fresh-admin-token",
		TokenType:   "Bearer",
	}})
	rule := repo.mustRule("conn-1")

	_, err := service.RunRule(ctx, rule.ID, "manual")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}
	if len(service.platform.testSessions) != 1 {
		t.Fatalf("expected one test call, got %d", len(service.platform.testSessions))
	}
	if service.platform.testSessions[0].AccessToken != "fresh-admin-token" {
		t.Fatalf("expected refreshed session token, got %q", service.platform.testSessions[0].AccessToken)
	}
}

func TestRunRuleUsesDefaultOpenAITestModel(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")

	_, err := service.RunRule(ctx, rule.ID, "manual")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}
	if len(service.platform.testOptions) != 1 {
		t.Fatalf("expected one test option, got %d", len(service.platform.testOptions))
	}
	if service.platform.testOptions[0].ModelID != DefaultOpenAITestModel {
		t.Fatalf("expected default openai model %q, got %q", DefaultOpenAITestModel, service.platform.testOptions[0].ModelID)
	}
}

func TestRunRuleUsesDefaultAnthropicTestModel(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	conn := service.conns.connections[0]
	conn.GroupType = "anthropic"
	conn.UpstreamGroupName = "Claude"
	service.conns.connections[0] = conn
	rule := repo.mustRule("conn-1")

	_, err := service.RunRule(ctx, rule.ID, "manual")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}
	if len(service.platform.testOptions) != 1 {
		t.Fatalf("expected one test option, got %d", len(service.platform.testOptions))
	}
	if service.platform.testOptions[0].ModelID != DefaultAnthropicTestModel {
		t.Fatalf("expected default anthropic model %q, got %q", DefaultAnthropicTestModel, service.platform.testOptions[0].ModelID)
	}
}

func TestRunRuleUsesDefaultGrokTestModel(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	conn := service.conns.connections[0]
	conn.GroupType = "grok"
	service.conns.connections[0] = conn
	rule := repo.mustRule("conn-1")

	_, err := service.RunRule(ctx, rule.ID, "manual")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}
	if len(service.platform.testOptions) != 1 {
		t.Fatalf("expected one test option, got %d", len(service.platform.testOptions))
	}
	if service.platform.testOptions[0].ModelID != DefaultGrokTestModel {
		t.Fatalf("expected default grok model %q, got %q", DefaultGrokTestModel, service.platform.testOptions[0].ModelID)
	}
}

func TestRunRuleUsesSavedTestModelConfig(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.testModelConfig = &TestModelConfig{
		UserID:           "user-1",
		AdminAccountID:   "admin-1",
		OpenAIModelID:    "gpt-custom",
		AnthropicModelID: "claude-custom",
		GrokModelID:      "grok-custom",
	}
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")

	_, err := service.RunRule(ctx, rule.ID, "manual")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}
	if len(service.platform.testOptions) != 1 {
		t.Fatalf("expected one test option, got %d", len(service.platform.testOptions))
	}
	if service.platform.testOptions[0].ModelID != "gpt-custom" {
		t.Fatalf("expected saved model, got %q", service.platform.testOptions[0].ModelID)
	}
}

func TestRunRuleUsesSavedGrokTestModelForXAI(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.testModelConfig = &TestModelConfig{UserID: "user-1", AdminAccountID: "admin-1", GrokModelID: "grok-custom"}
	service := newTestService(repo)
	conn := service.conns.connections[0]
	conn.GroupType = "xai"
	service.conns.connections[0] = conn
	rule := repo.mustRule("conn-1")

	_, err := service.RunRule(ctx, rule.ID, "manual")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}
	if len(service.platform.testOptions) != 1 || service.platform.testOptions[0].ModelID != "grok-custom" {
		t.Fatalf("expected saved grok model, got %#v", service.platform.testOptions)
	}
}

func TestRunRuleSkipsWhenAdminSessionCannotBeRecovered(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.SetSessionProvider(fakeSessionProvider{err: upstreamAuthError()})
	rule := repo.mustRule("conn-1")

	result, err := service.RunRule(ctx, rule.ID, "manual")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}
	if result.Status != StatusUnknown || !result.Success {
		t.Fatalf("expected unknown successful skip result, got %+v", result)
	}
	updated := repo.mustRule("conn-1")
	if updated.ConsecutiveFailures != 0 {
		t.Fatalf("admin auth failure should not count against channel, got %d", updated.ConsecutiveFailures)
	}
	if len(service.platform.testSessions) != 0 {
		t.Fatalf("expected no account test when admin session cannot be recovered")
	}
	if len(service.platform.schedulableCalls) != 0 {
		t.Fatalf("expected no schedulable changes when admin session cannot be recovered")
	}
}

func TestRunRulePausesAfterFailureThreshold(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.platform.testErr = errors.New("upstream timeout")
	rule := repo.mustRule("conn-1")
	rule.FailureThreshold = 2
	repo.rules[rule.ID] = rule

	first, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("first RunRule returned error: %v", err)
	}
	if first.Status != StatusFailed {
		t.Fatalf("expected first result failed, got %+v", first)
	}
	if len(service.platform.schedulableCalls) != 0 {
		t.Fatalf("first failure should not pause, got calls %+v", service.platform.schedulableCalls)
	}

	second, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("second RunRule returned error: %v", err)
	}
	if second.Status != StatusAutoPaused {
		t.Fatalf("expected auto paused result, got %+v", second)
	}
	if got := service.platform.schedulableCalls; len(got) != 1 || got[0].Schedulable {
		t.Fatalf("expected one disable call, got %+v", got)
	}
}

func TestRunRulePausesWhenBalanceBelowThreshold(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	balance := 0.5
	service.upstreams.site.Metrics.Balance.Value = &balance
	rule := repo.mustRule("conn-1")

	result, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}

	if result.Status != StatusBalancePaused {
		t.Fatalf("expected balance paused, got %+v", result)
	}
	if got := service.platform.schedulableCalls; len(got) != 1 || got[0].Schedulable {
		t.Fatalf("expected one disable call, got %+v", got)
	}
}

func TestRunRuleRefreshesStaleBalanceBeforeThresholdCheck(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	oldBalance := 10.0
	service.upstreams.site.Metrics.Balance.Value = &oldBalance
	oldSyncedAt := timeNowAdd(-10 * 60 * 1000)
	service.upstreams.site.LastSyncedAt = &oldSyncedAt
	newBalance := 0.4
	service.upstreams.refreshedSite = cloneSite(service.upstreams.site)
	service.upstreams.refreshedSite.Metrics.Balance.Value = &newBalance
	nowSyncedAt := timeNowAdd(0)
	service.upstreams.refreshedSite.LastSyncedAt = &nowSyncedAt
	rule := repo.mustRule("conn-1")

	result, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}

	if service.upstreams.refreshCount != 1 {
		t.Fatalf("expected stale upstream balance to refresh once, got %d", service.upstreams.refreshCount)
	}
	if result.Status != StatusBalancePaused {
		t.Fatalf("expected refreshed low balance to pause channel, got %+v", result)
	}
	if got := service.platform.schedulableCalls; len(got) != 1 || got[0].Schedulable {
		t.Fatalf("expected one disable call after refreshed low balance, got %+v", got)
	}
}

func TestRunRuleDoesNotRefreshFreshBalance(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	syncedAt := timeNowAdd(-60 * 1000)
	service.upstreams.site.LastSyncedAt = &syncedAt
	rule := repo.mustRule("conn-1")

	result, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}

	if service.upstreams.refreshCount != 0 {
		t.Fatalf("expected fresh upstream balance not to refresh, got %d", service.upstreams.refreshCount)
	}
	if result.Status != StatusHealthy {
		t.Fatalf("expected healthy result, got %+v", result)
	}
}

func TestRunRuleAutoPausesAfterBalanceRefreshFailures(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	syncedAt := timeNowAdd(-10 * 60 * 1000)
	service.upstreams.site.LastSyncedAt = &syncedAt
	service.upstreams.refreshErr = errors.New("upstream balance timeout")
	rule := repo.mustRule("conn-1")
	rule.FailureThreshold = 2
	repo.rules[rule.ID] = rule

	first, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("first RunRule returned error: %v", err)
	}
	if first.Status != StatusFailed {
		t.Fatalf("expected first stale balance refresh failure to fail, got %+v", first)
	}
	if len(service.platform.schedulableCalls) != 0 {
		t.Fatalf("first refresh failure should not pause, got %+v", service.platform.schedulableCalls)
	}

	second, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("second RunRule returned error: %v", err)
	}
	if second.Status != StatusAutoPaused {
		t.Fatalf("expected second refresh failure to auto pause, got %+v", second)
	}
	if got := service.platform.schedulableCalls; len(got) != 1 || got[0].Schedulable {
		t.Fatalf("expected one disable call after repeated refresh failures, got %+v", got)
	}
}

func TestRunRuleRestoresAutoPausedHealthyChannel(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")
	rule.LastStatus = StatusAutoPaused
	repo.rules[rule.ID] = rule

	result, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}

	if result.Status != StatusHealthy {
		t.Fatalf("expected healthy result, got %+v", result)
	}
	if got := service.platform.schedulableCalls; len(got) != 1 || !got[0].Schedulable {
		t.Fatalf("expected one enable call, got %+v", got)
	}
}

func TestManualPausedRuleDoesNotAutoRestore(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")
	rule.ManualPaused = true
	rule.LastStatus = StatusManualPaused
	repo.rules[rule.ID] = rule

	result, err := service.RunRule(ctx, rule.ID, "scheduled")
	if err != nil {
		t.Fatalf("RunRule returned error: %v", err)
	}

	if result.Status != StatusManualPaused {
		t.Fatalf("expected manual paused result, got %+v", result)
	}
	if len(service.platform.schedulableCalls) != 0 {
		t.Fatalf("manual pause should not auto restore, got %+v", service.platform.schedulableCalls)
	}
}

func TestResumeRuleRestoresManualPausedDispatch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")
	rule.ManualPaused = true
	rule.LastStatus = StatusManualPaused
	repo.rules[rule.ID] = rule

	result, err := service.ResumeRule(ctx, "user-1", rule.ID)
	if err != nil {
		t.Fatalf("ResumeRule returned error: %v", err)
	}

	if result.Status != StatusHealthy {
		t.Fatalf("expected healthy result, got %+v", result)
	}
	if got := service.platform.schedulableCalls; len(got) != 1 || !got[0].Schedulable {
		t.Fatalf("expected one enable call, got %+v", got)
	}
	updated := repo.mustRule("conn-1")
	if updated.ManualPaused {
		t.Fatalf("expected manual pause cleared, got %+v", updated)
	}
	if updated.DesiredSchedulable == nil || !*updated.DesiredSchedulable {
		t.Fatalf("expected desired schedulable true, got %+v", updated.DesiredSchedulable)
	}
}

func TestSetRuleEnabledOnlyControlsDetection(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")

	updated, err := service.UpdateRule(ctx, "user-1", rule.ID, UpdateRuleRequest{Enabled: boolPtr(false)})
	if err != nil {
		t.Fatalf("UpdateRule returned error: %v", err)
	}

	if updated.Enabled {
		t.Fatalf("expected monitoring disabled, got %+v", updated)
	}
	if got := service.platform.schedulableCalls; len(got) != 0 {
		t.Fatalf("monitor toggle must not change schedulable, got %+v", got)
	}
}

func TestManualRunStillChecksDisabledRule(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")
	rule.Enabled = false
	repo.rules[rule.ID] = rule

	result, err := service.RunRuleForUser(ctx, "user-1", rule.ID, "manual")
	if err != nil {
		t.Fatalf("RunRuleForUser returned error: %v", err)
	}

	if result.Status != StatusHealthy {
		t.Fatalf("expected manual run to perform a real check, got %+v", result)
	}
}

func TestSetRuleSchedulableOnlyControlsRemoteDispatch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")

	if err := service.SetRuleSchedulable(ctx, "user-1", rule.ID, false); err != nil {
		t.Fatalf("SetRuleSchedulable returned error: %v", err)
	}

	updated := repo.mustRule("conn-1")
	if !updated.Enabled {
		t.Fatalf("dispatch toggle must not disable monitoring, got %+v", updated)
	}
	if updated.DesiredSchedulable == nil || *updated.DesiredSchedulable {
		t.Fatalf("expected desired schedulable false to be stored, got %+v", updated.DesiredSchedulable)
	}
	if got := service.platform.schedulableCalls; len(got) != 1 || got[0].AccountID != "123" || got[0].Schedulable {
		t.Fatalf("expected one remote disable call for account 123, got %+v", got)
	}
}

func TestSetRuleSchedulableSummaryReflectsRequestedState(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	remoteDisabled := false
	service.platform.accounts = []AdminAccountStatus{{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: &remoteDisabled}}
	rule := repo.mustRule("conn-1")

	if err := service.SetRuleSchedulable(ctx, "user-1", rule.ID, true); err != nil {
		t.Fatalf("SetRuleSchedulable returned error: %v", err)
	}
	summary, err := service.Summary(ctx, "user-1")
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	channel := summary.Channels[0]
	if channel.Schedulable == nil || !*channel.Schedulable {
		t.Fatalf("expected summary to reflect requested schedulable=true, got %+v", channel.Schedulable)
	}
	if summary.Stats.DispatchPaused != 0 || summary.Stats.Available != 1 {
		t.Fatalf("expected enabled dispatch to be available, got %+v", summary.Stats)
	}
}

func TestSetRulePriorityUpdatesSub2APIAccountPriority(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")

	if err := service.SetRulePriority(ctx, "user-1", rule.ID, 3); err != nil {
		t.Fatalf("SetRulePriority returned error: %v", err)
	}

	if got := service.platform.priorityCalls; len(got) != 1 || got[0].AccountID != "123" || got[0].Priority != 3 {
		t.Fatalf("expected one priority update for account 123, got %+v", got)
	}
	updated := repo.mustRule("conn-1")
	if updated.LastMessage != "手动设置优先级为 3" {
		t.Fatalf("expected priority message stored, got %q", updated.LastMessage)
	}
}

func TestBulkUpdateRulesAppliesSelectedRules(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	second := DefaultRule("user-1", "admin-1", "conn-2")
	repo.rules[second.ID] = second
	service := newTestService(repo)
	service.conns.connections = append(service.conns.connections, my_sites.RealConnection{
		ID:                "conn-2",
		UpstreamSiteID:    "site-1",
		UpstreamGroupID:   "g-upstream-2",
		UpstreamGroupName: "GPT-4.1",
		AdminAccountID:    "456",
		AdminAccountName:  "A-【site】-GPT-4.1",
		OwnGroupIDs:       []string{"own-1"},
		GroupType:         "openai",
	})

	updated, err := service.BulkUpdateRules(ctx, "user-1", BulkUpdateRuleRequest{
		RuleIDs:              []string{"conn-1"},
		Enabled:              boolPtr(false),
		CheckIntervalMinutes: intPtr(3),
		FailureThreshold:     intPtr(5),
		BalanceThreshold:     floatPtr(2.5),
	})
	if err != nil {
		t.Fatalf("BulkUpdateRules returned error: %v", err)
	}
	if len(updated) != 1 || updated[0].ID != "conn-1" {
		t.Fatalf("expected only conn-1 updated, got %+v", updated)
	}
	first := repo.mustRule("conn-1")
	if first.Enabled || first.CheckIntervalMinutes != 3 || first.FailureThreshold != 5 || first.BalanceThreshold != 2.5 {
		t.Fatalf("unexpected first rule after bulk update: %+v", first)
	}
	other := repo.mustRule("conn-2")
	if !other.Enabled || other.CheckIntervalMinutes != DefaultCheckIntervalMinutes {
		t.Fatalf("unselected rule should remain unchanged, got %+v", other)
	}
}

func TestSummaryIncludesRemoteSchedulableAndRecentResults(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.results = append(repo.results,
		Result{ID: "r1", RuleID: "conn-1", ConnectionID: "conn-1", Status: StatusHealthy, Success: true, LatencyMS: intPtr(40)},
		Result{ID: "r2", RuleID: "conn-1", ConnectionID: "conn-1", Status: StatusFailed, Success: false},
	)
	service := newTestService(repo)
	disabled := false
	service.platform.accounts = []AdminAccountStatus{{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: &disabled}}

	summary, err := service.Summary(ctx, "user-1")
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.Stats.DispatchPaused != 1 {
		t.Fatalf("expected one dispatch paused channel, got %+v", summary.Stats)
	}
	if summary.Stats.Available != 0 {
		t.Fatalf("schedulable=false should not count as available, got %+v", summary.Stats)
	}
	channel := summary.Channels[0]
	if channel.Schedulable == nil || *channel.Schedulable {
		t.Fatalf("expected schedulable=false in channel status, got %+v", channel.Schedulable)
	}
	if len(channel.RecentResults) != 2 {
		t.Fatalf("expected recent results in summary, got %+v", channel.RecentResults)
	}
	if channel.RecentTotal != 2 || channel.RecentSuccess != 1 || channel.UptimePercent != 50 {
		t.Fatalf("unexpected recent stats: total=%d success=%d uptime=%.1f", channel.RecentTotal, channel.RecentSuccess, channel.UptimePercent)
	}
}

func TestApplyRateRuleDisablesChannelsAtOrAboveOwnGroupMultiplier(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.state().OwnGroups = []my_sites.GroupOption{{Name: "PLUS", Multiplier: 1.2}}
	service.upstreams.site.Metrics.Groups = []upstream.GroupInfo{{ID: "g-upstream", Name: "GPT-4o", Multiplier: floatPtr(1.2)}}
	service.platform.accounts = []AdminAccountStatus{{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: boolPtr(true), Priority: intPtr(9)}}

	rule, err := service.UpdateRateRule(ctx, "user-1", UpdateRateRuleRequest{
		Enabled:          boolPtr(true),
		AutoApplyOnCheck: boolPtr(true),
		UpdatePriority:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("UpdateRateRule returned error: %v", err)
	}
	if !rule.Enabled {
		t.Fatalf("expected saved rate rule enabled, got %+v", rule)
	}

	result, err := service.ApplyRateRule(ctx, "user-1", "manual")
	if err != nil {
		t.Fatalf("ApplyRateRule returned error: %v", err)
	}

	if result.DisabledCount != 1 || result.EnabledCount != 0 {
		t.Fatalf("expected one disabled channel, got %+v", result)
	}
	if got := service.platform.schedulableCalls; len(got) != 1 || got[0].AccountID != "123" || got[0].Schedulable {
		t.Fatalf("expected one disable call for account 123, got %+v", got)
	}
	updated := repo.mustRule("conn-1")
	if updated.DesiredSchedulable == nil || *updated.DesiredSchedulable {
		t.Fatalf("expected desired schedulable false, got %+v", updated.DesiredSchedulable)
	}
	row := result.Rows[0]
	if row.RateGateStatus != RateGateBlocked || row.OwnGroupMultiplier == nil || row.UpstreamEffectiveMultiplier == nil {
		t.Fatalf("expected blocked row with multipliers, got %+v", row)
	}
}

func TestApplyRateRuleEnablesCheapChannelsAndWritesPriority(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.state().OwnGroups = []my_sites.GroupOption{{Name: "PLUS", Multiplier: 1.2}, {Name: "PRO", Multiplier: 1.5}}
	service.upstreams.site.Metrics.Groups = []upstream.GroupInfo{
		{ID: "g-upstream", Name: "GPT-4o", Multiplier: floatPtr(0.7)},
		{ID: "g-upstream-2", Name: "GPT-4.1", Multiplier: floatPtr(0.9)},
	}
	second := DefaultRule("user-1", "admin-1", "conn-2")
	second.DesiredSchedulable = boolPtr(false)
	repo.rules[second.ID] = second
	service.conns.connections = append(service.conns.connections, my_sites.RealConnection{
		ID:                "conn-2",
		UpstreamSiteID:    "site-1",
		UpstreamGroupID:   "g-upstream-2",
		UpstreamGroupName: "GPT-4.1",
		AdminAccountID:    "456",
		AdminAccountName:  "A-【site】-GPT-4.1",
		OwnGroupIDs:       []string{"2"},
		GroupType:         "openai",
	})
	service.state().Mappings = []my_sites.GroupMapping{
		{OwnGroup: "PLUS", UpstreamTargets: []my_sites.UpstreamGroupRef{{SiteID: "site-1", GroupName: "GPT-4o"}}},
		{OwnGroup: "PRO", UpstreamTargets: []my_sites.UpstreamGroupRef{{SiteID: "site-1", GroupName: "GPT-4.1"}}},
	}
	service.platform.accounts = []AdminAccountStatus{
		{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: boolPtr(false), Priority: intPtr(9)},
		{ID: "456", Name: "A-【site】-GPT-4.1", Schedulable: boolPtr(false), Priority: intPtr(9)},
	}
	if _, err := service.UpdateRateRule(ctx, "user-1", UpdateRateRuleRequest{Enabled: boolPtr(true), UpdatePriority: boolPtr(true)}); err != nil {
		t.Fatalf("UpdateRateRule returned error: %v", err)
	}

	result, err := service.ApplyRateRule(ctx, "user-1", "manual")
	if err != nil {
		t.Fatalf("ApplyRateRule returned error: %v", err)
	}

	if result.EnabledCount != 2 || result.PriorityUpdated != 2 {
		t.Fatalf("expected two enabled and prioritized channels, got %+v", result)
	}
	if got := service.platform.schedulableCalls; len(got) != 2 || !got[0].Schedulable || !got[1].Schedulable {
		t.Fatalf("expected two enable calls, got %+v", got)
	}
	if got := service.platform.priorityCalls; len(got) != 2 || got[0].AccountID != "123" || got[0].Priority != 1 || got[1].AccountID != "456" || got[1].Priority != 2 {
		t.Fatalf("expected priority updates by ascending multiplier, got %+v", got)
	}
}

func TestApplyRateRuleDoesNotOverwriteManualPriorityOverride(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.state().OwnGroups = []my_sites.GroupOption{{Name: "PLUS", Multiplier: 1.2}}
	service.upstreams.site.Metrics.Groups = []upstream.GroupInfo{{ID: "g-upstream", Name: "GPT-4o", Multiplier: floatPtr(0.7)}}
	// The rule last wrote priority 1, but the administrator changed it to 7 on Sub2API.
	rule := repo.mustRule("conn-1")
	rule.PriorityManaged = true
	rule.LastAppliedPriority = intPtr(1)
	rule.OriginalPriority = intPtr(9)
	repo.rules[rule.ID] = rule
	service.platform.accounts = []AdminAccountStatus{{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: boolPtr(true), Priority: intPtr(7)}}
	if _, err := service.UpdateRateRule(ctx, "user-1", UpdateRateRuleRequest{Enabled: boolPtr(true), UpdatePriority: boolPtr(true)}); err != nil {
		t.Fatalf("UpdateRateRule returned error: %v", err)
	}

	if _, err := service.ApplyRateRule(ctx, "user-1", "scheduled"); err != nil {
		t.Fatalf("ApplyRateRule returned error: %v", err)
	}
	if len(service.platform.priorityCalls) != 0 {
		t.Fatalf("automatic rule overwrote manual priority: %+v", service.platform.priorityCalls)
	}
	updated := repo.mustRule("conn-1")
	if !updated.PriorityConflict {
		t.Fatalf("expected manual priority conflict to be recorded, got %+v", updated)
	}
}

func TestApplyRateRuleDoesNotOverwriteManualSchedulableOverride(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.state().OwnGroups = []my_sites.GroupOption{{Name: "PLUS", Multiplier: 1.2}}
	service.upstreams.site.Metrics.Groups = []upstream.GroupInfo{{ID: "g-upstream", Name: "GPT-4o", Multiplier: floatPtr(1.5)}}
	// The rule last disabled this account, then the administrator re-enabled it remotely.
	rule := repo.mustRule("conn-1")
	rule.SchedulableManaged = true
	rule.LastAppliedSchedulable = boolPtr(false)
	rule.OriginalSchedulable = boolPtr(true)
	repo.rules[rule.ID] = rule
	service.platform.accounts = []AdminAccountStatus{{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: boolPtr(true), Priority: intPtr(9)}}
	if _, err := service.UpdateRateRule(ctx, "user-1", UpdateRateRuleRequest{Enabled: boolPtr(true), UpdatePriority: boolPtr(false)}); err != nil {
		t.Fatalf("UpdateRateRule returned error: %v", err)
	}

	if _, err := service.ApplyRateRule(ctx, "user-1", "scheduled"); err != nil {
		t.Fatalf("ApplyRateRule returned error: %v", err)
	}
	if len(service.platform.schedulableCalls) != 0 {
		t.Fatalf("automatic rule overwrote manual schedulable value: %+v", service.platform.schedulableCalls)
	}
	if !repo.mustRule("conn-1").SchedulableConflict {
		t.Fatalf("expected manual schedulable conflict to be recorded")
	}
}

func TestApplyRateRuleKeepsManualOverrideAfterRemoteValueMatchesPreviousWrite(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.state().OwnGroups = []my_sites.GroupOption{{Name: "PLUS", Multiplier: 1.2}}
	service.upstreams.site.Metrics.Groups = []upstream.GroupInfo{{ID: "g-upstream", Name: "GPT-4o", Multiplier: floatPtr(1.5)}}

	// The remote value happens to match our previous write again, but the
	// recorded conflict still means the administrator owns this field.
	rule := repo.mustRule("conn-1")
	rule.SchedulableManaged = true
	rule.SchedulableConflict = true
	rule.LastAppliedSchedulable = boolPtr(true)
	rule.OriginalSchedulable = boolPtr(false)
	repo.rules[rule.ID] = rule
	service.platform.accounts = []AdminAccountStatus{{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: boolPtr(true), Priority: intPtr(9)}}

	if _, err := service.ApplyRateRule(ctx, "user-1", "scheduled"); err != nil {
		t.Fatalf("ApplyRateRule returned error: %v", err)
	}
	if len(service.platform.schedulableCalls) != 0 {
		t.Fatalf("automatic rule reclaimed an administrator override: %+v", service.platform.schedulableCalls)
	}
}

func TestUpstreamGroupMultiplierUsesStableIDAfterRename(t *testing.T) {
	site := &upstream.Site{Metrics: upstream.Metrics{Groups: []upstream.GroupInfo{{
		ID: "group-1", Name: "renamed-group", Multiplier: floatPtr(0.8),
	}}}}
	connection := my_sites.RealConnection{UpstreamGroupID: "group-1", UpstreamGroupName: "legacy-group"}

	multiplier := upstreamGroupMultiplier(site, connection)
	if multiplier == nil || *multiplier != 0.8 {
		t.Fatalf("expected renamed group multiplier to resolve by ID, got %v", multiplier)
	}
}

func TestDisablingRateRuleRestoresOnlySystemManagedPriority(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")
	rule.PriorityManaged = true
	rule.OriginalPriority = intPtr(9)
	rule.LastAppliedPriority = intPtr(1)
	repo.rules[rule.ID] = rule
	service.platform.accounts = []AdminAccountStatus{{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: boolPtr(true), Priority: intPtr(1)}}
	if _, err := service.UpdateRateRule(ctx, "user-1", UpdateRateRuleRequest{Enabled: boolPtr(true)}); err != nil {
		t.Fatalf("enable UpdateRateRule returned error: %v", err)
	}

	if _, err := service.UpdateRateRule(ctx, "user-1", UpdateRateRuleRequest{Enabled: boolPtr(false)}); err != nil {
		t.Fatalf("disable UpdateRateRule returned error: %v", err)
	}
	if got := service.platform.priorityCalls; len(got) != 1 || got[0].Priority != 9 {
		t.Fatalf("expected original priority restore, got %+v", got)
	}
	updated := repo.mustRule("conn-1")
	if updated.PriorityManaged || updated.LastAppliedPriority != nil || updated.OriginalPriority != nil {
		t.Fatalf("expected ownership to clear after restore, got %+v", updated)
	}
}

func TestRateRuleDoesNotReEnableBalancePausedChannel(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.state().OwnGroups = []my_sites.GroupOption{{Name: "PLUS", Multiplier: 1.2}}
	service.upstreams.site.Metrics.Groups = []upstream.GroupInfo{{ID: "g-upstream", Name: "GPT-4o", Multiplier: floatPtr(0.5)}}
	service.platform.accounts = []AdminAccountStatus{{ID: "123", Name: "A-【site】-GPT-4o", Schedulable: boolPtr(false), Priority: intPtr(9)}}
	rule := repo.mustRule("conn-1")
	rule.LastStatus = StatusBalancePaused
	rule.DesiredSchedulable = boolPtr(false)
	repo.rules[rule.ID] = rule
	if _, err := service.UpdateRateRule(ctx, "user-1", UpdateRateRuleRequest{Enabled: boolPtr(true), UpdatePriority: boolPtr(true)}); err != nil {
		t.Fatalf("UpdateRateRule returned error: %v", err)
	}

	result, err := service.ApplyRateRule(ctx, "user-1", "manual")
	if err != nil {
		t.Fatalf("ApplyRateRule returned error: %v", err)
	}

	if result.EnabledCount != 0 || result.SkippedCount != 1 {
		t.Fatalf("expected balance-paused channel skipped, got %+v", result)
	}
	if len(service.platform.schedulableCalls) != 0 {
		t.Fatalf("balance paused channel must not be re-enabled, got %+v", service.platform.schedulableCalls)
	}
	if result.Rows[0].RateGateStatus != RateGateSkipped {
		t.Fatalf("expected skipped gate status, got %+v", result.Rows[0])
	}
}

type fakeRepository struct {
	rules            map[string]Rule
	results          []Result
	returnNilResults bool
	rateRule         *RateRule
	rateResults      []RateApplyResult
	testModelConfig  *TestModelConfig
}

func newFakeRepository() *fakeRepository {
	repo := &fakeRepository{rules: map[string]Rule{}}
	repo.rules["conn-1"] = DefaultRule("user-1", "admin-1", "conn-1")
	return repo
}

func (r *fakeRepository) mustRule(connectionID string) Rule {
	rule, ok := r.rules[connectionID]
	if !ok {
		panic("missing fake rule")
	}
	return rule
}

func (r *fakeRepository) EnsureSchema(context.Context) error                      { return nil }
func (r *fakeRepository) EnsureRulesForExistingConnections(context.Context) error { return nil }
func (r *fakeRepository) EnsureRuleForConnection(_ context.Context, userID, adminAccountID string, conn my_sites.RealConnection) (Rule, error) {
	if rule, ok := r.rules[conn.ID]; ok {
		return rule, nil
	}
	rule := DefaultRule(userID, adminAccountID, conn.ID)
	r.rules[conn.ID] = rule
	return rule, nil
}
func (r *fakeRepository) ListRulesForWorkspace(context.Context, string, string) ([]Rule, error) {
	rules := make([]Rule, 0, len(r.rules))
	for _, rule := range r.rules {
		rules = append(rules, rule)
	}
	return rules, nil
}
func (r *fakeRepository) GetRule(_ context.Context, id string) (*Rule, error) {
	for _, rule := range r.rules {
		if rule.ID == id {
			next := rule
			return &next, nil
		}
	}
	return nil, nil
}
func (r *fakeRepository) UpdateRule(_ context.Context, rule Rule) error {
	r.rules[rule.ConnectionID] = rule
	return nil
}
func (r *fakeRepository) AddResult(_ context.Context, result Result) error {
	r.results = append(r.results, result)
	return nil
}
func (r *fakeRepository) ListRecentResults(context.Context, string, int) ([]Result, error) {
	if r.returnNilResults {
		return nil, nil
	}
	results := []Result{}
	for _, result := range r.results {
		if result.RuleID == "conn-1" {
			results = append(results, result)
		}
	}
	return results, nil
}
func (r *fakeRepository) ListDueRules(context.Context, int) ([]Rule, error) { return nil, nil }
func (r *fakeRepository) GetRateRule(context.Context, string, string) (*RateRule, error) {
	if r.rateRule == nil {
		return nil, nil
	}
	next := *r.rateRule
	return &next, nil
}
func (r *fakeRepository) SaveRateRule(_ context.Context, rule RateRule) error {
	r.rateRule = &rule
	return nil
}
func (r *fakeRepository) AddRateApplyResult(_ context.Context, result RateApplyResult) error {
	r.rateResults = append(r.rateResults, result)
	return nil
}
func (r *fakeRepository) GetLastRateApplyResult(context.Context, string, string) (*RateApplyResult, error) {
	if len(r.rateResults) == 0 {
		return nil, nil
	}
	next := r.rateResults[len(r.rateResults)-1]
	return &next, nil
}
func (r *fakeRepository) GetTestModelConfig(context.Context, string, string) (*TestModelConfig, error) {
	if r.testModelConfig == nil {
		return nil, nil
	}
	next := *r.testModelConfig
	return &next, nil
}
func (r *fakeRepository) SaveTestModelConfig(_ context.Context, config TestModelConfig) error {
	r.testModelConfig = &config
	return nil
}

type fakeConnections struct {
	connections []my_sites.RealConnection
}

func (f *fakeConnections) ListRealConnections(context.Context, string, string) ([]my_sites.RealConnection, error) {
	return append([]my_sites.RealConnection(nil), f.connections...), nil
}
func (f *fakeConnections) GetRealConnection(_ context.Context, id, _ string, _ string) (*my_sites.RealConnection, error) {
	for _, conn := range f.connections {
		if id == conn.ID {
			next := conn
			return &next, nil
		}
	}
	return nil, nil
}

type fakeStateStore struct {
	state *my_sites.State
}

func (f fakeStateStore) Get(context.Context, string, string) (*my_sites.State, error) {
	return f.state, nil
}

type fakeUpstreams struct {
	site          *upstream.Site
	refreshedSite *upstream.Site
	refreshCount  int
	refreshErr    error
}

func (f *fakeUpstreams) GetSite(context.Context, string) (*upstream.Site, error) {
	return f.site, nil
}

func (f *fakeUpstreams) RefreshSite(context.Context, string) (*upstream.Site, error) {
	f.refreshCount++
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	if f.refreshedSite != nil {
		f.site = f.refreshedSite
	}
	return f.site, nil
}

type fakeAccounts struct{}

func (fakeAccounts) RequireCurrentID(context.Context, string) (string, error) {
	return "admin-1", nil
}

type fakeMonitorPlatform struct {
	testErr          error
	accounts         []AdminAccountStatus
	testSessions     []upstream.Session
	testOptions      []AccountTestOptions
	schedulableCalls []schedulableCall
	priorityCalls    []priorityCall
}

type schedulableCall struct {
	AccountID   string
	Schedulable bool
}

type priorityCall struct {
	AccountID string
	Priority  int
}

func (f *fakeMonitorPlatform) TestSub2APIAdminAccount(session upstream.Session, _ string, options AccountTestOptions) (AccountTestResult, error) {
	f.testSessions = append(f.testSessions, session)
	f.testOptions = append(f.testOptions, options)
	if f.testErr != nil {
		return AccountTestResult{}, f.testErr
	}
	return AccountTestResult{Success: true, Message: "ok", LatencyMS: 42, Model: "gpt-test"}, nil
}

func (f *fakeMonitorPlatform) SetSub2APIAdminAccountSchedulable(_ upstream.Session, accountID string, schedulable bool) error {
	f.schedulableCalls = append(f.schedulableCalls, schedulableCall{AccountID: accountID, Schedulable: schedulable})
	return nil
}

func (f *fakeMonitorPlatform) ListSub2APIAdminAccounts(upstream.Session) ([]AdminAccountStatus, error) {
	return append([]AdminAccountStatus(nil), f.accounts...), nil
}

func (f *fakeMonitorPlatform) UpdateSub2APIAdminAccountPriority(_ upstream.Session, accountID string, priority int) error {
	f.priorityCalls = append(f.priorityCalls, priorityCall{AccountID: accountID, Priority: priority})
	return nil
}

type fakeSessionProvider struct {
	session upstream.Session
	err     error
}

func (f fakeSessionProvider) RequireSession(context.Context, string, string) (upstream.Session, error) {
	if f.err != nil {
		return upstream.Session{}, f.err
	}
	return f.session, nil
}

func upstreamAuthError() error {
	return &upstream.RequestError{MessageKey: upstream.ErrorAuth, Platform: upstream.PlatformSub2API}
}

type testService struct {
	*Service
	platform  *fakeMonitorPlatform
	upstreams *fakeUpstreams
	conns     *fakeConnections
}

func newTestService(repo *fakeRepository) *testService {
	platform := &fakeMonitorPlatform{}
	conn := my_sites.RealConnection{
		ID:                "conn-1",
		UpstreamSiteID:    "site-1",
		UpstreamGroupID:   "g-upstream",
		UpstreamGroupName: "GPT-4o",
		AdminAccountID:    "123",
		AdminAccountName:  "A-【site】-GPT-4o",
		OwnGroupIDs:       []string{"own-1"},
		GroupType:         "openai",
	}
	balance := 2.0
	upstreams := &fakeUpstreams{site: &upstream.Site{
		ID:             "site-1",
		UserID:         "user-1",
		AdminAccountID: "admin-1",
		Name:           "pool.example.com",
		Platform:       upstream.PlatformSub2API,
		RechargeRate:   1,
		Status:         upstream.StatusConnected,
		Session:        &upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: "https://pool.example.com", AccessToken: "token", TokenType: "Bearer"},
		Metrics: upstream.Metrics{
			Balance: upstream.MetricValue{Value: &balance, Display: "2"},
		},
	}}
	state := &my_sites.State{
		UserID:         "user-1",
		AdminAccountID: "admin-1",
		Session:        upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: "https://admin.example.com", AccessToken: "admin-token", TokenType: "Bearer"},
		Mappings: []my_sites.GroupMapping{
			{OwnGroup: "PLUS", UpstreamTargets: []my_sites.UpstreamGroupRef{{SiteID: "site-1", GroupName: "GPT-4o"}}},
		},
	}
	conns := &fakeConnections{connections: []my_sites.RealConnection{conn}}
	service := NewService(repo, conns, fakeStateStore{state: state}, upstreams, platform, fakeAccounts{})
	return &testService{Service: service, platform: platform, upstreams: upstreams, conns: conns}
}

func (s *testService) state() *my_sites.State {
	store := s.Service.states.(fakeStateStore)
	return store.state
}

func boolPtr(value bool) *bool        { return &value }
func intPtr(value int) *int           { return &value }
func floatPtr(value float64) *float64 { return &value }

func timeNowAdd(deltaMS int64) int64 {
	return timeNow().Add(time.Duration(deltaMS) * time.Millisecond).UnixMilli()
}

func timeNow() time.Time {
	return time.Now()
}

func cloneSite(site *upstream.Site) *upstream.Site {
	if site == nil {
		return nil
	}
	next := *site
	return &next
}
