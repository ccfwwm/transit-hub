package channel_monitor

import (
	"context"
	"math"
	"testing"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

func TestChannelRateOverrideSaveResetAndValidation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	for _, rate := range []float64{0, 0.1234, 1.8} {
		updated, err := service.UpdateRule(ctx, "user-1", "conn-1", UpdateRuleRequest{UpstreamMultiplierOverride: floatPtr(rate)})
		if err != nil || updated.UpstreamMultiplierOverride == nil || *updated.UpstreamMultiplierOverride != rate {
			t.Fatalf("save %v: %+v %v", rate, updated, err)
		}
	}
	updated, err := service.UpdateRule(ctx, "user-1", "conn-1", UpdateRuleRequest{Enabled: boolPtr(false)})
	if err != nil || updated.UpstreamMultiplierOverride == nil || *updated.UpstreamMultiplierOverride != 1.8 {
		t.Fatal("unrelated edit lost override")
	}
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1)} {
		if _, err := service.UpdateRule(ctx, "user-1", "conn-1", UpdateRuleRequest{UpstreamMultiplierOverride: floatPtr(invalid)}); err == nil {
			t.Fatalf("accepted invalid rate %v", invalid)
		}
	}
	updated, err = service.UpdateRule(ctx, "user-1", "conn-1", UpdateRuleRequest{ResetUpstreamMultiplierOverride: true})
	if err != nil || updated.UpstreamMultiplierOverride != nil {
		t.Fatalf("reset failed: %+v %v", updated, err)
	}
}

func TestCustomActualRateControlsPlanAndHighRateSwitch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo)
	service.state().OwnGroups = []my_sites.GroupOption{{Name: "PLUS", Multiplier: 1}}
	service.upstreams.site.RechargeRate = 2
	service.upstreams.site.Metrics.Groups = []upstream.GroupInfo{{ID: "g-upstream", Name: "GPT-4o", Multiplier: floatPtr(0.1)}}
	for _, rate := range []float64{0, 0.4, 1, 1.2} {
		for _, allow := range []bool{false, true} {
			rule := repo.mustRule("conn-1")
			rule.UpstreamMultiplierOverride = floatPtr(rate)
			rule.AllowWhenUpstreamRateGteOwn = allow
			row := service.buildRatePlanRow(ctx, service.conns.connections[0], rule, service.state(), service.platform.accounts[0], DefaultRateRule("user-1", "admin-1"))
			expected := rate < 1 || allow
			if row.UpstreamEffectiveMultiplier == nil || *row.UpstreamEffectiveMultiplier != rate || row.SuggestedSchedulable != expected || row.GroupDecisions[0].Allowed != expected {
				t.Fatalf("rate=%v allow=%v row=%+v", rate, allow, row)
			}
			rule.LastStatus = StatusBalancePaused
			row = service.buildRatePlanRow(ctx, service.conns.connections[0], rule, service.state(), service.platform.accounts[0], DefaultRateRule("user-1", "admin-1"))
			if row.SuggestedSchedulable {
				t.Fatal("rate option bypassed balance protection")
			}
		}
	}
}

func TestMissingRemoteAccountDoesNotDisableKeyCheck(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(repo)
	conn := service.conns.connections[0]
	conn.UpstreamKey = "test-key"
	rule := repo.mustRule("conn-1")
	for _, accounts := range []map[string]AdminAccountStatus{nil, {}} {
		row := service.channelStatus(context.Background(), conn, rule, service.state(), accounts, DefaultTestModelConfig("user-1", "admin-1"))
		if !row.CheckSupported || row.Supported || row.Status == StatusUnsupported || row.DispatchUnavailableReason == "" {
			t.Fatalf("incorrect capabilities: %+v", row)
		}
		if accounts == nil && row.DispatchUnavailableReason != "admin.channelMonitor.errors.accountsUnavailable" {
			t.Fatal("fetch failure reported as deletion")
		}
	}
}

func TestSummaryReconcilesLiveConflictForTakeover(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(repo)
	rule := repo.mustRule("conn-1")
	rule.SchedulableManaged = true
	rule.LastAppliedSchedulable = boolPtr(false)
	rule.DesiredSchedulable = boolPtr(false)
	row := service.channelStatus(context.Background(), service.conns.connections[0], rule, service.state(), map[string]AdminAccountStatus{"123": service.platform.accounts[0]}, DefaultTestModelConfig("user-1", "admin-1"))
	if !row.TakeoverAvailable || !row.SchedulableConflict || !row.Supported || row.Schedulable == nil || !*row.Schedulable {
		t.Fatalf("live conflict hidden: %+v", row)
	}
}
