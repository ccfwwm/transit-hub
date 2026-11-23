package upstream

import (
	"sort"
	"time"
)

func marketplaceOutcome(m map[string]any) string {
	switch safeString(m, "outcome") {
	case "unknown":
		return "unknown"
	case "degraded":
		return "warning"
	case "fault":
		return "failed"
	case "success":
	default:
		if _, ok := m["success"].(bool); !ok {
			return "unknown"
		}
	}
	if m["success"] == true || safeString(m, "outcome") == "success" {
		if n := firstNumber(m, []string{"first_token_ms"}); n != nil && *n > 15000 {
			return "warning"
		}
		return "healthy"
	}
	return "failed"
}
func parseMarketplaceHealth(payload any, site Site, now time.Time) []ModelHealth {
	out := []ModelHealth{}
	for _, raw := range dataArray(payload) {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		a, ok := m["availability"].(map[string]any)
		if !ok {
			continue
		}
		name := safeString(m, "name")
		id := groupID2(m)
		gid := ""
		for _, g := range site.Metrics.Groups {
			if g.ID == id {
				gid = g.ID
				break
			}
		}
		if gid == "" {
			gid = mappedGroup(site, name)
		}
		h := ModelHealth{ID: "sub2api_marketplace:" + id, Model: name, GroupID: gid, GroupName: name, Source: "sub2api_marketplace", Scope: "channel_reference", Mode: "unknown", CollectedAt: now, Timeline: []HealthPoint{}, ObservationKind: "group_request_logs", Message: "上游号池最近请求状态（分组聚合），不代表全部模型均已检测"}
		h.CheckedAt = healthTime(a["last_checked_at"])
		h.Status = marketplaceOutcome(map[string]any{"outcome": a["last_status"]})
		if rate := firstNumber(a, []string{"availability_rate"}); rate != nil && *rate >= 0 && *rate <= 1 {
			n := *rate * 100
			h.Availability = &n
		}
		items, _ := a["recent_results"].([]any)
		for _, raw := range items {
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			point := HealthPoint{Status: marketplaceOutcome(p), CheckedAt: healthTime(p["checked_at"])}
			if n := firstNumber(p, []string{"first_token_ms"}); n != nil && *n >= 0 {
				v := int(*n)
				point.LatencyMS = &v
			}
			h.Timeline = append(h.Timeline, point)
		}
		sort.SliceStable(h.Timeline, func(i, j int) bool {
			a, b := h.Timeline[i].CheckedAt, h.Timeline[j].CheckedAt
			if a == nil {
				return b != nil
			}
			return b != nil && a.Before(*b)
		})
		if len(h.Timeline) > 60 {
			h.Timeline = h.Timeline[len(h.Timeline)-60:]
		}
		if len(h.Timeline) > 0 {
			last := h.Timeline[len(h.Timeline)-1]
			if h.CheckedAt == nil || last.CheckedAt != nil && !last.CheckedAt.Before(*h.CheckedAt) {
				h.Status = last.Status
				h.LatencyMS = last.LatencyMS
				h.CheckedAt = last.CheckedAt
			}
		}
		if len(h.Timeline) == 0 && h.CheckedAt == nil {
			continue
		}
		h.CurrentStatus = h.Status
		out = append(out, qualifyHealth(h))
	}
	return out
}
