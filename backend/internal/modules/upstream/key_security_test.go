package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSecurityClonePreservesRestrictionsAndRemainingQuota(t *testing.T) {
	old := map[string]any{"group_id": float64(113), "quota": float64(100), "quota_used": float64(23), "ip_blacklist": []any{"192.0.2.4"}, "status": "inactive", "expires_at": "2027-01-02T03:04:05Z", "rate_limit_1d": float64(9), "key": "must-not-copy", "id": float64(44)}
	got, e := SecurityCloneConfig(PlatformSub2API, old, "rotation", []string{DefaultMonitorIP})
	if e != nil {
		t.Fatal(e)
	}
	if got["quota"] != float64(77) || got["status"] != "inactive" || got["expires_at"] != old["expires_at"] || got["rate_limit_1d"] != old["rate_limit_1d"] || got["key"] != nil || got["id"] != nil {
		t.Fatal("restriction lost", got)
	}
	old["quota_used"] = float64(100)
	if _, e = SecurityCloneConfig(PlatformSub2API, old, "rotation", []string{DefaultMonitorIP}); e == nil {
		t.Fatal("exhausted quota became unlimited")
	}
	n := map[string]any{"group": "pro", "remain_quota": float64(17), "expired_time": float64(1800000000), "status": float64(2), "unlimited_quota": false, "model_limits_enabled": true, "model_limits": "a,b", "cross_group_retry": false, "key": "never"}
	got, e = SecurityCloneConfig(PlatformNewAPI, n, "rotation", []string{DefaultMonitorIP})
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"group", "remain_quota", "expired_time", "status", "unlimited_quota", "model_limits_enabled", "model_limits", "cross_group_retry"} {
		if !reflect.DeepEqual(n[key], got[key]) {
			t.Fatal(key, "changed")
		}
	}
	if got["allow_ips"] != DefaultMonitorIP || got["key"] != nil {
		t.Fatal(got)
	}
	delete(n, "group")
	n["group_config"] = ""
	n["preset_id"] = float64(0)
	n["sticky_session"] = true
	got, e = SecurityCloneConfig(PlatformNewAPI, n, "rotation", []string{DefaultMonitorIP})
	if e != nil || got["group_config"] != "" || got["sticky_session"] != true || got["group"] != nil {
		t.Fatal("fork group config lost", e)
	}
	if !SecurityKeyGroupMatches(PlatformNewAPI, n, "configured-group") {
		t.Fatal("fork was treated as a different explicit group")
	}
}

func TestForwardingRotationPreservesCredentialsAndManualPause(t *testing.T) {
	credentials := map[string]any{"api_key": "old-secret", "base_url": "https://dc.example.test", "pool_mode": true, "nested": map[string]any{"keep": true}}
	puts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		record := map[string]any{"id": float64(2211), "type": "apikey", "platform": "deepseek", "credentials": credentials, "schedulable": false, "priority": float64(17), "rate_multiplier": 0.6}
		if r.Method == "PUT" {
			puts++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body) != 1 || body["schedulable"] != nil || body["priority"] != nil {
				t.Error("unrelated account settings overwritten")
			}
			next := body["credentials"].(map[string]any)
			if next["base_url"] != credentials["base_url"] || next["pool_mode"] != true || next["nested"] == nil {
				t.Error("credentials lost")
			}
			credentials = next
		}
		data := any(record)
		if r.URL.Path == "/api/v1/admin/accounts/data" {
			data = map[string]any{"accounts": []any{record}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	p := NewPlatformService(NewHTTPClient(server.Client()))
	session := Session{BaseURL: server.URL, Platform: PlatformSub2API, AccessToken: "fixture"}
	for i := 0; i < 2; i++ {
		if e := p.ReplaceForwardingKey(context.Background(), session, "2211", "old-secret", "new-secret"); e != nil {
			t.Fatal(e)
		}
	}
	if puts != 1 || credentials["api_key"] != "new-secret" {
		t.Fatal("retry was not idempotent")
	}
	if e := p.ReplaceForwardingKey(context.Background(), session, "2211", "unexpected-old", "another-secret"); e == nil {
		t.Fatal("manual credential change overwritten")
	}
}

func TestSecurityCreateRecoversAcceptedRequestAndVerifiesRevocation(t *testing.T) {
	creates := 0
	deleted := false
	ignoreDelete := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			creates++
			t.Error("accepted key was submitted again")
		}
		if r.Method == "DELETE" && !ignoreDelete {
			deleted = true
		}
		items := []any{}
		if !deleted {
			items = append(items, map[string]any{"id": float64(9), "name": "THrotate-unique", "key": "created-once"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": items, "total": len(items)}})
	}))
	defer server.Close()
	p := NewPlatformService(NewHTTPClient(server.Client()))
	session := Session{BaseURL: server.URL, Platform: PlatformSub2API, AccessToken: "fixture"}
	id, key, e := p.CreateSecurityKey(context.Background(), session, map[string]any{"name": "THrotate-unique"}, "custom-secret")
	if e != nil || id != "9" || key != "created-once" || creates != 0 {
		t.Fatal("lost response recovery failed", e)
	}
	if e = p.DeleteSecurityKey(context.Background(), session, id); e == nil {
		t.Fatal("ignored delete was reported successful")
	}
	ignoreDelete = false
	if e = p.DeleteSecurityKey(context.Background(), session, id); e != nil {
		t.Fatal(e)
	}
}

func TestSecurityInventoryHandlesLegacyArrayAndDeniedResponse(t *testing.T) {
	denied := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if denied {
			w.Write([]byte(`{"success":false,"message":"login required"}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":7,"group":"pro"}]}`))
	}))
	defer server.Close()
	p := NewPlatformService(NewHTTPClient(server.Client()))
	session := Session{BaseURL: server.URL, Platform: PlatformNewAPI, AccessToken: "fixture", UserID: "1"}
	rows, e := p.SecurityKeys(context.Background(), session)
	if e != nil || len(rows) != 1 || calls != 1 {
		t.Fatal("legacy array pagination", len(rows), calls, e)
	}
	denied = true
	if _, e = p.SecurityKeys(context.Background(), session); e == nil {
		t.Fatal("permission failure presented as empty key list")
	}
}

func TestSecurityTokenUsesUnmaskedLegacyDetail(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts++
			t.Error("legacy token unexpectedly requested missing key action")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"data":{"id":7,"key":"legacy-token-0123456789abcdef","group_config":"","allow_ips":"206.82.2.46"}}`))
	}))
	defer server.Close()
	p := NewPlatformService(NewHTTPClient(server.Client()))
	key, e := p.SecurityTokenKey(context.Background(), Session{Platform: PlatformNewAPI, BaseURL: server.URL, AccessToken: "fixture", UserID: "1"}, "7")
	if e != nil || key != "legacy-token-0123456789abcdef" || posts != 0 {
		t.Fatal("legacy key unsupported", e)
	}
}
