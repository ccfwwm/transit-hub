package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewAPIRecentCoverageImportsModelsOutsideDefaultSummary(t *testing.T) {
	site := Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "image", Name: "图片（渠道1）"}}}}
	pricing := healthFixture(t, `{"data":[{"model_name":"gpt-image-2.5","enable_groups":["图片（渠道1）"],"supported_endpoint_types":["image-generation"]},{"model_name":"gemini-nano-banana-2.1","enable_groups":["图片（渠道1）"],"supported_endpoint_types":["image-generation"]}]}`)
	summary := healthFixture(t, `{"data":{"models":[{"model_name":"gpt-image-2.5","recent_success_series":[{"ts":1795230000,"success_rate":100}]}]}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || len(r.URL.Query()["recent_model"]) != 2 {
			t.Error("not fetching complete pricing model coverage")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"recent_success_series":{"gemini-nano-banana-2.1":[{"ts":1795226400,"success_rate":100},{"ts":1795230000,"success_rate":0}]}}}`))
	}))
	defer server.Close()
	site.BaseURL = server.URL
	s := &HealthService{platform: NewPlatformService(NewHTTPClient(server.Client()))}
	merged := s.fetchNewAPIRecentModels(context.Background(), site, requestOptions{}, "/api/perf-metrics/summary?hours=24", summary, pricing)
	rows := parseNewAPIPerformance(merged, pricing, site, time.Now())
	if len(rows) != 2 {
		t.Fatal("model missing outside summary top list", rows)
	}
	for _, row := range rows {
		if row.GroupID != "image" || row.Mode != "image" {
			t.Fatal("pricing group mapping lost")
		}
		if strings.HasPrefix(row.Model, "gemini") && (row.Status != "failed" || len(row.Timeline) != 2) {
			t.Fatal("last returned status ignored", row)
		}
	}
}

func healthFixture(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestNewAPIPerformanceKeepsExactGroupStatistics(t *testing.T) {
	site := Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "a", Name: "image2 1k"}, {ID: "b", Name: "image2 2k 4k"}}}}
	pricing := healthFixture(t, `{"data":[{"model_name":"gpt-image-2","enable_groups":["image2 1k","image2 2k 4k"],"supported_endpoint_types":["image-generation"]}]}`)
	summary := healthFixture(t, `{"data":{"models":[{"model_name":"gpt-image-2","success_rate":99,"avg_latency_ms":35000,"avg_tps":40,"recent_success_series":[{"ts":1795183200,"success_rate":100}]}]}}`)
	base := parseNewAPIPerformance(summary, pricing, site, time.Now())
	if len(base) != 2 || base[0].Usable || base[0].Scope != "model_reference" || base[0].Mode != "image" {
		t.Fatalf("aggregate wrongly scoped: %+v", base)
	}
	detail := healthFixture(t, `{"data":{"model_name":"gpt-image-2","groups":[{"group":"image2 1k","success_rate":98.4,"avg_latency_ms":37000,"avg_tps":43,"series":[{"ts":1795183200,"success_rate":99,"avg_latency_ms":45000,"avg_tps":33}]},{"group":"image2 2k 4k","success_rate":10.5,"avg_latency_ms":11000,"avg_tps":127,"series":[{"ts":1795183200,"success_rate":0}]}]}}`)
	rows := mergeNewAPIPerformanceDetails(base, detail, site, pricing)
	if len(rows) != 2 {
		t.Fatalf("duplicate aggregate/detail: %+v", rows)
	}
	by := map[string]ModelHealth{}
	for _, h := range rows {
		by[h.GroupID] = h
		if h.Usable || h.Scope != "group_model_reference" {
			t.Fatal("statistics became Key detection proof")
		}
	}
	if by["a"].Status != "healthy" || by["b"].Status != "failed" || *by["b"].Availability != 10.5 || by["b"].Timeline[0].Status != "failed" || *by["a"].Timeline[0].LatencyMS != 45000 {
		t.Fatal("different group statistics mixed")
	}
	if *by["a"].Availability == *base[0].Availability {
		t.Fatal("detail ignored")
	}
	if len(mergeNewAPIPerformanceDetails(rows, detail, site, pricing)) != 2 {
		t.Fatal("detail refresh duplicates")
	}
}
func TestNewAPIPerformanceDoesNotInventTimeOrHistory(t *testing.T) {
	rows := parseNewAPIPerformance(healthFixture(t, `{"data":{"models":[{"model_name":"m","success_rate":100,"avg_latency_ms":10}]}}`), nil, Site{}, time.Now())
	if len(rows) != 1 || rows[0].CheckedAt != nil || len(rows[0].Timeline) != 0 || rows[0].Fresh || rows[0].Usable {
		t.Fatal("collection time became a test observation")
	}
	for _, tc := range []struct {
		rate   float64
		status string
	}{{100, "healthy"}, {90, "healthy"}, {89.9, "warning"}, {70, "warning"}, {69.9, "failed"}, {0, "failed"}, {-1, "unknown"}, {101, "unknown"}} {
		if perfStatus(&tc.rate) != tc.status {
			t.Fatalf("incorrect upstream color band: %+v", tc)
		}
	}
}
func TestNewAPIChannelRoutesDoNotInheritFamilyStatus(t *testing.T) {
	site := Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "img", Name: "IMAGE"}, {ID: "text", Name: "LLM-GPT-pro"}}}}
	pricing := healthFixture(t, `{"data":[{"model_name":"gpt-image-2","enable_groups":["IMAGE"],"supported_endpoint_types":["openai-image"]},{"model_name":"unmonitored-image","enable_groups":["IMAGE"]},{"model_name":"gpt-6.1-sol","enable_groups":["LLM-GPT-pro"]}]}`)
	payload := healthFixture(t, `{"data":{"summary":{"enabled":true,"ready":true},"items":[{"name":"gpt-image-family","category":"image","latest_status":"degraded","routes":[{"name":"gpt-image-2","latest_status":"available","timeline":{"started_at":1795182600,"ended_at":1795183800,"bucket_seconds":600,"statuses":["unavailable","available","available"]},"pace":{"kind":"generation","ms":43000}},{"name":"gpt-image-2-x","latest_status":"degraded","timeline":{"started_at":1795182600,"ended_at":1795183800,"bucket_seconds":600,"statuses":["degraded","degraded","degraded"]}}]},{"name":"GPT","category":"text","latest_status":"available","routes":[{"name":"LLM-GPT-pro","latest_status":"available","timeline":{"started_at":1795182600,"ended_at":1795183800,"bucket_seconds":600,"statuses":["available","available","available"]},"pace":{"kind":"first_token","ms":5000}}]}]}}`)
	rows := parseNewAPIChannelMonitors(payload, pricing, site, time.Now())
	if len(rows) != 3 {
		t.Fatalf("lost routes: %+v", rows)
	}
	if rows[0].Model != "gpt-image-2" || rows[0].Status != "healthy" || rows[0].GroupID != "img" || rows[0].Usable || len(rows[0].Timeline) != 3 || rows[0].Timeline[0].Status != "failed" || *rows[0].LatencyMS != 43000 {
		t.Fatalf("model route failed: %+v", rows[0])
	}
	if rows[1].Status != "warning" || len(rows[1].Timeline) != 3 {
		t.Fatal("parent status copied to routes")
	}
	if rows[2].Model != "LLM-GPT-pro" || rows[2].Scope != "channel_reference" || rows[2].GroupID != "text" || rows[2].Usable {
		t.Fatal("text channel expanded into fake model reports")
	}
	dataRecord(payload)["summary"].(map[string]any)["ready"] = false
	if len(parseNewAPIChannelMonitors(payload, pricing, site, time.Now())) != 0 {
		t.Fatal("unready monitoring accepted")
	}
}
func TestNewAPIHealthFetchUsesReadOnlyEncodedProtocol(t *testing.T) {
	var details atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("non-read-only request")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/perf-metrics/summary":
			w.Write([]byte(`{"success":true,"data":{"models":[{"model_name":"m/a+b","success_rate":100}]}}`))
		case "/api/pricing":
			w.Write([]byte(`{"success":true,"data":[{"model_name":"m/a+b","enable_groups":["pro"]}]}`))
		case "/api/perf-metrics":
			details.Add(1)
			if r.URL.Query().Get("model") != "m/a+b" {
				t.Error("model query not encoded")
			}
			w.Write([]byte(`{"success":true,"data":{"model_name":"m/a+b","groups":[{"group":"pro","success_rate":75,"avg_latency_ms":20,"series":[{"ts":1795183200,"success_rate":75}]}]}}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := &HealthService{platform: NewPlatformService(NewHTTPClient(server.Client()))}
	site := Site{BaseURL: server.URL, Metrics: Metrics{Groups: []GroupInfo{{ID: "p", Name: "pro"}}}}
	rows, e := s.fetchNewAPIModels(context.Background(), site, requestOptions{Context: context.Background()}, "newapi_perf")
	if e != nil || len(rows) != 1 || details.Load() != 1 || rows[0].Status != "warning" || rows[0].GroupID != "p" || rows[0].Scope != "group_model_reference" {
		t.Fatalf("wrong fetch: %+v %v", rows, e)
	}
}

// qiuqiutoken/foxrouter forks include the requested models in data.models;
// other versions only return their series in data.recent_success_series.
func TestRecentModelsMergesBothNewAPIReturnFormats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/perf-metrics/summary" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if len(r.URL.Query()["recent_model"]) != 2 {
			t.Error("missing requested models")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"models":[{"model_name":"image/2+edit","recent_success_series":[{"ts":1795183200,"success_rate":0},{"ts":1795186800,"success_rate":100}]}],"recent_success_series":{"audio":[{"ts":1795186800,"success_rate":0}]}}}`))
	}))
	defer server.Close()
	pricing := healthFixture(t, `{"data":[{"model_name":"image/2+edit","enable_groups":["pro"]},{"model_name":"audio","enable_groups":["pro"]}]}`)
	original := healthFixture(t, `{"data":{"models":[{"model_name":"top","recent_success_series":[{"ts":1795186800,"success_rate":100}]}]}}`)
	s := &HealthService{platform: NewPlatformService(NewHTTPClient(server.Client()))}
	site := Site{BaseURL: server.URL, Metrics: Metrics{Groups: []GroupInfo{{ID: "g", Name: "pro"}}}}
	merged := s.fetchNewAPIRecentModels(context.Background(), site, requestOptions{}, "/api/perf-metrics/summary?hours=24", original, pricing)
	rows := parseNewAPIPerformance(merged, pricing, site, time.Now())
	found := map[string]ModelHealth{}
	for _, row := range rows {
		found[row.Model] = row
	}
	if len(found) != 3 || found["image/2+edit"].Status != "healthy" || len(found["image/2+edit"].Timeline) != 2 || found["audio"].Status != "failed" || found["image/2+edit"].GroupID != "g" {
		t.Fatalf("lost requested model states: %+v", rows)
	}
}
