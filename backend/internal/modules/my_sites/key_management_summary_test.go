package my_sites

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"transithub/backend/internal/modules/upstream"
)

func TestSub2UsageSummaryReadsTrendWithoutDetailedLogs(t *testing.T) {
	today := time.Date(2026, 11, 23, 0, 0, 0, 0, usageZone)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/usage/dashboard/snapshot-v2" || q.Get("api_key_id") != "7" || q.Get("timezone") != "Asia/Shanghai" || q.Get("include_models") != "false" {
			t.Errorf("unexpected upstream request: %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"granularity": "day", "start_date": "2026-11-17", "end_date": "2026-11-23",
			"trend": []map[string]any{{"date": "2026-11-23", "requests": 3, "actual_cost": 0.25}, {"date": "2026-11-20", "requests": 9, "actual_cost": 1.5}},
		}})
	}))
	defer remote.Close()
	manager := &KeyManagement{platform: upstream.NewPlatformService(upstream.NewHTTPClient(remote.Client()))}
	site := &upstream.Site{Session: &upstream.Session{BaseURL: remote.URL, Platform: upstream.PlatformSub2API, AccessToken: "fixture"}}
	report := manager.sub2UsageSummary(context.Background(), site, "7", today)
	if report.State != "complete" || report.Unit != "USD" || report.Today.Amount == nil || *report.Today.Amount != 0.25 || report.Week.Amount == nil || *report.Week.Amount != 1.75 || report.Today.Requests != 3 || report.Week.Requests != 12 {
		t.Fatalf("unexpected usage summary: %+v", report)
	}
}

func TestNewAPIUsageSummaryUsesTokenIDAndMarksUnattributedLogs(t *testing.T) {
	today := time.Date(2026, 11, 23, 0, 0, 0, 0, usageZone)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/log/self" || r.URL.Query().Get("token_name") != "" {
			t.Errorf("unexpected upstream request: %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"total": 3, "items": []map[string]any{
			{"id": 1, "created_at": today.Unix() + 10, "token_id": 7, "token_name": "same", "quota": 10},
			{"id": 2, "created_at": today.Unix() - 10, "token_id": 8, "token_name": "same", "quota": 25},
			{"id": 3, "created_at": today.Unix() - 20, "token_name": "same", "quota": 50},
		}}})
	}))
	defer remote.Close()
	manager := &KeyManagement{platform: upstream.NewPlatformService(upstream.NewHTTPClient(remote.Client()))}
	site := &upstream.Site{Session: &upstream.Session{BaseURL: remote.URL, Platform: upstream.PlatformNewAPI, AccessToken: "fixture", UserID: "1"}}
	report := manager.newAPIUsageSummary(context.Background(), site, today)
	first, second := report.byID["7"], report.byID["8"]
	if report.state != "partial" || first.Today.Amount == nil || *first.Today.Amount != 10 || second.Today.Amount != nil || second.Week.Amount == nil || *second.Week.Amount != 25 {
		t.Fatalf("same-name or unverified records merged: %+v", report)
	}
}
