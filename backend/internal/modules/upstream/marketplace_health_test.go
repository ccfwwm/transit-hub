package upstream

import (
	"testing"
	"time"
)

func TestMarketplaceHealthIsAGroupReferenceNotEveryCatalogModel(t *testing.T) {
	payload := healthFixture(t, `{"data":[{"id":10,"name":"GPT Pro","models":[{"id":"gpt-a"},{"id":"gpt-b"}],"availability":{"availability_rate":0.8,"last_status":"success","last_checked_at":"2026-11-20T23:07:05+08:00","recent_results":[{"success":false,"outcome":"fault","checked_at":"2026-11-20T23:00:00+08:00"},{"success":true,"first_token_ms":16000,"checked_at":"2026-11-20T23:05:00+08:00"},{"success":true,"outcome":"unknown","checked_at":"2026-11-20T23:06:00+08:00"},{"success":true,"outcome":"success","first_token_ms":3000,"checked_at":"2026-11-20T23:07:05+08:00"}]}},{"id":11,"name":"No observations","models":[{"id":"gpt-a"}],"availability":{"recent_results":[]}}]}`)
	rows := parseMarketplaceHealth(payload, Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "10", Name: "GPT Pro"}}}}, time.Now())
	if len(rows) != 1 {
		t.Fatalf("catalog incorrectly expanded into model proof: %+v", rows)
	}
	h := rows[0]
	if h.Model != "GPT Pro" || h.GroupID != "10" || h.Scope != "channel_reference" || h.Usable || h.Status != "healthy" || len(h.Timeline) != 4 || h.Timeline[0].Status != "failed" || h.Timeline[1].Status != "warning" || h.Timeline[2].Status != "unknown" || *h.Availability != 80 || *h.LatencyMS != 3000 {
		t.Fatalf("lost group history: %+v", h)
	}
}

func TestMarketplaceHealthPreservesCurrentStatusWhenHistoryLags(t *testing.T) {
	now := time.Now().UTC()
	older := now.Add(-15 * time.Minute)
	payload := map[string]any{"data": []any{map[string]any{
		"id": "10",
		"availability": map[string]any{
			"last_status":     "degraded",
			"last_checked_at": now.Format(time.RFC3339Nano),
			"recent_results":  []any{map[string]any{"outcome": "success", "checked_at": older.Format(time.RFC3339Nano)}},
		},
	}}}
	rows := parseMarketplaceHealth(payload, Site{}, now)
	if len(rows) != 1 || rows[0].Status != "warning" || !rows[0].Fresh || len(rows[0].Timeline) != 1 || rows[0].Timeline[0].Status != "healthy" {
		t.Fatalf("older history overrode current status: %+v", rows)
	}
}
