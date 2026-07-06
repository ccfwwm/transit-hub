package channel_monitor

import (
	"context"
	"errors"
	"testing"

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
	if got := service.platform.schedulableCalls; len(got) != 1 || got[0] {
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
	if got := service.platform.schedulableCalls; len(got) != 1 || got[0] {
		t.Fatalf("expected one disable call, got %+v", got)
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
	if got := service.platform.schedulableCalls; len(got) != 1 || !got[0] {
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

func TestResumeRuleRestoresManualPausedHealthyChannel(t *testing.T) {
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
	if got := service.platform.schedulableCalls; len(got) != 1 || !got[0] {
		t.Fatalf("expected one enable call, got %+v", got)
	}
	updated := repo.mustRule("conn-1")
	if updated.ManualPaused {
		t.Fatalf("expected manual pause cleared, got %+v", updated)
	}
}

type fakeRepository struct {
	rules   map[string]Rule
	results []Result
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
	return nil, nil
}
func (r *fakeRepository) ListDueRules(context.Context, int) ([]Rule, error) { return nil, nil }

type fakeConnections struct {
	conn my_sites.RealConnection
}

func (f fakeConnections) ListRealConnections(context.Context, string, string) ([]my_sites.RealConnection, error) {
	return []my_sites.RealConnection{f.conn}, nil
}
func (f fakeConnections) GetRealConnection(_ context.Context, id, _ string, _ string) (*my_sites.RealConnection, error) {
	if id == f.conn.ID {
		return &f.conn, nil
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
	site *upstream.Site
}

func (f fakeUpstreams) GetSite(context.Context, string) (*upstream.Site, error) {
	return f.site, nil
}

type fakeAccounts struct{}

func (fakeAccounts) RequireCurrentID(context.Context, string) (string, error) {
	return "admin-1", nil
}

type fakeMonitorPlatform struct {
	testErr          error
	schedulableCalls []bool
}

func (f *fakeMonitorPlatform) TestSub2APIAdminAccount(upstream.Session, string, AccountTestOptions) (AccountTestResult, error) {
	if f.testErr != nil {
		return AccountTestResult{}, f.testErr
	}
	return AccountTestResult{Success: true, Message: "ok", LatencyMS: 42, Model: "gpt-test"}, nil
}

func (f *fakeMonitorPlatform) SetSub2APIAdminAccountSchedulable(_ upstream.Session, _ string, schedulable bool) error {
	f.schedulableCalls = append(f.schedulableCalls, schedulable)
	return nil
}

type testService struct {
	*Service
	platform  *fakeMonitorPlatform
	upstreams fakeUpstreams
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
	upstreams := fakeUpstreams{site: &upstream.Site{
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
	service := NewService(repo, fakeConnections{conn: conn}, fakeStateStore{state: state}, upstreams, platform, fakeAccounts{})
	return &testService{Service: service, platform: platform, upstreams: upstreams}
}
