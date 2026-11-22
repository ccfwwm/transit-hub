package upstream

import (
	"encoding/json"
	"testing"
)

func TestSitePayloadPreservesSkipTLSVerify(t *testing.T) {
	site := &Site{
		ID:            "site-with-invalid-certificate",
		SkipTLSVerify: true,
		Settings:      SiteSettings{GroupAPIURLs: map[string]string{"g": "https://dc.example.test"}, BalanceThreshold: floatPtrForCache(3), MonitorProfile: &MonitorProfile{Template: "sub2api_plaza", Path: "/custom/model-plaza"}},
		Session: &Session{
			Platform:        PlatformNewAPI,
			InsecureSkipTLS: true,
		},
	}

	encoded, err := json.Marshal(toPayload(site))
	if err != nil {
		t.Fatalf("marshal site payload: %v", err)
	}
	var payload sitePayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal site payload: %v", err)
	}

	restored := fromPayload(payload)
	if restored.Settings.GroupAPIURLs["g"] != "https://dc.example.test" {
		t.Fatal("group API address lost in Redis")
	}
	if restored.Settings.MonitorProfile == nil || restored.Settings.MonitorProfile.Path != "/custom/model-plaza" || restored.Settings.BalanceThreshold == nil || *restored.Settings.BalanceThreshold != 3 {
		t.Fatal("site settings were dropped by Redis serialization")
	}
	if !restored.SkipTLSVerify {
		t.Fatal("expected site-scoped TLS bypass to survive the Redis payload round trip")
	}
	if restored.Session == nil || !restored.Session.InsecureSkipTLS {
		t.Fatal("expected the session TLS setting to survive the Redis payload round trip")
	}
}

func floatPtrForCache(v float64) *float64 { return &v }
