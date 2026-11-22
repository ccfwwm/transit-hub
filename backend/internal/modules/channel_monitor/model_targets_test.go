package channel_monitor

import (
	"testing"
	"transithub/backend/internal/modules/upstream"
)

func TestActiveModeSelectsOnlyOneModelPerGroup(t *testing.T) {
	_, fixture, _, _ := targetFixture()
	conn := fixture.conns.connections[0]
	config := TestModelConfig{StatusSource: "active", GroupModels: []TestModelGroupConfig{{GroupID: "a", ModelID: "first"}, {GroupID: "a", ModelID: "second"}, {GroupID: "b", ModelID: "other"}}}
	got := selectedModelConfigs(Rule{}, conn, nil, config, "a")
	if len(got) != 1 || got[0].ModelID != "first" {
		t.Fatalf("active mode selected extra billable models: %+v", got)
	}
	config.StatusSource = "upstream"
	if got = selectedModelConfigs(Rule{}, conn, nil, config, "a"); len(got) != 1 {
		t.Fatal("passive scheduling must only follow the configured model")
	}
	if normalizeStatusSource("") != "upstream" {
		t.Fatal("default mode must be passive")
	}
}

func TestDisplaySourceDoesNotCountServersOrExpiredReports(t *testing.T) {
	if reportedStatus([]upstream.ModelHealth{{Source: "newapi_kuma", Scope: "channel_reference", Fresh: true, Status: StatusHealthy}}) != StatusUnknown {
		t.Fatal("channel uptime was promoted to model health")
	}
	if reportedStatus([]upstream.ModelHealth{{Status: StatusHealthy, Scope: "infrastructure", Fresh: true}, {Status: StatusHealthy, Scope: "group_model", Fresh: false}}) != StatusUnknown {
		t.Fatal("display promoted server or stale evidence")
	}
	if reportedStatus([]upstream.ModelHealth{{Status: StatusHealthy, Scope: "group_model", Fresh: true}, {Status: StatusFailed, Scope: "group_model", Fresh: true}}) != StatusHealthy {
		t.Fatal("partial report lost healthy model")
	}
}

func TestModelAggregationPreservesDispatchEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		models   []TargetState
		status   string
		failures int
	}{
		{"low balance overrides old healthy evidence", []TargetState{{Status: StatusHealthy}, {Status: StatusBalancePaused}}, StatusBalancePaused, 0},
		{"all low balance", []TargetState{{Status: StatusBalancePaused}, {Status: StatusBalancePaused}}, StatusBalancePaused, 0},
		{"partial model failure", []TargetState{{Status: StatusFailed, ConsecutiveFailures: 3}, {Status: StatusHealthy}}, StatusHealthy, 0},
		{"unknown is not failure", []TargetState{{Status: StatusFailed, ConsecutiveFailures: 3}, {Status: StatusUnknown}}, StatusUnknown, 0},
		{"all failures need each threshold", []TargetState{{Status: StatusFailed, ConsecutiveFailures: 3}, {Status: StatusFailed, ConsecutiveFailures: 1}}, StatusFailed, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := aggregateModels(Rule{ID: "rule"}, "group", tc.models)
			if got.Status != tc.status || got.ConsecutiveFailures != tc.failures {
				t.Fatalf("aggregate: status=%s failures=%d", got.Status, got.ConsecutiveFailures)
			}
		})
	}
}

func TestChangedModelSetInvalidatesAggregate(t *testing.T) {
	_, fixture, _, _ := targetFixture()
	conn := fixture.conns.connections[0]
	rule := Rule{ID: "rule"}
	config := TestModelConfig{GroupModels: []TestModelGroupConfig{{GroupID: "a", ModelID: "new-model", Mode: "text"}, {GroupID: "a", ModelID: "good", Mode: "text"}}}
	fp := configFingerprint(conn, AccountTestOptions{ModelID: "good", Mode: "text"})
	stored := map[string]TargetState{"a": {Fingerprint: fp, Status: StatusFailed, ConsecutiveFailures: 2, Models: []TargetState{{Fingerprint: fp, Model: "good", Status: StatusFailed}}}}
	got := targetFor(rule, conn, "a", nil, config, stored)
	if got.Status != StatusUnknown || got.ConsecutiveFailures != 0 {
		t.Fatalf("changed model set retained stale scheduling evidence: %+v", got)
	}
}
