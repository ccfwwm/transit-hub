package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonitorBindingMatchesCompleteKeyAcrossPages(t *testing.T) {
	for _, platform := range []Platform{PlatformSub2API, PlatformNewAPI} {
		t.Run(string(platform), func(t *testing.T) {
			reveals := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/key") {
					if platform != PlatformNewAPI || r.Method != "POST" || !strings.Contains(r.URL.Path, "/2/") {
						t.Fatal("unexpected key reveal")
					}
					reveals++
					w.Write([]byte(`{"data":{"key":"target-secret"}}`))
					return
				}
				if r.Method != "GET" {
					t.Fatal("matching mutated an upstream")
				}
				page := r.URL.Query().Get("page")
				if platform == PlatformNewAPI {
					page = r.URL.Query().Get("p")
				}
				raw := `{"id":1,"key":"unrelated","group_id":5,"group":"other"}`
				if page == "2" {
					raw = `{"id":2,"key":"sk-target-secret","group_id":"7","group":"pro"}`
					if platform == PlatformNewAPI {
						raw = `{"id":2,"key":"tar***ret","group":"pro"}`
					}
				}
				w.Write([]byte(`{"data":{"items":[` + raw + `],"total":2,"page_size":1}}`))
			}))
			defer server.Close()
			service := NewPlatformService(NewHTTPClient(server.Client()))
			site := Site{Session: &Session{Platform: platform, BaseURL: server.URL, AccessToken: "login"}}
			found, e := service.FindAPIKeyGroup(context.Background(), site, "sk-target-secret")
			if e != nil || found.ID != "2" {
				t.Fatal("did not find full-key match on later page", found.ID, e)
			}
			if platform == PlatformSub2API && found.GroupID != "7" || platform == PlatformNewAPI && (found.GroupID != "pro" || reveals != 1) {
				t.Fatal("wrong Key group", found.GroupID)
			}
		})
	}
	if SameMonitorKey(PlatformNewAPI, "tar***ret", "target-secret") || SameMonitorKey(PlatformSub2API, "sk-aaa", "aaa") {
		t.Fatal("partial Key accepted")
	}
	if MonitorAPIBase("https://example.test/proxy/v1/") != "https://example.test/proxy" || MonitorAPIBase("https://example.test/proxy") == MonitorAPIBase("https://example.test/other") {
		t.Fatal("API base paths mixed")
	}
	if !MonitorAPICandidate("https://dc.apikl.ai", "https://apikl.ai") || !MonitorAPICandidate("https://vapi.example.test", "https://direct-vapi.example.test") || MonitorAPICandidate("https://same.test/one", "https://same.test/two") || MonitorAPICandidate("https://unrelated.test", "https://apikl.ai") {
		t.Fatal("API alias candidates incorrectly selected")
	}
}
func TestForwardingCredentialsStayServerSide(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"items":[{"id":9,"type":"apikey","platform":"openai","credentials":{"base_url":"https://upstream.test/v1","api_key":"private-existing-key"},"group_ids":[1,2]}],"total":1}}`))
	}))
	defer server.Close()
	service := NewPlatformService(NewHTTPClient(server.Client()))
	rows, e := service.ListSub2APIAdminAccounts(Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "admin"})
	if e != nil || len(rows) != 1 || rows[0].APIKey != "private-existing-key" || len(rows[0].GroupIDs) != 2 {
		t.Fatal("forwarding credentials not parsed", e)
	}
	b, _ := json.Marshal(rows)
	if strings.Contains(string(b), "private-existing-key") || strings.Contains(string(b), "upstream.test") {
		t.Fatal("forwarding credentials exposed in JSON")
	}
}

func TestForwardingCredentialExportUsesExactAccountAndHonorsDenial(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint(denied), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != "GET" {
					t.Error("credential discovery mutated account")
				}
				if r.URL.Path == "/api/v1/admin/accounts/9" {
					w.Write([]byte(`{"data":{"id":9,"type":"apikey","platform":"openai","credentials":{"base_url":"https://upstream.test"},"group_ids":[1]}}`))
					return
				}
				if r.URL.Path != "/api/v1/admin/accounts/data" || r.URL.Query().Get("ids") != "9" || r.URL.Query().Get("include_proxies") != "false" {
					t.Error("credential export exceeded selected account")
					w.WriteHeader(400)
					return
				}
				if denied {
					w.WriteHeader(403)
					w.Write([]byte(`{"message":"STEP_UP_REQUIRED"}`))
					return
				}
				w.Write([]byte(`{"data":{"accounts":[{"platform":"openai","credentials":{"base_url":"https://upstream.test","api_key":"private-key"}}]}}`))
			}))
			defer server.Close()
			service := NewPlatformService(NewHTTPClient(server.Client()))
			a, e := service.FetchForwardingAccount(context.Background(), Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "admin"}, "9")
			if denied {
				if e == nil || a.APIKey != "" {
					t.Fatal("restricted export was bypassed")
				}
				return
			}
			if e != nil || a.ID != "9" || a.APIKey != "private-key" || len(a.GroupIDs) != 1 {
				t.Fatal("selected export did not hydrate forwarding credentials", e)
			}
		})
	}
}
