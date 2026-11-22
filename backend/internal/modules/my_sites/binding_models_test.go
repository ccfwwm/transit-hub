package my_sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"transithub/backend/internal/modules/upstream"
)

func TestBindingModelsUsesExactAccountKeyWithoutGeneration(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer exact-key" {
			t.Errorf("unexpected model directory request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"gpt-image-2.5"},{"id":"gpt-6.2"}]}`))
	}))
	defer server.Close()
	site := &upstream.Site{ID: "s", UserID: "u", AdminAccountID: "a", BaseURL: server.URL, Session: &upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: server.URL}}
	service := NewService(&realConnectStateRepo{}, upstream.NewPlatformService(upstream.NewHTTPClient(server.Client())), realConnectLookup{site: site})
	service.SetAdminAccountResolver(realConnectAccounts{id: "a"})
	service.connRepository = &realConnectConnRepo{getConn: &RealConnection{ID: "c", UpstreamSiteID: "s", UpstreamGroupID: "g", UpstreamKey: "exact-key"}}
	v, e := service.BindingModels(context.Background(), "u", "s", "g", "", "c")
	if e != nil || len(v.Items) != 2 || v.Source != "key" || calls != 1 {
		t.Fatalf("directory unavailable: %+v %v", v, e)
	}
	if _, e = service.BindingModels(context.Background(), "other", "s", "g", "", "c"); e == nil || calls != 1 {
		t.Fatal("cross-workspace directory request allowed")
	}
}
