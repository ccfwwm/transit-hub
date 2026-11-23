package channel_monitor

import (
	"testing"
	"time"

	"transithub/backend/internal/modules/upstream"
)

func TestCompactModelSummaryUsesLatestStatusAndKeepsHistoryForDisplay(t *testing.T) {
	old := time.Now().Add(-time.Minute)
	latest := time.Now()
	models := []upstream.ModelHealth{{Model: "image", Status: StatusFailed, Source: "upstream", CheckedAt: &old}, {Model: "image", Status: StatusHealthy, Source: "upstream", CheckedAt: &latest, Timeline: []upstream.HealthPoint{{Status: StatusFailed, CheckedAt: &old}, {Status: StatusHealthy, CheckedAt: &latest}}}, {Model: "other", Status: StatusFailed, Source: "upstream", CheckedAt: &latest}}
	got := selectedModelSummary(models, nil, "image", "")
	if got.Status != StatusHealthy || len(got.Timeline) != 2 || got.Timeline[0].Status != StatusFailed {
		t.Fatalf("history changed latest model status: %+v", got)
	}
	got = selectedModelSummary(models, map[string]ManualModelCheck{"image": {Model: "image", Status: StatusFailed, CheckedAt: &old}}, "image", "")
	if got.Status != StatusHealthy {
		t.Fatal("old manual check overrode latest upstream status")
	}
	newer := latest.Add(time.Minute)
	got = selectedModelSummary(models, map[string]ManualModelCheck{"image": {Model: "image", Status: StatusFailed, CheckedAt: &newer}}, "image", "")
	if got.Status != StatusFailed || len(got.Timeline) != 3 || len(models[1].Timeline) != 2 {
		t.Fatal("latest manual result missing or source history mutated")
	}
	got = selectedModelSummary(models, nil, "missing", "")
	if got.Status != StatusUnknown || len(got.Timeline) != 0 {
		t.Fatal("another model was used for missing primary model")
	}
}

func TestCompactModelSummaryUsesOnlyMatchingLineAsFallback(t *testing.T) {
	now := time.Now()
	older, later := now.Add(-time.Minute), now.Add(time.Second)
	line := upstream.ModelHealth{ID: "line", Model: "group", GroupID: "g", Scope: "channel_reference", Source: "sub2api_marketplace", Status: "warning", Fresh: true, CheckedAt: &now, Timeline: []upstream.HealthPoint{{Status: StatusHealthy, CheckedAt: &older}, {Status: "warning", CheckedAt: &now}}}
	olderLine := upstream.ModelHealth{ID: "older", GroupID: "g", Scope: "channel_reference", Source: "newapi_kuma", Status: StatusHealthy, Fresh: true, CheckedAt: &older}
	otherGroup := upstream.ModelHealth{ID: "other", Model: "group", GroupID: "other", Scope: "channel_reference", Status: StatusHealthy, Fresh: true, CheckedAt: &later}
	unmapped := upstream.ModelHealth{ID: "unmapped", Model: "group", GroupName: "group", Scope: "channel_reference", Status: StatusHealthy, Fresh: true, CheckedAt: &later}
	infrastructure := upstream.ModelHealth{Model: "selected", GroupID: "g", Scope: "infrastructure", Status: StatusHealthy, Fresh: true, CheckedAt: &later}
	model := upstream.ModelHealth{ID: "model", Model: "selected", GroupID: "g", Scope: "group_model", Source: "sub2api_v1", Status: StatusFailed, Fresh: true, CheckedAt: &older}
	results := []upstream.ModelHealth{otherGroup, unmapped, infrastructure, olderLine, line}

	got := selectedModelSummary(results, nil, "selected", "g")
	if got.ID != line.ID || got.Scope != line.Scope || got.Status != "warning" || got.Usable || len(got.Timeline) != 2 {
		t.Fatalf("missing latest matching line status: %+v", got)
	}
	got = selectedModelSummary(append(results, model), nil, "selected", "g")
	if got.ID != model.ID || got.Status != StatusFailed {
		t.Fatalf("line overrode selected model observation: %+v", got)
	}
	got = selectedModelSummary(results[:3], nil, "selected", "g")
	if got.Source != "catalog" || got.Status != StatusUnknown {
		t.Fatalf("unrelated observation used for display: %+v", got)
	}
	line.Fresh = false
	got = selectedModelSummary([]upstream.ModelHealth{line}, nil, "selected", "g")
	if got.Status != StatusUnknown || len(got.Timeline) != 2 || got.Scope != "channel_reference" {
		t.Fatalf("stale line displayed as current or lost its history: %+v", got)
	}
	if got := selectedStatusWithManual(results, nil, "group"); got != StatusUnknown {
		t.Fatalf("line contributed model status: %s", got)
	}
}
