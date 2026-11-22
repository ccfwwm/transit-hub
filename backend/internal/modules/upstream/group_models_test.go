package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGroupDirectoryReadsMarketplaceObjectsWithoutCreatingKey(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Errorf("directory mutated credentials: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/groups/available" {
			w.Write([]byte(`{"data":[{"id":144,"name":"国模(华为渠道)"}]}`))
			return
		}
		if r.URL.Path != "/api/v1/marketplace/models" {
			t.Error(r.URL.Path)
		}
		w.Write([]byte(`{"data":[{"id":144,"name":"国模(华为渠道)","models":[{"id":"glm-5.3","display_name":"GLM"},{"id":"deepseek-v4.1-flash"},"deepseek-v4.1-flash",{"name":"Display label only"}]},{"id":10,"models":[{"id":"wrong-group"}]}]}`))
	}))
	defer server.Close()
	s := NewPlatformService(NewHTTPClient(server.Client()))
	site := Site{BaseURL: server.URL, Session: &Session{BaseURL: server.URL, Platform: PlatformSub2API}}
	ids, e := s.FetchGroupModelIDs(context.Background(), site, "144")
	if e != nil || len(ids) != 2 || ids[0] != "deepseek-v4.1-flash" || ids[1] != "glm-5.3" || calls != 2 {
		t.Fatalf("wrong group models: %v %v calls=%d", ids, e, calls)
	}
}
func TestGroupDirectoryUsesStandardObjectListFirst(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/groups/available" {
			t.Error("unnecessary fallback request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":144,"models":[{"model":"exact-model"}]}]}`))
	}))
	defer server.Close()
	s := NewPlatformService(NewHTTPClient(server.Client()))
	ids, e := s.FetchGroupModelIDs(context.Background(), Site{BaseURL: server.URL, Session: &Session{Platform: PlatformSub2API}}, "144")
	if e != nil || len(ids) != 1 || ids[0] != "exact-model" {
		t.Fatalf("object directory not parsed: %v %v", ids, e)
	}
}
