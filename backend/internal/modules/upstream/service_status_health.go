package upstream

import "time"

// Some New API installations expose their user-facing status page as service
// components rather than Kuma monitors or model performance statistics.
func parseNewAPIServiceStatus(payload, pricing any, site Site, now time.Time) []ModelHealth {
	data := dataRecord(payload)
	items, _ := data["components"].([]any)
	offers := map[string]healthModelOffer{}
	for _, o := range newAPIHealthOffers(pricing) {
		offers[o.model] = o
	}
	out := []ModelHealth{}
	for _, raw := range items {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := safeString(m, "name")
		if name == "" {
			continue
		}
		summary, _ := m["summary"].(map[string]any)
		h := ModelHealth{ID: "newapi_service:" + name, Model: name, GroupName: name, GroupID: mappedGroup(site, name), Source: "newapi_service", Scope: "channel_reference", Mode: "unknown", Status: healthStatus(safeString(summary, "state")), CollectedAt: now, CheckedAt: healthTime(data["updated_at"]), Timeline: []HealthPoint{}, ObservationKind: "service_status", Message: "上游状态页服务组件，不代表指定 Key 的逐模型检测"}
		if n := firstNumber(m, []string{"uptime_rate"}); n != nil && *n >= 0 && *n <= 1 {
			v := *n * 100
			h.Availability = &v
		}
		points, _ := m["today_points"].([]any)
		for _, raw := range points {
			if p, ok := raw.(map[string]any); ok {
				h.Timeline = append(h.Timeline, HealthPoint{Status: healthStatus(safeString(p, "state")), CheckedAt: healthTime(p["hour"])})
			}
		}
		if len(h.Timeline) > 60 {
			h.Timeline = h.Timeline[len(h.Timeline)-60:]
		}
		if offer, ok := offers[name]; ok {
			h.Scope = "model_reference"
			h.GroupID = ""
			h.GroupName = ""
			h.Mode = offer.mode
			h.Message = "上游状态页该模型的服务状态，非当前 Key 的检测"
			out = append(out, offerHealth(h, site, offer.groups)...)
		} else {
			out = append(out, qualifyHealth(h))
		}
	}
	return out
}
