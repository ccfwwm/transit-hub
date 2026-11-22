package upstream

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
)

// V1 details may contain additional models omitted from the list. Missing
// timestamps remain missing; a primary model's history is never copied.
func mergeV1Details(base []ModelHealth, payload any, monitorID string, now time.Time) []ModelHealth {
	data := dataRecord(payload)
	models, _ := data["models"].([]any)
	template := ModelHealth{Source: "sub2api_v1", Scope: "site_model", CollectedAt: now, GroupName: safeString(data, "group_name")}
	for _, h := range base {
		if h.ID == "sub2api_v1:"+monitorID {
			template = h
			break
		}
	}
	for _, raw := range models {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		model := safeString(m, "model")
		if model == "" {
			continue
		}
		present := false
		for _, h := range base {
			if h.Model == model && h.ID == "sub2api_v1:"+monitorID {
				present = true
				break
			}
		}
		if present {
			continue
		}
		h := template
		h.ID = "sub2api_v1:" + monitorID + ":" + model
		h.Model = model
		h.Status = healthStatus(safeString(m, "latest_status"))
		h.CheckedAt = healthTime(m["checked_at"])
		if h.CheckedAt == nil {
			h.CheckedAt = healthTime(m["latest_checked_at"])
		}
		h.Timeline = healthTimeline(m["timeline"])
		h.PingMS = nil
		h.LatencyMS = nil
		h.Throughput = firstNumber(m, []string{"tokens_per_second", "throughput_tps"})
		h.Availability = firstNumber(m, []string{"availability", "availability_7d"})
		if n := firstNumber(m, []string{"latest_latency_ms", "latency_ms"}); n != nil {
			v := int(*n)
			h.LatencyMS = &v
		}
		h.Message = "上游多模型检测结果；未提供独立检测时间时仅作参考"
		replaced := false
		for i, existing := range base {
			if existing.ID == h.ID {
				base[i] = qualifyHealth(h)
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, qualifyHealth(h))
		}
	}
	return base
}
func (s *HealthService) fetchV1Details(ctx context.Context, site Site, opts requestOptions, list any, base []ModelHealth) []ModelHealth {
	items, _ := dataRecord(list)["items"].([]any)
	path := healthPath(site, "sub2api_v1", "detail")
	if path == "" {
		return base
	}
	ids := []string{}
	for _, v := range items {
		if m, ok := v.(map[string]any); ok {
			id := groupID2(m)
			if _, e := strconv.ParseInt(id, 10, 64); e == nil {
				ids = append(ids, id)
			}
		}
	}
	type result struct {
		id      string
		payload any
	}
	results := make(chan result, len(ids))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			var raw []byte
			key := healthCacheKey(site, id)
			if s.db != nil && s.db.QueryRow(ctx, `SELECT payload FROM upstream_health_detail_cache WHERE site_id=$1 AND monitor_id=$2 AND updated_at>now()-interval '5 minutes'`, site.ID, key).Scan(&raw) == nil {
				var v any
				if json.Unmarshal(raw, &v) == nil {
					results <- result{id, v}
					return
				}
			}
			option := opts
			run, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			option.Context = run
			response, e := s.platform.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+expandHealthPath(path, "{id}", id), option)
			if e != nil {
				return
			}
			raw, _ = json.Marshal(response.Payload)
			if s.db != nil {
				_, _ = s.db.Exec(ctx, `INSERT INTO upstream_health_detail_cache VALUES($1,$2,$3,now()) ON CONFLICT(site_id,monitor_id) DO UPDATE SET payload=EXCLUDED.payload,updated_at=now()`, site.ID, key, raw)
			}
			results <- result{id, response.Payload}
		}(id)
	}
	wg.Wait()
	close(results)
	for r := range results {
		base = mergeV1Details(base, r.payload, r.id, time.Now().UTC())
	}
	return base
}
