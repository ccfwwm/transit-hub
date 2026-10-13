package channel_monitor

import (
	"context"
	"errors"
	"testing"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

type priorityTestStore struct {
	*fakeRepository
	groups []PriorityGroupRule
}

func (s *priorityTestStore) ListPriorityGroupRules(context.Context, string, string) ([]PriorityGroupRule, error) {
	return s.groups, nil
}

func (s *priorityTestStore) SavePriorityGroupRule(_ context.Context, rule PriorityGroupRule) error {
	for i := range s.groups {
		if s.groups[i].GroupID == rule.GroupID {
			s.groups[i] = rule
			return nil
		}
	}
	s.groups = append(s.groups, rule)
	return nil
}

func priorityFixture() (*testService, *priorityTestStore) {
	repo := &priorityTestStore{fakeRepository: newFakeRepository(), groups: []PriorityGroupRule{
		{GroupID: "own-1", Enabled: true, AutoApplyOnRateChange: true},
		{GroupID: "own-2", Enabled: true, AutoApplyOnRateChange: true},
	}}
	s := newTestService(repo.fakeRepository)
	s.Service.store = repo
	s.state().OwnGroups = []my_sites.GroupOption{{ID: "own-1", Name: "PLUS", Multiplier: 1}, {ID: "own-2", Name: "OTHER", Multiplier: 1}}
	s.conns.connections = []my_sites.RealConnection{
		{ID: "conn-1", AdminAccountID: "123", UpstreamSiteID: "site-1", OwnGroupIDs: []string{"own-1"}},
		{ID: "conn-2", AdminAccountID: "456", UpstreamSiteID: "site-2", OwnGroupIDs: []string{"own-1"}},
		{ID: "conn-3", AdminAccountID: "789", UpstreamSiteID: "site-3", OwnGroupIDs: []string{"own-2"}},
	}
	for i, conn := range s.conns.connections {
		rule := DefaultRule("user-1", "admin-1", conn.ID)
		rule.UpstreamMultiplierOverride = floatPtr([]float64{0.8, 0.2, 0.01}[i])
		// These channels are deliberately protected from dispatch changes.
		rule.ManualPaused = true
		repo.rules[conn.ID] = rule
	}
	s.platform.accounts = []AdminAccountStatus{
		{ID: "123", Schedulable: boolPtr(false), Priority: intPtr(9)},
		{ID: "456", Schedulable: boolPtr(false), Priority: intPtr(9)},
		{ID: "789", Schedulable: boolPtr(false), Priority: intPtr(9)},
	}
	return s, repo
}

func TestPriorityApplyRanksPausedChannelsWithinSelectedGroup(t *testing.T) {
	s, _ := priorityFixture()
	result, err := s.ApplyPriorityRule(context.Background(), "user-1", "own-1", "manual-priority")
	if err != nil || !result.Success || result.Total != 2 || result.PriorityUpdated != 2 || result.SkippedCount != 0 {
		t.Fatalf("priority-only apply did not process paused channels: %+v err=%v", result, err)
	}
	if len(s.platform.schedulableCalls) != 0 {
		t.Fatal("priority apply changed dispatch state")
	}
	want := []priorityCall{{AccountID: "456", Priority: 1}, {AccountID: "123", Priority: 2}}
	for i, call := range s.platform.priorityCalls {
		if call != want[i] {
			t.Fatalf("wrong group ranking: %+v", s.platform.priorityCalls)
		}
	}
	preview, err := s.SummaryInGroup(context.Background(), "user-1", "own-1")
	if err != nil || preview.RateRule.Summary.PriorityChanges != 2 {
		t.Fatalf("preview disagrees with apply: %+v err=%v", preview.RateRule.Summary, err)
	}
	for _, channel := range preview.Channels {
		switch channel.AdminAccountID {
		case "123":
			if channel.RecommendedPriority == nil || *channel.RecommendedPriority != 2 {
				t.Fatalf("incorrect preview for 123: %+v", channel)
			}
		case "456":
			if channel.RecommendedPriority == nil || *channel.RecommendedPriority != 1 {
				t.Fatalf("incorrect preview for 456: %+v", channel)
			}
		case "789":
			if channel.RecommendedPriority != nil {
				t.Fatal("unrelated group retained a stale global recommendation")
			}
		}
	}
}

func TestPriorityRateChangeReranksWholeAffectedGroup(t *testing.T) {
	s, repo := priorityFixture()
	old := upstream.Metrics{Groups: []upstream.GroupInfo{{ID: "g", Multiplier: floatPtr(0.1)}}}
	changed := upstream.Metrics{Groups: []upstream.GroupInfo{{ID: "g", Multiplier: floatPtr(0.8)}}}
	s.ApplyPriorityAfterSync(context.Background(), "user-1", "admin-1", "site-1", old, old)
	if len(s.platform.priorityCalls) != 0 {
		t.Fatal("unchanged multiplier triggered writes")
	}
	s.ApplyPriorityAfterSync(context.Background(), "user-1", "admin-1", "site-1", old, changed)
	if len(s.platform.priorityCalls) != 2 || s.platform.priorityCalls[0].AccountID != "456" || s.platform.priorityCalls[1].Priority != 2 || len(repo.rateResults) != 1 {
		t.Fatalf("rate-change applied only the changed site or unrelated group: %+v results=%+v", s.platform.priorityCalls, repo.rateResults)
	}
	s.platform.priorityCalls = nil
	repo.groups[0].Enabled = false
	s.ApplyPriorityAfterSync(context.Background(), "user-1", "admin-1", "site-1", old, changed)
	if len(s.platform.priorityCalls) != 0 {
		t.Fatal("disabled group triggered writes")
	}
	repo.groups[0].Enabled = true
	repo.groups[0].AutoApplyOnRateChange = false
	s.ApplyPriorityAfterSync(context.Background(), "user-1", "admin-1", "site-1", old, changed)
	if len(s.platform.priorityCalls) != 0 {
		t.Fatal("disabled automatic trigger wrote priorities")
	}
}

func TestPriorityManualSettingSurvivesAutomaticChanges(t *testing.T) {
	s, repo := priorityFixture()
	ctx := context.Background()
	if err := s.SetRulePriorityInGroup(ctx, "user-1", "conn-1", 7, "own-1"); err != nil {
		t.Fatal(err)
	}
	s.platform.accounts[0].Priority = intPtr(7)
	s.platform.priorityCalls = nil
	result, err := s.applyPriorityForGroup(ctx, "user-1", "admin-1", "own-1", "site-1", "rate-change")
	if err != nil || result.SkippedCount != 1 || len(s.platform.priorityCalls) != 1 || s.platform.priorityCalls[0].AccountID != "456" {
		t.Fatalf("manual priority overwritten: %+v calls=%+v err=%v", result, s.platform.priorityCalls, err)
	}
	if _, err := s.ApplyPriorityRule(ctx, "user-1", "own-1", "manual-priority"); err != nil || repo.mustRule("conn-1").PriorityConflict {
		t.Fatalf("explicit group apply did not release manual choice: %v", err)
	}
}

type failingPriorityPlatform struct{ *fakeMonitorPlatform }

func (p failingPriorityPlatform) UpdateSub2APIAdminAccountPriority(upstream.Session, string, int) error {
	return errors.New("admin.channelMonitor.errors.priorityNotApplied")
}

func TestPriorityApplyRejectsMissingGroupAndReportsFailedWrites(t *testing.T) {
	s, _ := priorityFixture()
	for _, group := range []string{"", "nonexistent"} {
		if _, err := s.ApplyPriorityRule(context.Background(), "user-1", group, "manual-priority"); err == nil {
			t.Fatalf("accepted group %q", group)
		}
	}
	s.Service.platform = failingPriorityPlatform{s.platform}
	result, err := s.ApplyPriorityRule(context.Background(), "user-1", "own-1", "manual-priority")
	if err != nil || result.Success || result.PriorityUpdated != 0 || result.SkippedCount != 2 {
		t.Fatalf("failed writes reported as successful: %+v err=%v", result, err)
	}
}

func TestPriorityPlanClearsStaleRecommendationsAndRanksEqualRates(t *testing.T) {
	rows := []RatePlanRow{
		{ConnectionID: "a", AdminAccountID: "1", Supported: true, UpstreamEffectiveMultiplier: floatPtr(0), SuggestedPriority: intPtr(99)},
		{ConnectionID: "b", AdminAccountID: "2", Supported: true, UpstreamEffectiveMultiplier: floatPtr(0)},
		{ConnectionID: "c", AdminAccountID: "3", Supported: false, UpstreamEffectiveMultiplier: floatPtr(0), SuggestedPriority: intPtr(99)},
	}
	connections := []my_sites.RealConnection{{ID: "a", OwnGroupIDs: []string{"g"}}, {ID: "b", OwnGroupIDs: []string{"g"}}, {ID: "c", OwnGroupIDs: []string{"g"}}}
	plan := priorityPlanForGroup(rows, connections, "g")
	if *plan[0].SuggestedPriority != 1 || *plan[1].SuggestedPriority != 1 || plan[2].SuggestedPriority != nil {
		t.Fatalf("incorrect zero/equal/missing-account ranking: %+v", plan)
	}
}
