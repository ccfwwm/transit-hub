package upstream

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// This format is a published group request-statistics report. Its model list
// identifies supported models; the report is not a separate test of each Key.
func imageReportStatus(v any) string {
	r, ok := v.([]any)
	if !ok || len(r) < 2 {
		return "unknown"
	}
	n, nok := r[0].(float64)
	f, fok := r[1].(float64)
	if !nok || !fok || n <= 0 || f < 0 || f > n || math.IsNaN(n) || math.IsNaN(f) {
		return "unknown"
	}
	if f*10 < n {
		return "healthy"
	}
	if f >= 3 && f*10 >= n*3 {
		return "failed"
	}
	return "warning"
}
func imageReportCurrent(g map[string]any) string {
	if r, ok := g["recent"].([]any); ok && len(r) > 0 {
		if n, ok := r[0].(float64); ok && n >= 3 {
			return imageReportStatus(r)
		}
	}
	if n := firstNumber(g, []string{"tail_ok"}); n != nil && *n >= 2 {
		return "healthy"
	}
	return imageReportStatus(g["last20"])
}
func imageSlotStatus(v any) string {
	switch v {
	case "ok":
		return "healthy"
	case "warn":
		return "warning"
	case "bad":
		return "failed"
	default:
		return "unknown"
	}
}
func imageReportTimeline(g map[string]any, now time.Time, interval int) []HealthPoint {
	slots, _ := g["slots"].([]any)
	// Display uses carried colors, as on the source page. These timestamps are
	// buckets and never used to calculate current state or failure counters.
	anyRequests := false
	for _, raw := range slots {
		if p, ok := raw.([]any); ok && len(p) > 1 && imageSlotStatus(p[1]) != "unknown" {
			anyRequests = true
		}
	}
	carry := imageSlotStatus(g["prev"])
	if !anyRequests {
		carry = imageReportCurrent(g)
	}
	tail := 0.0
	var lastAt *time.Time
	if p, ok := g["prev_tail"].([]any); ok && len(p) > 1 {
		tail, _ = p[0].(float64)
		lastAt = healthTime(p[1])
	}
	out := []HealthPoint{}
	for _, raw := range slots {
		p, ok := raw.([]any)
		if !ok || len(p) < 2 {
			continue
		}
		stamp := healthTime(p[0])
		if stamp == nil {
			continue
		}
		status := imageSlotStatus(p[1])
		if status != "unknown" {
			carry = status
			tail = 0
			lastAt = nil
			if len(p) > 4 {
				tail, _ = p[4].(float64)
			}
			if len(p) > 5 {
				lastAt = healthTime(p[5])
			}
		} else {
			status = carry
			end := stamp.Add(time.Duration(interval) * time.Second)
			if end.After(now) {
				end = now
			}
			if status != "healthy" && tail >= 2 && lastAt != nil && end.Sub(*lastAt) >= 3*time.Hour {
				status = "healthy"
			}
		}
		point := HealthPoint{Status: status, CheckedAt: stamp}
		if len(p) > 3 {
			if latency, ok := p[3].(float64); ok && latency >= 0 {
				v := int(latency)
				point.LatencyMS = &v
			}
		}
		out = append(out, point)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CheckedAt.Before(*out[j].CheckedAt) })
	if len(out) > 60 {
		out = out[len(out)-60:]
	}
	return out
}
func parseImageStatusHealth(payload any, site Site, now time.Time) []ModelHealth {
	data := dataRecord(payload)
	groups, _ := data["groups"].([]any)
	interval := 1200
	if n := firstNumber(data, []string{"slot_seconds"}); n != nil && *n > 0 && *n <= 86400 {
		interval = int(*n)
	}
	generated := healthTime(data["generated_at"])
	out := []ModelHealth{}
	for _, raw := range groups {
		g, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := groupID2(g)
		name := safeString(g, "name")
		gid := id
		if gid == "" {
			gid = mappedGroup(site, name)
		}
		h := ModelHealth{GroupID: gid, GroupName: name, Source: "sub2api_image_status", Scope: "group_model_reference", Mode: "image", Status: imageReportCurrent(g), CheckedAt: generated, CollectedAt: now, IntervalSeconds: 300, ObservationKind: "group_request_logs", Message: "上游生图分组调用统计，模型列表共享分组报告"}
		h.CurrentStatus = h.Status
		if gid == "" {
			h.Scope = "site_model"
		}
		if h.CheckedAt == nil {
			h.CheckedAt = healthTime(g["last_at"])
		}
		h.Timeline = imageReportTimeline(g, now, interval)
		if n := firstNumber(g, []string{"latency_ms"}); n != nil && *n >= 0 {
			v := int(*n)
			h.LatencyMS = &v
		}
		if a, ok := g["avail"].(map[string]any); ok {
			h.Availability = firstNumber(a, []string{"7d"})
		}
		seen := map[string]bool{}
		for _, model := range strings.Split(safeString(g, "model"), "/") {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			row := h
			row.ID = fmt.Sprintf("sub2api_image_status:%s:%s", id, model)
			row.Model = model
			// Current group status is computed from the report's latest request counts;
			// its historical bucket colors cannot override that published state.
			row = qualifyHealth(row)
			row.Status = h.Status
			out = append(out, row)
		}
	}
	return out
}
