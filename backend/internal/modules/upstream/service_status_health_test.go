package upstream

import (
	"testing"
	"time"
)

func TestNewAPIServiceComponentsKeepSourceScope(t *testing.T) {
	payload := healthFixture(t, `{"data":{"updated_at":"2026-11-20T23:14:04+08:00","overall":{"state":"major_outage"},"components":[{"name":"codex-限时","summary":{"state":"operational"},"uptime_rate":0.91,"today_points":[{"hour":"2026-11-20T14:00:00Z","state":"partial_outage"},{"hour":"2026-11-20T15:00:00Z","state":"operational"}]},{"name":"gpt-image-2","summary":{"state":"degraded"},"uptime_rate":0.8,"today_points":[{"hour":"2026-11-20T14:00:00Z","state":"major_outage"}]}]}}`)
	pricing := healthFixture(t, `{"data":[{"model_name":"gpt-image-2","enable_groups":["IMAGE"]}]}`)
	rows := parseNewAPIServiceStatus(payload, pricing, Site{Metrics: Metrics{Groups: []GroupInfo{{ID: "g", Name: "codex-限时"}, {ID: "i", Name: "IMAGE"}}}}, time.Now())
	if len(rows) != 2 || rows[0].Scope != "channel_reference" || rows[0].GroupID != "g" || rows[0].Status != "healthy" || len(rows[0].Timeline) != 2 || rows[0].Timeline[0].Status != "warning" || rows[0].Usable {
		t.Fatalf("bad service component: %+v", rows)
	}
	if rows[1].Scope != "model_reference" || rows[1].GroupID != "i" || rows[1].Status != "warning" || rows[1].Timeline[0].Status != "failed" || rows[1].Usable {
		t.Fatal("service model lost or promoted")
	}
}
