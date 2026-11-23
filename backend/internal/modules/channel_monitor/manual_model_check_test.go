package channel_monitor

import (
	"testing"
	"time"
)

func TestOldOneShotMediaDoesNotSubmit(t *testing.T) {
	_, s, store, p := targetFixture()
	store.testModelConfig.StatusSource = "upstream"
	conn := s.conns.connections[0]
	site, _ := s.upstreams.GetSite(nil, conn.UpstreamSiteID)
	o := AccountTestOptions{ModelID: "good", Mode: "image", Adapter: "openai_image", OneShot: true}
	task := MediaTask{Key: "one-shot-media", RuleID: conn.ID, UserID: "user-1", AdminAccountID: "admin-1", Options: o, CreatedAt: time.Now(), Status: "submitting"}
	store.tasks[task.Key] = task
	s.executeMedia(store, task, conn, *site, o, false)
	got, e := store.GetMediaTask(nil, task.Key)
	if e != nil || got.Status != "blocked" || len(p.testOptions) != 0 || len(p.schedulableCalls) != 0 || len(store.states) != 0 {
		t.Fatalf("legacy one-shot entered periodic dispatch or submitted work: %+v %v", got, e)
	}
}
func TestDispatchStateReasons(t *testing.T) {
	for _, tc := range []struct {
		row  ChannelStatus
		want string
	}{
		{ChannelStatus{}, "unknown"},
		{ChannelStatus{Schedulable: boolPtr(true)}, "enabled"},
		{ChannelStatus{Schedulable: boolPtr(false), ManualPaused: true}, "manual"},
		{ChannelStatus{Schedulable: boolPtr(false), Status: StatusBalancePaused}, "balance"},
		{ChannelStatus{Schedulable: boolPtr(false), RateGateStatus: RateGateBlocked}, "rate"},
		{ChannelStatus{Schedulable: boolPtr(false), Status: StatusAutoPaused}, "auto"},
		{ChannelStatus{Schedulable: boolPtr(false), Status: StatusHealthy}, "disabled"},
	} {
		if got := dispatchState(tc.row); got != tc.want {
			t.Fatalf("%+v => %s, want %s", tc.row, got, tc.want)
		}
	}
}
