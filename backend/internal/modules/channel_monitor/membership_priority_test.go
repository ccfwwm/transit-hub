package channel_monitor

import (
	"context"
	"testing"
	"transithub/backend/internal/modules/my_sites"
)

func TestMembershipReranksAfterRemovalAndPreservesManualPriority(t *testing.T) {
	s, repo := priorityFixture()
	ctx := context.Background()
	// Membership changes are automatic even with the rate-change trigger off.
	repo.groups[0].Enabled = false
	result, e := s.applyPriorityForGroup(ctx, "user-1", "admin-1", "own-1", "", "membership-change")
	if e != nil || !result.Success || result.PriorityUpdated != 2 {
		t.Fatalf("membership ranking failed: %+v %v", result, e)
	}
	s.conns.connections = append(s.conns.connections[:1], s.conns.connections[2:]...)
	s.platform.priorityCalls = nil
	s.platform.accounts[0].Priority = intPtr(2)
	result, e = s.applyPriorityForGroup(ctx, "user-1", "admin-1", "own-1", "", "membership-change")
	if e != nil || !result.Success || len(s.platform.priorityCalls) != 1 || s.platform.priorityCalls[0] != (priorityCall{AccountID: "123", Priority: 1}) {
		t.Fatalf("deletion did not close ranking gap: %+v %v", s.platform.priorityCalls, e)
	}
	rule := repo.mustRule("conn-1")
	rule.PriorityConflict = true
	repo.rules[rule.ID] = rule
	s.platform.priorityCalls = nil
	result, e = s.applyPriorityForGroup(ctx, "user-1", "admin-1", "own-1", "", "membership-change")
	if e != nil || result.SkippedCount != 1 || len(s.platform.priorityCalls) != 0 || !repo.mustRule(rule.ID).PriorityConflict {
		t.Fatal("automatic membership change overwrote explicit manual priority")
	}
}
func TestMembershipHashesIgnoreOrderButTrackAccountAndGroups(t *testing.T) {
	a := my_sites.RealConnection{ID: "a", AdminAccountID: "1", OwnGroupIDs: []string{"g"}}
	b := my_sites.RealConnection{ID: "b", AdminAccountID: "2", OwnGroupIDs: []string{"g"}}
	h := membershipHashes([]my_sites.RealConnection{a, b})
	if h["g"] != membershipHashes([]my_sites.RealConnection{b, a})["g"] {
		t.Fatal("order triggered mutation")
	}
	if h["g"] == membershipHashes([]my_sites.RealConnection{a})["g"] {
		t.Fatal("removal ignored")
	}
	b.OwnGroupIDs = []string{"other"}
	if h["g"] == membershipHashes([]my_sites.RealConnection{a, b})["g"] {
		t.Fatal("group move ignored")
	}
}

func TestSharedAccountPriorityDoesNotOscillateBetweenGroups(t *testing.T) {
	conns := []my_sites.RealConnection{{ID: "shared", OwnGroupIDs: []string{"a", "b"}}, {ID: "cheap", OwnGroupIDs: []string{"b"}}, {ID: "costly", OwnGroupIDs: []string{"a"}}}
	rows := []RatePlanRow{{ConnectionID: "shared", AdminAccountID: "2", Supported: true, UpstreamEffectiveMultiplier: floatPtr(0.5)}, {ConnectionID: "cheap", AdminAccountID: "1", Supported: true, UpstreamEffectiveMultiplier: floatPtr(0.1)}, {ConnectionID: "costly", AdminAccountID: "3", Supported: true, UpstreamEffectiveMultiplier: floatPtr(0.9)}}
	for _, g := range []string{"a", "b"} {
		plan := priorityPlanForGroup(rows, conns, g)
		for _, row := range plan {
			if row.ConnectionID == "shared" && (row.SuggestedPriority == nil || *row.SuggestedPriority != 2) {
				t.Fatalf("shared priority differs in %s: %+v", g, plan)
			}
		}
	}
}

func TestReadPlanUsesTheSameRankAsMembershipWrites(t *testing.T) {
	s, _ := priorityFixture()
	ctx := context.Background()
	conns, rules, state, accounts, e := s.rateRuleContext(ctx, "user-1", "admin-1", true)
	if e != nil {
		t.Fatal(e)
	}
	rows, _ := s.buildRatePlan(ctx, conns, rules, state, accounts, DefaultRateRule("user-1", "admin-1"))
	for _, g := range []string{"own-1", "own-2"} {
		for _, expected := range priorityPlanForGroup(rows, conns, g) {
			for _, actual := range rows {
				if actual.ConnectionID == expected.ConnectionID && (actual.SuggestedPriority == nil || *actual.SuggestedPriority != *expected.SuggestedPriority) {
					t.Fatalf("cached rank differs from applied rank in %s: %+v", g, rows)
				}
			}
		}
	}
}
func TestPrimaryModelIsPerAccountAndPreservesOtherSettings(t *testing.T) {
	s, repo := priorityFixture()
	c := s.conns.connections[0]
	c.UserID = "user-1"
	c.WorkspaceAdminAccountID = "admin-1"
	before := repo.mustRule(c.ID)
	if e := s.SetPrimaryModel(context.Background(), c, "gpt-image-2.5"); e != nil {
		t.Fatal(e)
	}
	after := repo.mustRule(c.ID)
	if after.TestModelID != "gpt-image-2.5" || after.Enabled != before.Enabled || after.ManualPaused != before.ManualPaused || repo.mustRule("conn-2").TestModelID != "" {
		t.Fatal("primary model leaked or other settings changed")
	}
	config := DefaultTestModelConfig("user-1", "admin-1")
	opts := effectiveTestOptionsForGroup(after, c, s.state(), config, "own-1")
	if opts.Mode != "image" || opts.ModelID != "gpt-image-2.5" {
		t.Fatalf("image override used inherited text request: %+v", opts)
	}
	if e := s.SetPrimaryModel(context.Background(), c, ""); e != nil || repo.mustRule(c.ID).TestModelID != "" {
		t.Fatal("could not restore inherited model")
	}
}
