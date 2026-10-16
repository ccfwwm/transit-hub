package channel_monitor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

type targetTestStore struct {
	*fakeRepository
	mu     sync.Mutex
	states map[string]map[string]TargetState
	tasks  map[string]MediaTask
	claims int
}

func (r *targetTestStore) ListTargetStates(context.Context, string, string) (map[string]map[string]TargetState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]map[string]TargetState{}
	for id, groups := range r.states {
		out[id] = map[string]TargetState{}
		for g, s := range groups {
			out[id][g] = s
		}
	}
	return out, nil
}
func (r *targetTestStore) SaveTargetState(_ context.Context, s TargetState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.states[s.RuleID] == nil {
		r.states[s.RuleID] = map[string]TargetState{}
	}
	r.states[s.RuleID][s.GroupID] = s
	return nil
}
func (r *targetTestStore) GetMediaTask(_ context.Context, key string) (*MediaTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.tasks[key]
	if !ok {
		return nil, nil
	}
	return &v, nil
}
func (r *targetTestStore) SaveMediaTask(_ context.Context, s MediaTask) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s.CreatedAt = r.tasks[s.Key].CreatedAt
	s.UpdatedAt = time.Now()
	r.tasks[s.Key] = s
	return nil
}
func (r *targetTestStore) ResetMediaTask(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tasks, key)
	return nil
}
func (r *targetTestStore) ActiveMediaTask(_ context.Context, id string) (*MediaTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.tasks {
		if v.RuleID == id && (v.Status == "submitting" || v.Status == "pending" || v.Status == "unknown" || v.Status == "timeout") {
			return &v, nil
		}
	}
	return nil, nil
}
func (r *targetTestStore) ClaimMediaTask(_ context.Context, s MediaTask, before time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.tasks[s.Key]; ok && (!(v.Status == "success" || v.Status == "failed") || v.UpdatedAt.After(before)) {
		return false, nil
	}
	s.CreatedAt = time.Now()
	s.UpdatedAt = time.Now()
	s.Status = "submitting"
	r.tasks[s.Key] = s
	r.claims++
	return true, nil
}

type targetTestPlatform struct {
	*fakeMonitorPlatform
	probe func(AccountTestOptions) (AccountTestResult, error)
}

func (p *targetTestPlatform) ProbeUpstreamConnection(_ context.Context, _ upstream.Site, _ my_sites.RealConnection, o AccountTestOptions) (AccountTestResult, error) {
	p.mu.Lock()
	p.testOptions = append(p.testOptions, o)
	p.mu.Unlock()
	return p.probe(o)
}
func (p *targetTestPlatform) SetSub2APIAdminAccountSchedulable(session upstream.Session, id string, value bool) error {
	p.fakeMonitorPlatform.SetSub2APIAdminAccountSchedulable(session, id, value)
	for i := range p.accounts {
		if p.accounts[i].ID == id {
			p.accounts[i].Schedulable = boolPtr(value)
		}
	}
	return nil
}

func targetFixture() (context.Context, *testService, *targetTestStore, *targetTestPlatform) {
	repo := newFakeRepository()
	ts := newTestService(repo)
	store := &targetTestStore{fakeRepository: repo, states: map[string]map[string]TargetState{}, tasks: map[string]MediaTask{}}
	ts.store = store
	ts.state().OwnGroups = []my_sites.GroupOption{{ID: "a", Name: "same", Platform: "openai"}, {ID: "b", Name: "same", Platform: "openai"}, {ID: "empty", Name: "empty", Platform: "openai"}}
	ts.conns.connections[0].OwnGroupIDs = []string{"a", "b"}
	ts.conns.connections[0].UpstreamKey = "fixture"
	config := DefaultTestModelConfig("user-1", "admin-1")
	config.GroupModels = []TestModelGroupConfig{{GroupID: "a", ModelID: "good"}, {GroupID: "b", ModelID: "bad"}}
	repo.testModelConfig = &config
	platform := &targetTestPlatform{fakeMonitorPlatform: ts.platform, probe: func(o AccountTestOptions) (AccountTestResult, error) {
		return AccountTestResult{Model: o.ModelID, Success: o.ModelID == "good", Message: o.ModelID}, nil
	}}
	ts.Service.platform = platform
	return context.Background(), ts, store, platform
}

func TestGroupIndependentHealthAndAllGroupDispatchPolicy(t *testing.T) {
	ctx, s, store, p := targetFixture()
	for i := 0; i < 3; i++ {
		if _, err := s.RunRule(ctx, "conn-1", "manual"); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.schedulableCalls) != 0 {
		t.Fatal("one failed group disabled healthy shared account")
	}
	if store.states["conn-1"]["a"].Status != StatusHealthy || store.states["conn-1"]["b"].ConsecutiveFailures != 3 {
		t.Fatalf("states %+v", store.states)
	}
	summary, err := s.SummaryInGroup(ctx, "user-1", "b")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]GroupSummary{}
	for _, g := range summary.Groups {
		byID[g.GroupID] = g
	}
	if byID["a"].Available != 1 || byID["b"].Available != 0 || byID["empty"].Available != 0 {
		t.Fatalf("incorrect independent summary: %+v", byID)
	}
	if summary.Channels[0].Status != StatusFailed || summary.Channels[0].EffectiveTestModelID != "bad" {
		t.Fatal(summary.Channels[0])
	}
	p.probe = func(o AccountTestOptions) (AccountTestResult, error) {
		return AccountTestResult{Model: o.ModelID, Message: "failed"}, nil
	}
	for i := 0; i < 2; i++ {
		if _, err = s.RunRule(ctx, "conn-1", "manual"); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.schedulableCalls) != 1 || p.schedulableCalls[0].Schedulable {
		t.Fatal("all groups did not trigger one disable", p.schedulableCalls)
	}
	p.probe = func(o AccountTestOptions) (AccountTestResult, error) {
		return AccountTestResult{Success: o.ModelID == "good", Model: o.ModelID}, nil
	}
	if _, err = s.RunRule(ctx, "conn-1", "manual"); err != nil {
		t.Fatal(err)
	}
	if len(p.schedulableCalls) != 2 || !p.schedulableCalls[1].Schedulable {
		t.Fatal("healthy group did not restore account", p.schedulableCalls)
	}
	if err = s.SetRuleSchedulable(ctx, "user-1", "conn-1", false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RunRule(ctx, "conn-1", "manual"); err != nil {
		t.Fatal(err)
	}
	if len(p.schedulableCalls) != 3 {
		t.Fatal("manual dispatch protection was overridden")
	}
}

func TestGroupConfigResetInheritanceAndRoundDeduplication(t *testing.T) {
	ctx, s, store, p := targetFixture()
	cfg := store.testModelConfig
	cfg.GroupModels[1].ModelID = "good"
	if _, err := s.RunRule(ctx, "conn-1", "manual"); err != nil {
		t.Fatal(err)
	}
	if len(p.testOptions) != 1 || len(store.results) != 2 {
		t.Fatalf("same parameters weren't coalesced: probes=%d results=%d", len(p.testOptions), len(store.results))
	}
	cfg.GroupModels[0].ModelID = "new-model"
	summary, err := s.SummaryInGroup(ctx, "user-1", "a")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Channels[0].Status != StatusUnknown || summary.Channels[0].LastCheckedAt != nil {
		t.Fatal("config change retained old healthy state")
	}
	// Clearing group A must use platform default, never group B's model.
	cfg.GroupModels = cfg.GroupModels[1:]
	options := effectiveTestOptionsForGroup(store.mustRule("conn-1"), s.conns.connections[0], s.state(), *cfg, "a")
	if options.ModelID != DefaultOpenAITestModel || options.Source != "global" {
		t.Fatal(options)
	}
	rule := store.mustRule("conn-1")
	rule.TestModelID = "override"
	for _, group := range []string{"a", "b"} {
		if o := effectiveTestOptionsForGroup(rule, s.conns.connections[0], s.state(), *cfg, group); o.ModelID != "override" || o.Source != "custom" {
			t.Fatal(o)
		}
	}
}

func TestMediaSubmissionSurvivesRepeatedClicksAndRestart(t *testing.T) {
	ctx, s, store, p := targetFixture()
	store.testModelConfig.GroupModels = []TestModelGroupConfig{{GroupID: "a", ModelID: "gpt-image-2", Mode: "image"}, {GroupID: "b", ModelID: "gpt-image-2", Mode: "image"}}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	p.probe = func(o AccountTestOptions) (AccountTestResult, error) {
		started <- struct{}{}
		<-release
		return AccountTestResult{Success: true, Model: o.ModelID, Message: "decoded"}, nil
	}
	for i := 0; i < 3; i++ {
		result, err := s.RunRule(ctx, "conn-1", "manual")
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != StatusGenerating {
			t.Fatal(result)
		}
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker not started")
	}
	if store.claims != 1 {
		t.Fatal("duplicate paid submission", store.claims)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, _ := store.GetMediaTask(ctx, "conn-1|"+configFingerprint(s.conns.connections[0], effectiveTestOptionsForGroup(store.mustRule("conn-1"), s.conns.connections[0], s.state(), *store.testModelConfig, "a").options()))
		if task != nil && task.Status == "success" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	result, err := s.RunRule(ctx, "conn-1", "manual")
	if err != nil || result.Status != StatusHealthy {
		t.Fatalf("terminal result: %+v %v", result, err)
	}
	if store.claims != 1 {
		t.Fatal("completed result repeated billable request")
	}
	// A crash after registration but before a task ID is persisted must not POST.
	for key, task := range store.tasks {
		task.Status = "submitting"
		task.TaskID = ""
		store.tasks[key] = task
	}
	replacement := NewService(store, s.conns, s.states, s.upstreams, p, fakeAccounts{})
	result, err = replacement.RunRule(ctx, "conn-1", "manual")
	if err != nil || result.Status != StatusGenerating {
		t.Fatal(result, err)
	}
	if store.claims != 1 {
		t.Fatal("restart resubmitted an uncertain request")
	}
}

func TestMediaPollingTimeoutAndTransportAmbiguity(t *testing.T) {
	ctx, s, store, p := targetFixture()
	store.testModelConfig.GroupModels = []TestModelGroupConfig{{GroupID: "a", ModelID: "alias", Mode: "video", Adapter: "v6k_video_gateway", Seconds: 1}}
	s.conns.connections[0].OwnGroupIDs = []string{"a"}
	fp := configFingerprint(s.conns.connections[0], effectiveTestOptionsForGroup(store.mustRule("conn-1"), s.conns.connections[0], s.state(), *store.testModelConfig, "a").options())
	key := "conn-1|" + fp
	store.tasks[key] = MediaTask{Key: key, RuleID: "conn-1", ModelID: "alias", Mode: "video", Status: "pending", TaskID: "original", CreatedAt: time.Now().Add(-time.Minute), UpdatedAt: time.Now().Add(-20 * time.Second)}
	polled := make(chan struct{}, 1)
	p.probe = func(o AccountTestOptions) (AccountTestResult, error) {
		if o.TaskID != "original" {
			t.Error("poll lost task ID")
		}
		polled <- struct{}{}
		return AccountTestResult{}, errors.New("temporary network failure")
	}
	if _, err := s.RunRule(ctx, "conn-1", "manual"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-polled:
	case <-time.After(time.Second):
		t.Fatal("poll not queued")
	}
	for i := 0; i < 100; i++ {
		if _, active := s.mediaActive.Load(key); !active {
			break
		}
		time.Sleep(time.Millisecond)
	}
	task, _ := store.GetMediaTask(ctx, key)
	if task.TaskID != "original" || task.Status != "pending" || store.claims != 0 {
		t.Fatal(task)
	}
	task.CreatedAt = time.Now().Add(-61 * time.Minute)
	task.UpdatedAt = time.Now().Add(-20 * time.Second)
	store.mu.Lock()
	store.tasks[key] = *task
	store.mu.Unlock()
	result, err := s.RunRule(ctx, "conn-1", "manual")
	if err != nil || result.Status != StatusGenerating {
		t.Fatal(result, err)
	}
	task, _ = store.GetMediaTask(ctx, key)
	if task.Status != "timeout" || store.states["conn-1"]["a"].ConsecutiveFailures != 0 {
		t.Fatal(task)
	}
}

func TestPaginationAndGroupHealthFilters(t *testing.T) {
	_, s, _, _ := targetFixture()
	summary := SummaryResponse{Groups: []GroupSummary{{GroupID: "yes", Available: 1}, {GroupID: "no"}, {GroupID: "empty"}}}
	for i := 0; i < 500; i++ {
		group := "yes"
		if i%2 == 1 {
			group = "no"
		}
		summary.Channels = append(summary.Channels, ChannelStatus{RuleID: fmt.Sprint(i), OwnGroupIDs: []string{group}, SiteName: "test", Status: StatusHealthy})
	}
	response, err := s.channelPageFromSummary(context.Background(), summary, ChannelPageQuery{GroupStatus: "available", Page: 2, PageSize: 25})
	if err != nil || response.Total != 250 || len(response.Channels) != 25 || len(response.RuleIDs) != 250 {
		t.Fatal(response, err)
	}
	for _, row := range response.Channels {
		if row.OwnGroupIDs[0] != "yes" {
			t.Fatal("group status filter failed")
		}
	}
	response, err = s.channelPageFromSummary(context.Background(), summary, ChannelPageQuery{GroupID: "yes", GroupStatus: "unavailable"})
	if err != nil || response.Total != 0 {
		t.Fatal("filters contradicted", response)
	}
}

func TestUnknownGeneratingAndConfigErrorsNeverAvailable(t *testing.T) {
	for _, status := range []string{StatusUnknown, StatusGenerating, StatusConfigError} {
		if isAvailable(ChannelStatus{Status: status, Enabled: true, Supported: true, Schedulable: boolPtr(true)}) {
			t.Fatal(status)
		}
	}
}

func TestMediaTerminalFailureOnlyConsumedOnce(t *testing.T) {
	ctx, s, store, _ := targetFixture()
	store.testModelConfig.GroupModels = []TestModelGroupConfig{{GroupID: "a", ModelID: "gpt-image-2", Mode: "image"}}
	s.conns.connections[0].OwnGroupIDs = []string{"a"}
	rule := store.mustRule("conn-1")
	target := targetFor(rule, s.conns.connections[0], "a", s.state(), *store.testModelConfig, nil)
	key := rule.ID + "|" + target.Fingerprint
	store.tasks[key] = MediaTask{Key: key, RuleID: rule.ID, ModelID: "gpt-image-2", Status: "failed", Message: "invalid output", CreatedAt: time.Now().Add(-time.Second), UpdatedAt: time.Now()}
	for i := 0; i < 4; i++ {
		if _, err := s.RunRule(ctx, rule.ID, "manual"); err != nil {
			t.Fatal(err)
		}
	}
	if store.states[rule.ID]["a"].ConsecutiveFailures != 1 || len(store.results) != 1 {
		t.Fatalf("reused terminal counted repeatedly: %+v histories=%d", store.states, len(store.results))
	}
}

func TestTextErrorCoalescedAcrossGroups(t *testing.T) {
	ctx, s, store, p := targetFixture()
	store.testModelConfig.GroupModels[1].ModelID = "good"
	p.probe = func(AccountTestOptions) (AccountTestResult, error) {
		return AccountTestResult{}, errors.New("transport failed")
	}
	if _, err := s.RunRule(ctx, "conn-1", "manual"); err != nil {
		t.Fatal(err)
	}
	if len(p.testOptions) != 1 || len(store.results) != 2 {
		t.Fatalf("same failing request wasn't coalesced: %d", len(p.testOptions))
	}
}

func TestMediaQueueAdmissionNeverWaitsForGeneration(t *testing.T) {
	_, s, _, _ := targetFixture()
	block := make(chan struct{})
	defer close(block)
	start := time.Now()
	for i := 0; i < 500; i++ {
		s.queueMedia(fmt.Sprintf("queued-%d", i), func() { <-block })
	}
	if time.Since(start) > time.Second {
		t.Fatal("500 media admissions blocked request")
	}
}
