package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
)

func TestSub2APIGroupSettingsUseNativeAllowlistAndPartialUpdate(t *testing.T) {
	group := map[string]any{"id": 5, "status": "active", "model_allowlist": map[string]any{"enabled": false}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/groups/5/model-allowlist-candidates" {
			writeJSON(w, map[string]any{"data": map[string]any{"models": []string{"gpt-image-2", "gpt-image-2.5"}}})
			return
		}
		if r.URL.Path != "/api/v1/admin/groups/5" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPut {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body) != 2 || body["status"] != "inactive" || body["model_allowlist"] == nil {
				t.Errorf("unexpected group update: %+v", body)
			}
			group["status"], group["model_allowlist"] = body["status"], body["model_allowlist"]
		}
		writeJSON(w, map[string]any{"data": group})
	}))
	defer server.Close()
	service := NewPlatformService(NewHTTPClient(server.Client()))
	session := Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "fixture"}
	ctx := context.Background()
	before, err := service.FetchSub2APIGroupSettings(ctx, session, "5", true)
	if err != nil || len(before.Candidates) != 2 || before.Allowlist == nil || before.Allowlist.Enabled {
		t.Fatalf("initial settings = %+v, err = %v", before, err)
	}
	update := Sub2APIGroupSettings{Status: "inactive", Allowlist: &ModelAllowlist{Enabled: true, Models: []string{"gpt-image-*"}}}
	after, err := service.UpdateSub2APIGroupSettings(ctx, session, "5", update)
	if err != nil || !reflect.DeepEqual(update, after) {
		t.Fatalf("saved settings = %+v, err = %v", after, err)
	}
}

func TestForwardingModelSettingsPreserveNativeAccountConfiguration(t *testing.T) {
	for _, platform := range []string{"openai", "imagevideo", "anthropic"} {
		t.Run(platform, func(t *testing.T) {
			credentials := map[string]any{"base_url": "https://example.test", "pool_mode": true}
			extra := map[string]any{"other_setting": true}
			account := map[string]any{"id": 32, "platform": platform, "type": "apikey", "credentials": credentials, "extra": extra}
			credentials["model_mapping"] = map[string]string{"model-a": "original-upstream-alias", "model-b": "model-b"}
			if platform == "openai" {
				extra["openai_passthrough"] = true
			}
			if platform == "imagevideo" {
				extra["media_models"] = []map[string]any{
					{"model": "model-a", "enabled": true, "template_ref": map[string]any{"id": "asset", "version": 1}},
					{"model": "model-b", "enabled": true, "template_ref": map[string]any{"id": "asset", "version": 1}},
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/admin/accounts/32" {
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if r.Method == http.MethodPut {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if updated, ok := body["credentials"].(map[string]any); ok {
						if updated["pool_mode"] != true || updated["base_url"] != credentials["base_url"] || updated["api_key"] != nil {
							t.Errorf("credential data was overwritten: %+v", updated)
						}
						mapping, _ := updated["model_mapping"].(map[string]any)
						if v := mapping["model-a"]; v != nil && v != "original-upstream-alias" {
							t.Errorf("existing alias was overwritten: %v", v)
						}
						account["credentials"] = updated
					}
					if updated, ok := body["extra"].(map[string]any); ok {
						if updated["other_setting"] != true {
							t.Errorf("unrelated account settings were overwritten: %+v", updated)
						}
						if platform == "imagevideo" {
							items, _ := updated["media_models"].([]any)
							if len(items) != 2 || items[0].(map[string]any)["template_ref"] == nil || items[1].(map[string]any)["enabled"] != false {
								t.Errorf("media capability was dropped or overwritten: %+v", updated)
							}
						}
						account["extra"] = updated
					}
				}
				writeJSON(w, map[string]any{"data": account})
			}))
			defer server.Close()
			session := Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "fixture"}
			service := NewPlatformService(NewHTTPClient(server.Client()))
			ctx := context.Background()
			before, err := service.FetchForwardingModelSettings(ctx, session, "32")
			if err != nil || len(before.Models) != 2 || before.Enabled != (platform != "openai") {
				t.Fatalf("initial settings = %+v, err = %v", before, err)
			}
			requested := []string{"model-a"}
			saved, err := service.UpdateForwardingModelSettings(ctx, session, "32", ModelAllowlist{Enabled: true, Models: requested})
			if err != nil || !saved.Enabled || !slices.Equal(saved.Models, requested) {
				t.Fatalf("saved settings = %+v, err = %v", saved, err)
			}
			if platform != "imagevideo" {
				saved, err = service.UpdateForwardingModelSettings(ctx, session, "32", ModelAllowlist{})
				if err != nil || saved.Enabled {
					t.Fatalf("cleared settings = %+v, err = %v", saved, err)
				}
				if platform == "openai" && !slices.Equal(saved.Models, requested) {
					t.Fatalf("disabled passthrough did not preserve the draft allowlist: %+v", saved)
				}
			}
		})
	}
}
