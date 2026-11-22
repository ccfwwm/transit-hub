package upstream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNewAPITokenCreationBoundsLongGroupNameAndLooksUpExactName(t *testing.T) {
	var saved, group string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/token/":
			var p map[string]any
			if json.NewDecoder(r.Body).Decode(&p) != nil {
				t.Fatal("invalid request")
			}
			saved = p["name"].(string)
			group = p["group"].(string)
			if len(saved) > 50 || !utf8.ValidString(saved) {
				t.Errorf("invalid token name: %q", saved)
			}
			if p["allow_ips"] != "206.82.2.46" {
				t.Error("whitelist lost")
			}
			w.Write([]byte(`{"success":true}`))
		case r.Method == "GET" && r.URL.Path == "/api/token/":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{map[string]any{"id": 123, "name": saved}}})
		case r.Method == "POST" && r.URL.Path == "/api/token/123/key":
			w.Write([]byte(`{"success":true,"data":{"key":"exact-key"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	s := NewPlatformService(NewHTTPClient(server.Client()))
	name := "ABCD-foxrouter-有视频-国模svip分组（0.06x）"
	id, key, e := s.CreateNewAPIToken(Session{Platform: PlatformNewAPI, BaseURL: server.URL, AccessToken: "auth", UserID: "1"}, name, "国模svip分组（0.06x）")
	if e != nil || id != "123" || key != "exact-key" || group != "国模svip分组（0.06x）" || saved == name {
		t.Fatalf("creation failed: id=%s name=%q group=%q err=%v", id, saved, group, e)
	}
	if boundedNewAPITokenName(name) != saved || boundedNewAPITokenName(name+"another") == saved || !strings.HasPrefix(saved, "ABCD-") {
		t.Fatal("bounded name lost deterministic identity")
	}
}
func TestNewAPIRejectedTokenCreationIsNotInvalidJSONAndDoesNotContinue(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"message":"rejected"}`))
	}))
	defer server.Close()
	s := NewPlatformService(NewHTTPClient(server.Client()))
	_, _, e := s.CreateNewAPIToken(Session{Platform: PlatformNewAPI, BaseURL: server.URL, AccessToken: "auth", UserID: "1"}, "test", "g")
	if e == nil || e.Error() != ErrorTokenCreationRejected || calls != 1 {
		t.Fatalf("business rejection treated as invalid JSON or retried: %v calls=%d", e, calls)
	}
}
