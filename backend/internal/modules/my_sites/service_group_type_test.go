package my_sites

import "testing"

func TestGrokGroupTypeMappings(t *testing.T) {
	if got := groupTypeToNewAPIChannelType("grok"); got != 48 {
		t.Fatalf("groupTypeToNewAPIChannelType(grok) = %d, want 48", got)
	}
	if got := groupTypeToNewAPIChannelType("xai"); got != 48 {
		t.Fatalf("groupTypeToNewAPIChannelType(xai) = %d, want 48", got)
	}
	if got := groupTypePrefix("grok"); got != "G" {
		t.Fatalf("groupTypePrefix(grok) = %q, want G", got)
	}

	payload := buildAccountPayload("grok", "https://api.example.com", "sk-test", []int{7}, "grok-account")
	if got := payload["platform"]; got != "grok" {
		t.Fatalf("platform = %v, want grok", got)
	}
	if got := payload["concurrency"]; got != 1000 {
		t.Fatalf("concurrency = %v, want 1000", got)
	}
	credentials, ok := payload["credentials"].(map[string]any)
	if !ok || credentials["pool_mode"] != true {
		t.Fatalf("credentials.pool_mode = %v, want true", credentials["pool_mode"])
	}
	extra, ok := payload["extra"].(map[string]any)
	if !ok || extra["openai_passthrough"] != true {
		t.Fatalf("extra.openai_passthrough = %v, want true", extra["openai_passthrough"])
	}
}
