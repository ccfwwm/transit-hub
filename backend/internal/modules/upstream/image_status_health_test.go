package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestImageReportImportsAllGroupsAndExplicitModels(t *testing.T) {
	raw, e := os.ReadFile("testdata/apikl-image-status.json")
	if e != nil {
		t.Fatal(e)
	}
	var payload any
	if e = json.Unmarshal(raw, &payload); e != nil {
		t.Fatal(e)
	}
	rows := parseImageStatusHealth(payload, Site{}, time.Now())
	groups := map[string]bool{}
	fourK := map[string]bool{}
	for _, r := range rows {
		groups[r.GroupID] = true
		if r.Scope != "group_model_reference" || r.Usable || r.Mode != "image" || len(r.Timeline) != 60 {
			t.Fatalf("group report promoted or history lost: %+v", r)
		}
		if r.GroupID == "92" {
			fourK[r.Model] = true
			if r.Status != "healthy" || qualifyHealth(r).Status != "healthy" {
				t.Fatalf("4K current status lost: %+v", r)
			}
		}
	}
	if len(groups) != 9 || len(rows) != 25 || len(fourK) != 5 || !fourK["gpt-image-2"] || !fourK["gpt-image-2.6-sunburst"] {
		t.Fatalf("missing source groups/models: groups=%d rows=%d 4K=%v", len(groups), len(rows), fourK)
	}
}
func TestImageCurrentReportIsIndependentOfHistoricalBars(t *testing.T) {
	cases := []struct{ name, recent, last20, tail, want string }{
		{"latest green old red", "[20,0]", "[20,20]", "0", "healthy"},
		{"latest red old green", "[20,20]", "[20,0]", "0", "failed"},
		{"warning", "[20,4]", "[20,0]", "0", "warning"},
		{"successful tail", "[1,1]", "[20,20]", "2", "healthy"},
		{"no recent uses last20", "[1,0]", "[20,20]", "0", "failed"},
		{"empty", "[0,0]", "[0,0]", "0", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := healthFixture(t, `{"groups":[{"id":92,"name":"4K","model":"gpt-image-2","recent":`+tc.recent+`,"last20":`+tc.last20+`,"tail_ok":`+tc.tail+`,"last_at":"2026-11-21T00:00:00Z","slots":[["2026-11-21T12:00:00Z","bad",100,9000,0,null]]}]}`)
			rows := parseImageStatusHealth(p, Site{}, time.Now())
			if len(rows) != 1 || rows[0].Status != tc.want || qualifyHealth(rows[0]).Status != tc.want {
				t.Fatalf("history changed current report: %+v", rows)
			}
		})
	}
}
func TestImageReportTemplateUsesReadOnlyCustomPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/custom-image.json" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"generated_at":"2026-11-21T12:00:00Z","groups":[{"id":92,"name":"4K","model":"gpt-image-2 / gpt-image-2.6-flare","recent":[20,0]}]}`))
	}))
	defer server.Close()
	h := &HealthService{platform: NewPlatformService(NewHTTPClient(server.Client()))}
	site := Site{BaseURL: server.URL, Platform: PlatformSub2API, Session: &Session{Platform: PlatformSub2API}, Settings: SiteSettings{MonitorProfile: &MonitorProfile{Template: "sub2api_image_status", Path: "/custom-image.json"}}}
	rows, e := h.fetch(context.Background(), site, "sub2api_image_status")
	if e != nil || len(rows) != 2 || rows[0].Status != "healthy" || rows[0].GroupID != "92" {
		t.Fatalf("custom report unsupported: %+v %v", rows, e)
	}
}
