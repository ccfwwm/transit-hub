package httpserver

import "testing"

func TestMonitorTemplateRouteReceivesAuthenticatedContext(t *testing.T) {
	server := &Server{}
	for _, path := range []string{"/api/upstream-monitor-templates", "/api/upstream-sites/site/group-key-policy", "/api/channel-monitor/rules/rule/models/check"} {
		if !server.protectedPath(path) {
			t.Fatalf("monitor route bypasses authenticated context: %s", path)
		}
	}
}
