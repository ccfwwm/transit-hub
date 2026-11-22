package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestHealthFreshnessAndScope(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-time.Minute)
	old := now.Add(-31 * time.Minute)
	for _, tc := range []struct {
		name   string
		h      ModelHealth
		usable bool
	}{
		{"exact", ModelHealth{Scope: "group_model", GroupID: "3", Status: "healthy", CheckedAt: &fresh}, true},
		{"stale", ModelHealth{Scope: "group_model", GroupID: "3", Status: "healthy", CheckedAt: &old, IntervalSeconds: 3600}, false},
		{"no time", ModelHealth{Scope: "group_model", GroupID: "3", Status: "healthy"}, false},
		{"server online", ModelHealth{Scope: "infrastructure", GroupID: "3", Status: "healthy", CheckedAt: &fresh}, false},
		{"aggregate", ModelHealth{Scope: "site_model", Status: "healthy", CheckedAt: &fresh}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if qualifyHealth(tc.h).Usable != tc.usable {
				t.Fatal("incorrect availability evidence")
			}
		})
	}
}
func TestV1LatestObservationAndExtraModelTimestamp(t *testing.T) {
	now := time.Now().UTC()
	payload := map[string]any{"data": map[string]any{"items": []any{map[string]any{"id": float64(5), "group_name": "pro", "primary_model": "model", "primary_status": "operational", "timeline": []any{map[string]any{"status": "operational", "checked_at": now.Add(-time.Hour).Format(time.RFC3339)}, map[string]any{"status": "degraded", "checked_at": now.Add(-time.Minute).Format(time.RFC3339)}}, "extra_models": []any{map[string]any{"model": "other", "status": "operational"}}}}}}
	rows := parseV1Health(payload, Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "3", Name: "pro"}}}}, now)
	if len(rows) != 2 || rows[0].Status != "warning" || rows[0].GroupID != "3" || !rows[0].Fresh || rows[0].Usable {
		t.Fatalf("incorrect primary: %+v", rows)
	}
	if rows[1].CheckedAt != nil || rows[1].Usable {
		t.Fatal("extra model inherited primary timestamp")
	}
}

func TestReportedHistoryAndHiddenV2Samples(t *testing.T) {
	now := time.Now().UTC()
	points := []any{}
	for i := 0; i < 4; i++ {
		status := "operational"
		if i == 2 {
			status = "error"
		}
		points = append(points, map[string]any{"status": status, "latency_ms": float64(1200), "checked_at": now.Add(-time.Duration(i*15) * time.Minute).Format(time.RFC3339)})
	}
	payload := map[string]any{"data": map[string]any{"items": []any{map[string]any{"id": float64(7), "group_name": "pro", "primary_model": "model", "availability_7d": 98.28, "timeline": points}}}}
	rows := parseV1Health(payload, Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "3", Name: "pro"}}}}, now)
	if len(rows) != 1 || len(rows[0].Timeline) != 4 || rows[0].Timeline[1].Status != "failed" || rows[0].IntervalSeconds != 900 || rows[0].Availability == nil || rows[0].Throughput != nil {
		t.Fatalf("lost upstream history or invented throughput: %+v", rows)
	}
	metrics := map[string]any{"request_count": float64(0), "has_requests": true, "success_rate": 0.9828, "ttft": map[string]any{"avg_ms": 29180.0}}
	bucket := map[string]any{"bucket_start": now.Add(-time.Minute).Format(time.RFC3339), "metrics": metrics, "health": map[string]any{"overall": "healthy"}}
	payload = map[string]any{"data": map[string]any{"items": []any{map[string]any{"group_id": float64(3), "model": "reported", "metrics": metrics, "health": map[string]any{"overall": "healthy", "minimum_sample": float64(50)}, "buckets": []any{bucket}}}}}
	rows = parseV2Health(payload, Site{}, now)
	if len(rows) != 1 || rows[0].Status != "healthy" || rows[0].Usable || len(rows[0].Timeline) != 1 || rows[0].LatencyMS == nil || *rows[0].LatencyMS != 29180 || rows[0].Availability == nil {
		t.Fatalf("hidden sample must retain reported status as reference: %+v", rows)
	}
}

func TestFamilyMonitorReferenceDoesNotBecomeDispatchEvidence(t *testing.T) {
	platform, rate := "anthropic", 0.8
	site := Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "31", Name: "claude full", Platform: &platform, Multiplier: &rate}}}}
	now := time.Now().UTC()
	item := map[string]any{"id": float64(13), "name": "Full CC", "group_name": "claude family", "provider": "anthropic", "group_rate_multipliers": []any{0.8}, "primary_model": "claude-haiku", "primary_status": "operational", "primary_checked_at": now.Format(time.RFC3339), "status_source": "request_logs", "metrics_scope": "group", "availability": 100.0, "availability_7d": 0.0}
	payload := map[string]any{"data": map[string]any{"items": []any{item}}}
	rows := parseV1Health(payload, site, now)
	if len(rows) != 1 || rows[0].GroupID != "31" || rows[0].Usable || !rows[0].Fresh || rows[0].Availability == nil || *rows[0].Availability != 100 {
		t.Fatalf("family report not safely displayed: %+v", rows)
	}
	site.Metrics.Groups = append(site.Metrics.Groups, GroupInfo{ID: "32", Name: "another", Platform: &platform, Multiplier: &rate})
	rows = parseV1Health(payload, site, now)
	if rows[0].GroupID != "" {
		t.Fatal("ambiguous rate must not select group")
	}
}
func TestKeyPolicyPreservesNewAPIFieldsAndValidatesAddresses(t *testing.T) {
	if _, e := NormalizeAllowedIPs([]string{"0.0.0.0; rm -rf"}); e == nil {
		t.Fatal("invalid address accepted")
	}
	values, e := NormalizeAllowedIPs([]string{"206.82.2.46", "2001:db8::1", "192.0.2.9/24", "206.82.2.46"})
	if e != nil || len(values) != 3 {
		t.Fatalf("bad normalization: %v %v", values, e)
	}
	merged := mergeManagedIPs([]string{"198.51.100.5", "192.0.2.1"}, []string{"192.0.2.1"}, []string{DefaultMonitorIP}, false)
	if !reflect.DeepEqual(merged, []string{"198.51.100.5", DefaultMonitorIP}) {
		t.Fatal(merged)
	}
	var updated map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": 1, "name": "keep", "remain_quota": 123, "unlimited_quota": false, "expired_time": 999, "group": "pro", "model_limits_enabled": true, "model_limits": "a,b", "cross_group_retry": true, "allow_user_quota_query": false, "allow_ips": "198.51.100.5", "key": "must-not-write"}})
			return
		}
		if e := json.NewDecoder(r.Body).Decode(&updated); e != nil {
			t.Error(e)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()
	p := NewPlatformService(NewHTTPClient(server.Client()))
	session := Session{Platform: PlatformNewAPI, BaseURL: server.URL, AccessToken: "token", UserID: "1"}
	record, _, e := p.readKeyPolicy(context.Background(), session, "1")
	if e != nil {
		t.Fatal(e)
	}
	if e = p.writeKeyPolicy(context.Background(), session, "1", record, []string{DefaultMonitorIP}); e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{"remain_quota", "expired_time", "unlimited_quota", "group", "model_limits_enabled", "model_limits", "cross_group_retry", "allow_user_quota_query"} {
		if !reflect.DeepEqual(record[k], updated[k]) {
			t.Fatalf("changed %s", k)
		}
	}
	if _, ok := updated["key"]; ok {
		t.Fatal("secret sent back in policy update")
	}
}

func TestKumaPreservesRealHistoryAndGroupReferences(t *testing.T) {
	now := time.Now().UTC()
	payload := map[string]any{"data": []any{map[string]any{"categoryName": "health", "monitors": []any{
		map[string]any{"name": "newapi-Claude Code专用 - Max号池", "status": float64(1), "uptime": 0.98645, "heartbeats": []any{map[string]any{"status": float64(0), "time": "2026-11-20 11:59:48.948", "ping": float64(1300)}, map[string]any{"status": float64(1), "time": "2026-11-20 12:00:48.948", "ping": float64(1200)}}},
		map[string]any{"name": "Azure GPT / gpt-6-luna", "group": "Azure GPT", "status": float64(1)},
		map[string]any{"name": "PostgreSQL", "status": float64(1)},
	}}}}
	rows := associateKumaGroups(parseKumaHealth(payload, now), Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "1", Name: "Claude Code专用-Max号池"}, {ID: "2", Name: "Azure GPT"}}}})
	if len(rows) != 3 || rows[0].GroupID != "1" || rows[0].Scope != "channel_reference" || len(rows[0].Timeline) != 2 || rows[0].Timeline[0].Status != "failed" || rows[0].LatencyMS == nil || *rows[0].LatencyMS != 1200 || rows[0].Availability == nil || *rows[0].Availability < 98 {
		t.Fatalf("lost Kuma data: %+v", rows)
	}
	if rows[0].CheckedAt != nil || rows[0].Usable || rows[0].Fresh {
		t.Fatal("timezone-less history fabricated fresh model evidence")
	}
	if rows[1].Model != "gpt-6-luna" || rows[1].Scope != "model_reference" || rows[1].GroupID != "2" || rows[1].Usable {
		t.Fatal("explicit model reference lost or promoted")
	}
	if rows[2].GroupID != "" || rows[2].Scope != "infrastructure" || rows[2].Usable {
		t.Fatal("server online became model evidence")
	}
	if len(parseKumaHealth(map[string]any{"data": []any{}}, now)) != 0 {
		t.Fatal("empty upstream invented health")
	}
}
func TestV1DetailModelsNeverInheritPrimaryHistory(t *testing.T) {
	now := time.Now().UTC()
	lat := 1300
	base := []ModelHealth{{ID: "sub2api_v1:7", Model: "primary", GroupID: "3", Source: "sub2api_v1", Scope: "group_model", Status: "healthy", CheckedAt: &now, LatencyMS: &lat, Timeline: []HealthPoint{{Status: "healthy", CheckedAt: &now}}}}
	payload := map[string]any{"data": map[string]any{"models": []any{map[string]any{"model": "primary", "latest_status": "operational"}, map[string]any{"model": "additional", "latest_status": "error", "latest_latency_ms": float64(450), "availability_7d": 95.0}}}}
	rows := mergeV1Details(base, payload, "7", now)
	if len(rows) != 2 || rows[1].Model != "additional" || rows[1].Status != "failed" || rows[1].GroupID != "3" || rows[1].LatencyMS == nil || *rows[1].LatencyMS != 450 {
		t.Fatalf("detail missing: %+v", rows)
	}
	if rows[1].CheckedAt != nil || len(rows[1].Timeline) > 0 || rows[1].Usable {
		t.Fatal("additional model inherited primary proof")
	}
	if len(mergeV1Details(rows, payload, "7", now)) != 2 {
		t.Fatal("detail refresh duplicated model")
	}
}
