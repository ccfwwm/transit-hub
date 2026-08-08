package upstream

import (
	"encoding/json"
	"testing"
)

func TestSitePayloadPreservesSkipTLSVerify(t *testing.T) {
	site := &Site{
		ID:            "site-with-invalid-certificate",
		SkipTLSVerify: true,
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
	if !restored.SkipTLSVerify {
		t.Fatal("expected site-scoped TLS bypass to survive the Redis payload round trip")
	}
	if restored.Session == nil || !restored.Session.InsecureSkipTLS {
		t.Fatal("expected the session TLS setting to survive the Redis payload round trip")
	}
}
