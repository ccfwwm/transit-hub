package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMonitorProfilesCustomPathsAndIsolation(t *testing.T) {
	for _, path := range []string{"https://other.example/status", "//other.example/status", "/%2fother.example", "/api/\\status", "/api/%0astatus", "/api/status#x"} {
		if _, e := normalizeMonitorProfile(&MonitorProfile{Template: "sub2api_plaza", Path: path}); e == nil {
			t.Fatalf("invalid path accepted: %s", path)
		}
	}
	a := &MonitorProfile{Template: "sub2api_plaza", Path: "/custom/status"}
	b := &MonitorProfile{Template: "sub2api_plaza", Path: "/other/status"}
	if healthProfileHash(a) == healthProfileHash(b) || healthProfileHash(nil) != "" {
		t.Fatal("profile changes share health cache")
	}
	if got := expandHealthPath("/status/{model}?model={model}", "{model}", "a/b+c d"); got != "/status/a%2Fb+c%20d?model=a%2Fb%2Bc+d" {
		t.Fatal(got)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/custom/status" {
			t.Errorf("unexpected monitor request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"groups":[{"id":4,"name":"pro","models":[{"name":"m","availability":{"status":"healthy","source":"real"},"performance":{"trend":[{"status":"healthy","bucket_start":"2026-11-20T12:00:00Z"}]}}]}]}}`))
	}))
	defer server.Close()
	service := &HealthService{platform: NewPlatformService(NewHTTPClient(server.Client()))}
	rows, e := service.fetch(context.Background(), Site{BaseURL: server.URL, Platform: PlatformSub2API, Session: &Session{}, Settings: SiteSettings{MonitorProfile: a}, Metrics: Metrics{Groups: []GroupInfo{{ID: "4", Name: "different-name"}}}}, "sub2api_plaza")
	if e != nil || len(rows) != 1 || rows[0].GroupID != "4" || rows[0].Model != "m" {
		t.Fatalf("custom path failed: %+v %v", rows, e)
	}
}
func TestPlazaKeepsPerModelHistoryAndRejectsPaddedPoints(t *testing.T) {
	payload := healthFixture(t, `{"data":{"groups":[{"id":4,"name":"pro","availability":{"status":"unavailable","source":"real"},"models":[{"name":"image","availability":{"status":"healthy","source":"real"},"performance":{"trend":[{"bucket_start":"2026-11-20T12:00:00Z","status":"unavailable"},{"bucket_start":"2026-11-20T12:02:00Z","status":"healthy"},{"bucket_start":"2026-11-20T12:03:00Z","status":"healthy","padded":true}]}},{"name":"configured-only","availability":{"status":"healthy","source":"default"},"performance":{"trend":[]}}]}]}}`)
	rows := parseModelPlazaHealth(payload, Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "4", Name: "pro"}}}}, time.Now())
	if len(rows) != 2 || rows[0].Scope != "channel_reference" || rows[1].Model != "image" || rows[1].Status != "healthy" || len(rows[1].Timeline) != 2 || rows[1].Timeline[0].Status != "failed" || rows[1].Usable {
		t.Fatalf("plaza observations mixed or invented: %+v", rows)
	}
}
func TestNewAPILastPointOverridesWindowSuccessRate(t *testing.T) {
	payload := healthFixture(t, `{"data":{"models":[{"model_name":"image","success_rate":10.5,"recent_success_series":[{"ts":1795186800,"success_rate":0},{"ts":1795190400,"success_rate":100}]}]}}`)
	rows := parseNewAPIPerformance(payload, nil, Site{}, time.Now())
	if len(rows) != 1 || rows[0].Status != "healthy" || rows[0].Availability == nil || *rows[0].Availability != 10.5 || len(rows[0].Timeline) != 2 || rows[0].Timeline[0].Status != "failed" {
		t.Fatalf("historical rate overrode last green point: %+v", rows)
	}
}
