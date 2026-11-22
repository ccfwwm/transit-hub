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
	got := selectedModelSummary(models, nil, "image")
	if got.Status != StatusHealthy || len(got.Timeline) != 2 || got.Timeline[0].Status != StatusFailed {
		t.Fatalf("history changed latest model status: %+v", got)
	}
	got = selectedModelSummary(models, map[string]ManualModelCheck{"image": {Model: "image", Status: StatusFailed, CheckedAt: &old}}, "image")
	if got.Status != StatusHealthy {
		t.Fatal("old manual check overrode latest upstream status")
	}
	newer := latest.Add(time.Minute)
	got = selectedModelSummary(models, map[string]ManualModelCheck{"image": {Model: "image", Status: StatusFailed, CheckedAt: &newer}}, "image")
	if got.Status != StatusFailed || len(got.Timeline) != 3 || len(models[1].Timeline) != 2 {
		t.Fatal("latest manual result missing or source history mutated")
	}
	got = selectedModelSummary(models, nil, "missing")
	if got.Status != StatusUnknown || len(got.Timeline) != 0 {
		t.Fatal("another model was used for missing primary model")
	}
}
