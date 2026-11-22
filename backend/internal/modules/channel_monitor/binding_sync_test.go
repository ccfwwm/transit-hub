package channel_monitor

import (
	"context"
	"testing"

	"transithub/backend/internal/modules/my_sites"
)

type bindingSyncStub struct{ result my_sites.MonitorBindingResult }

func (b bindingSyncStub) SyncMonitorBindings(context.Context, string, string, my_sites.MonitorBindingRequest) (my_sites.MonitorBindingResult, error) {
	return b.result, nil
}

func TestBindingSyncPreservesExistingControlsAndOnlyTakesOverNewAccount(t *testing.T) {
	for _, imported := range []bool{false, true} {
		ctx, s, store, p := targetFixture()
		rule := store.mustRule("conn-1")
		rule.Enabled = false
		rule.ManualPaused = true
		rule.PriorityManaged = false
		store.rules[rule.ConnectionID] = rule
		s.SetBindingSync(bindingSyncStub{my_sites.MonitorBindingResult{Matched: 1, Items: []my_sites.MonitorBindingItem{{ConnectionID: "conn-1", Status: "matched", NeedsTakeover: imported}}}})
		result, e := s.syncMonitoringBindings(ctx, "user-1", "admin-1", my_sites.MonitorBindingRequest{})
		if e != nil || result.Items[0].Status != "matched" {
			t.Fatalf("sync failed: %+v %v", result, e)
		}
		saved := store.mustRule("conn-1")
		if saved.Enabled || !saved.ManualPaused || saved.PriorityManaged {
			t.Fatal("sync reset manual controls")
		}
		if !imported && len(p.schedulableCalls) != 0 {
			t.Fatal("existing binding changed remote scheduling")
		}
		if imported && (!saved.SchedulableManaged || !saved.AutoEnableBlocked || saved.LastAppliedSchedulable == nil || *saved.LastAppliedSchedulable) {
			t.Fatal("imported account did not enter the existing takeover gate")
		}
		if len(p.testOptions) != 0 {
			t.Fatal("binding synchronization triggered paid model generation")
		}
	}
}
