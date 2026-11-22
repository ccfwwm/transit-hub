package httpserver

import "testing"

func TestUpstreamKeyManagementUsesAuthMiddleware(t *testing.T) {
	server := &Server{}
	for _, path := range []string{"/api/upstream-keys", "/api/upstream-keys/site/key", "/api/upstream-keys/site/key/usage", "/api/upstream-key-bindings/preview", "/api/upstream-key-bindings/apply"} {
		if !server.protectedPath(path) {
			t.Errorf("authentication middleware not enabled for %s", path)
		}
	}
}
