package channel_monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

func TestKeyCatalogUsesForwardingAPIAndKeepsManagementRequestsSeparate(t *testing.T) {
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("unexpected credential or route")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"model"}]}`))
	}))
	defer api.Close()
	adapter := NewPlatformAdapter(upstream.NewPlatformService(upstream.NewHTTPClient(api.Client())))
	site := upstream.Site{BaseURL: "http://management.invalid"}
	conn := my_sites.RealConnection{UpstreamGroupID: "g", UpstreamAPIURL: api.URL, UpstreamKey: "fixture-key"}
	models, e := adapter.FetchKeyModels(context.Background(), site, conn)
	if e != nil || len(models) != 1 || calls != 1 {
		t.Fatal("model directory did not use real API", e)
	}
	site.Settings.GroupAPIURLs = map[string]string{"g": api.URL}
	conn.UpstreamAPIURL = "http://old-api.invalid"
	if _, e = adapter.FetchKeyModels(context.Background(), site, conn); e != nil || calls != 2 {
		t.Fatal("explicit group API setting ignored", e)
	}
}
