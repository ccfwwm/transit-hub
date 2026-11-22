package upstream

import (
	"fmt"
	"sort"
	"time"
)

func plazaReport(record map[string]any, base ModelHealth) ModelHealth {
	a, _ := record["availability"].(map[string]any)
	p, _ := record["performance"].(map[string]any)
	base.Status = healthStatus(safeString(a, "status"))
	base.ObservationKind = safeString(a, "source")
	base.Timeline = []HealthPoint{}
	points, _ := p["trend"].([]any)
	if len(points) == 0 {
		if last, ok := p["last_observation"].(map[string]any); ok {
			points = []any{last}
		}
	}
	for _, raw := range points {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["padded"] == true {
			continue
		}
		point := HealthPoint{Status: healthStatus(safeString(m, "status")), CheckedAt: healthTime(m["bucket_start"])}
		if n := firstNumber(m, []string{"ttft_ms", "first_token_ms", "latency_ms"}); n != nil {
			v := int(*n)
			point.LatencyMS = &v
		}
		base.Timeline = append(base.Timeline, point)
	}
	sort.SliceStable(base.Timeline, func(i, j int) bool {
		a, b := base.Timeline[i].CheckedAt, base.Timeline[j].CheckedAt
		if a == nil {
			return b != nil
		}
		return b != nil && a.Before(*b)
	})
	if len(base.Timeline) > 60 {
		base.Timeline = base.Timeline[len(base.Timeline)-60:]
	}
	if len(base.Timeline) > 0 {
		last := base.Timeline[len(base.Timeline)-1]
		base.CheckedAt = last.CheckedAt
		base.LatencyMS = last.LatencyMS
	}
	if n := firstNumber(p, []string{"avg_ttft_ms", "ttft_ms", "latency_ms"}); n != nil && *n > 0 {
		v := int(*n)
		base.LatencyMS = &v
	}
	// 'default' media availability is a configured fallback on the upstream,
	// rather than a successful observation. Keep it unknown if no real samples.
	if base.ObservationKind == "default" && len(base.Timeline) == 0 {
		base.Status = "unknown"
		base.Message = "上游默认可用配置，尚无真实观测"
	}
	return qualifyHealth(base)
}
func parseModelPlazaHealth(payload any, site Site, now time.Time) []ModelHealth {
	groups, _ := dataRecord(payload)["groups"].([]any)
	out := []ModelHealth{}
	for _, raw := range groups {
		g, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := groupID2(g)
		name := safeString(g, "name")
		gid := ""
		for _, own := range site.Metrics.Groups {
			if own.ID == id {
				gid = id
				break
			}
		}
		if gid == "" {
			gid = mappedGroup(site, name)
		}
		base := ModelHealth{ID: "sub2api_plaza:" + id, GroupID: gid, GroupName: name, Source: "sub2api_plaza", Scope: "channel_reference", Mode: safeString(g, "platform"), Model: name, CollectedAt: now, Message: "上游分组调用状态参考，不代替每个模型的独立结果"}
		group := plazaReport(g, base)
		if len(group.Timeline) > 0 || group.Status != "unknown" {
			out = append(out, group)
		}
		models, _ := g["models"].([]any)
		for _, raw := range models {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			model := safeString(m, "name")
			if model == "" {
				continue
			}
			h := base
			h.Model = model
			h.ID = fmt.Sprintf("%s:%s:%s", base.ID, safeString(m, "platform"), model)
			h.Scope = "group_model_reference"
			h.Mode = safeString(m, "platform")
			h.Message = "上游该分组和模型的观测结果，非当前 Key 的主动检测"
			h = plazaReport(m, h)
			if h.Status == "unknown" && len(h.Timeline) == 0 {
				continue
			}
			out = append(out, h)
		}
	}
	return out
}
