package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestForwardingAPIURLPreservesCredentialOptionsAndBoundKey(t *testing.T) {
	credentials := map[string]any{"api_key": "bound-key", "base_url": "https://old.example/v1", "model_mapping": map[string]any{"a": "b"}, "enable_responses": true}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/admin/accounts/data":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"accounts": []any{map[string]any{"platform": "openai", "type": "apikey", "credentials": credentials}}}})
		case r.Method == "PUT" && r.URL.Path == "/api/v1/admin/accounts/9":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if len(body) != 1 {
				t.Error("unrelated account fields overwritten")
			}
			c := body["credentials"].(map[string]any)
			if c["api_key"] != "bound-key" || c["enable_responses"] != true || !reflect.DeepEqual(c["model_mapping"], credentials["model_mapping"]) {
				t.Error("credential options lost")
			}
			credentials = c
			writes++
			w.Write([]byte(`{"data":{}}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/admin/accounts/9":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 9, "platform": "openai", "type": "apikey", "credentials": credentials}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	s := NewPlatformService(NewHTTPClient(server.Client()))
	session := Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "admin"}
	if e := s.UpdateForwardingAPIURL(context.Background(), session, "9", "bound-key", "https://new.example/prefix"); e != nil {
		t.Fatal(e)
	}
	if e := s.UpdateForwardingAPIURL(context.Background(), session, "9", "bound-key", "https://new.example/prefix"); e != nil || writes != 1 {
		t.Fatalf("retry not idempotent: writes=%d err=%v", writes, e)
	}
	if e := s.UpdateForwardingAPIURL(context.Background(), session, "9", "different-key", "https://other.example"); e == nil || writes != 1 {
		t.Fatal("modified a different account Key")
	}
}

func TestAPIURLPriorityAndNormalization(t *testing.T) {
	s := Site{BaseURL: "https://manage.example", Settings: SiteSettings{DefaultAPIURL: "https://api.example/prefix", APIURLManaged: true, GroupAPIURLs: map[string]string{"g": "https://group.example", "reset": ""}}}
	if EffectiveAPIURL(s, "g", "https://old.example") != "https://group.example" || EffectiveAPIURL(s, "reset", "https://old.example") != "https://api.example/prefix" || EffectiveAPIURL(s, "other", "https://old.example") != "https://api.example/prefix" {
		t.Fatal("API priority incorrect")
	}
	s.Settings = SiteSettings{}
	if EffectiveAPIURL(s, "g", "https://legacy.example") != "https://legacy.example" {
		t.Fatal("legacy route changed without an explicit edit")
	}
	for _, raw := range []string{"https://user:secret@api.example", "file:///tmp/file", "https://api.example?key=secret", "https://api.example/#key", "https://"} {
		if _, e := NormalizeRoutingURL(raw); e == nil {
			t.Errorf("accepted invalid URL %q", raw)
		}
	}
	for _, raw := range []string{"*.example.com", "127.0.0.1", "localhost", "api.example.com/path", "https://api.example.com:443", "foo.local", "a..com"} {
		if _, e := NormalizeUpstreamHost(raw); e == nil {
			t.Errorf("accepted invalid host %q", raw)
		}
	}
	if h, e := NormalizeUpstreamHost(" HTTPS://API.Example.COM/ "); e != nil || h != "api.example.com" {
		t.Fatalf("host normalization: %q %v", h, e)
	}
}
