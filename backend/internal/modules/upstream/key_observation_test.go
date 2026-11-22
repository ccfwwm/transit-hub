package upstream

import (
	"testing"
	"time"
)

func TestKeyObservationUsesOnlyExplicitWhitelistMetadata(t *testing.T) {
	c := NewHTTPClient(nil)
	received := make(chan []KeySecurityObservation, 4)
	c.SetKeyObserver(func(v []KeySecurityObservation) { received <- v })
	c.notifyKeyResponse("https://example.test/proxy/api/v1/keys?page=1", map[string]any{"data": map[string]any{"items": []any{map[string]any{"id": float64(7), "group_id": float64(113), "key": "never-forward-this", "ip_whitelist": []any{"206.82.2.46"}}, map[string]any{"id": float64(8), "key": "hidden-no-whitelist"}}}})
	select {
	case values := <-received:
		if len(values) != 1 || values[0].KeyID != "7" || values[0].BaseURL != "https://example.test/proxy" || values[0].GroupID != "113" || values[0].AllowedIPs[0] != DefaultMonitorIP {
			t.Fatal(values)
		}
	case <-time.After(time.Second):
		t.Fatal("missing observation")
	}
	c.notifyKeyResponse("https://example.test/api/token/7", map[string]any{"data": map[string]any{"id": float64(7), "group": "pro", "allow_ips": nil}})
	select {
	case values := <-received:
		if len(values) != 1 || len(values[0].AllowedIPs) != 0 {
			t.Fatal("null whitelist not treated as unrestricted")
		}
	case <-time.After(time.Second):
		t.Fatal("missing unrestricted observation")
	}
	c.notifyKeyResponse("https://example.test/api/v1/channel-monitors", map[string]any{"data": map[string]any{"id": float64(7), "ip_whitelist": []any{}}})
	select {
	case <-received:
		t.Fatal("unrelated response treated as key")
	case <-time.After(20 * time.Millisecond):
	}
}
