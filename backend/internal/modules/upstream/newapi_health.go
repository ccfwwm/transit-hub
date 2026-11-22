package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

func unixHealthTime(v any) *time.Time {
	n, ok := v.(float64)
	if !ok || n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	t := time.Unix(int64(n), 0).UTC()
	return &t
}

// Pricing only identifies where a public model is offered. It never proves
// health, nor does it turn site-wide statistics into a particular Key's test.
type healthModelOffer struct {
	model, mode string
	groups      []string
}

func newAPIHealthOffers(payload any) []healthModelOffer {
	out := []healthModelOffer{}
	for _, raw := range dataArray(payload) {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		model := safeString(m, "model_name")
		if model == "" {
			continue
		}
		mode := safeString(m, "billing_capability")
		if mode == "" {
			mode = "unknown"
		}
		if endpoints, ok := m["supported_endpoint_types"].([]any); ok {
			for _, e := range endpoints {
				switch fmt.Sprint(e) {
				case "image-generation", "openai-image":
					mode = "image"
				case "openai-video":
					mode = "video"
				case "audio-speech", "openai-audio":
					mode = "audio"
				}
			}
		}
		groups := []string{}
		if values, ok := m["enable_groups"].([]any); ok {
			for _, g := range values {
				if v, ok := g.(string); ok && v != "" {
					groups = append(groups, v)
				}
			}
		}
		out = append(out, healthModelOffer{model: model, mode: mode, groups: groups})
	}
	return out
}
func offerHealth(h ModelHealth, site Site, groups []string) []ModelHealth {
	out := []ModelHealth{}
	seen := map[string]bool{}
	for _, name := range groups {
		gid := mappedGroup(site, name)
		if gid == "" || seen[gid] {
			continue
		}
		seen[gid] = true
		row := h
		row.GroupID = gid
		row.GroupName = name
		row.ID = h.ID + ":" + gid
		out = append(out, qualifyHealth(row))
	}
	if len(out) == 0 {
		out = append(out, qualifyHealth(h))
	}
	return out
}

func perfStatus(rate *float64) string {
	if rate == nil || *rate < 0 || *rate > 100 || math.IsNaN(*rate) || math.IsInf(*rate, 0) {
		return "unknown"
	}
	// Same public color bands used by New API's model performance page.
	if *rate < 70 {
		return "failed"
	}
	if *rate < 90 {
		return "warning"
	}
	return "healthy"
}
func parseNewAPIPerformance(payload, pricing any, site Site, now time.Time) []ModelHealth {
	data := dataRecord(payload)
	models, _ := data["models"].([]any)
	// Current New API returns requested models outside its default top-model
	// list in a separate model-keyed recent_success_series object.
	if recent, ok := data["recent_success_series"].(map[string]any); ok {
		byModel := map[string]map[string]any{}
		for _, raw := range models {
			if m, ok := raw.(map[string]any); ok {
				copy := map[string]any{}
				for k, v := range m {
					copy[k] = v
				}
				byModel[safeString(m, "model_name")] = copy
			}
		}
		for name, series := range recent {
			if name == "" {
				continue
			}
			m := byModel[name]
			if m == nil {
				m = map[string]any{"model_name": name}
				byModel[name] = m
			}
			m["recent_success_series"] = series
		}
		models = nil
		names := []string{}
		for name := range byModel {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			models = append(models, byModel[name])
		}
	}
	offers := map[string]healthModelOffer{}
	for _, o := range newAPIHealthOffers(pricing) {
		offers[o.model] = o
	}
	out := []ModelHealth{}
	for _, raw := range models {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		model := safeString(m, "model_name")
		if model == "" {
			continue
		}
		h := ModelHealth{ID: "newapi_perf:" + model, Model: model, Source: "newapi_perf", Scope: "model_reference", Mode: "unknown", CollectedAt: now, Availability: firstNumber(m, []string{"success_rate"}), Throughput: firstNumber(m, []string{"avg_tps"}), Timeline: []HealthPoint{}, ObservationKind: "request_statistics", Message: "上游逐模型调用统计（跨分组），不是指定 Key 的主动检测"}
		// The last returned point determines current state; the window rate only describes history.
		h.Status = "unknown"
		for _, key := range []string{"latest_status", "last_status", "current_status"} {
			if value := safeString(m, key); value != "" {
				h.Status = healthStatus(value)
				break
			}
		}
		if n := firstNumber(m, []string{"avg_latency_ms"}); n != nil && *n > 0 {
			v := int(*n)
			h.LatencyMS = &v
		}
		points, _ := m["recent_success_series"].([]any)
		for _, raw := range points {
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			stamp := unixHealthTime(p["ts"])
			if stamp == nil {
				continue
			}
			point := HealthPoint{Status: perfStatus(firstNumber(p, []string{"success_rate"})), CheckedAt: stamp, Throughput: firstNumber(p, []string{"avg_tps"})}
			if n := firstNumber(p, []string{"avg_latency_ms"}); n != nil && *n > 0 {
				v := int(*n)
				point.LatencyMS = &v
			}
			h.Timeline = append(h.Timeline, point)
		}
		sort.SliceStable(h.Timeline, func(i, j int) bool { return h.Timeline[i].CheckedAt.Before(*h.Timeline[j].CheckedAt) })
		if len(h.Timeline) > 60 {
			h.Timeline = h.Timeline[len(h.Timeline)-60:]
		}
		// Bucket timestamps describe statistics, not individual detection times.
		if len(h.Timeline) > 0 {
			h.Status = h.Timeline[len(h.Timeline)-1].Status
			h.CheckedAt = h.Timeline[len(h.Timeline)-1].CheckedAt
			h.IntervalSeconds = observedInterval(h.Timeline)
		}
		for _, key := range []string{"latest_checked_at", "last_checked_at", "last_observed_at"} {
			if stamp := healthTime(m[key]); stamp != nil {
				h.CheckedAt = stamp
				break
			}
		}
		if h.Status == "unknown" {
			h.Message = "上游尚无有效状态点"
		}
		o := offers[model]
		if o.mode != "" {
			h.Mode = o.mode
		}
		out = append(out, offerHealth(h, site, o.groups)...)
	}
	return out
}

func newAPIMonitorTimeline(raw any) ([]HealthPoint, *time.Time, int) {
	m, ok := raw.(map[string]any)
	if !ok {
		return []HealthPoint{}, nil, 0
	}
	start, end := unixHealthTime(m["started_at"]), unixHealthTime(m["ended_at"])
	interval := 0
	if n := firstNumber(m, []string{"bucket_seconds"}); n != nil && *n > 0 && *n <= 86400 {
		interval = int(*n)
	}
	statuses, _ := m["statuses"].([]any)
	points := []HealthPoint{}
	for i, v := range statuses {
		var stamp *time.Time
		if start != nil && interval > 0 {
			t := start.Add(time.Duration(i*interval) * time.Second)
			if end != nil && t.After(*end) {
				continue
			}
			stamp = &t
		}
		points = append(points, HealthPoint{Status: healthStatus(fmt.Sprint(v)), CheckedAt: stamp})
	}
	if len(points) > 60 {
		points = points[len(points)-60:]
	}
	return points, end, interval
}
func parseNewAPIChannelMonitors(payload, pricing any, site Site, now time.Time) []ModelHealth {
	data := dataRecord(payload)
	summary, ok := data["summary"].(map[string]any)
	if !ok || summary["enabled"] != true || summary["ready"] != true {
		return []ModelHealth{}
	}
	offers := map[string]healthModelOffer{}
	for _, o := range newAPIHealthOffers(pricing) {
		offers[o.model] = o
	}
	items, _ := data["items"].([]any)
	out := []ModelHealth{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		category := safeString(item, "category")
		routes, _ := item["routes"].([]any)
		for _, raw := range routes {
			route, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name := safeString(route, "name")
			if name == "" {
				continue
			}
			h := ModelHealth{ID: "newapi_monitors:" + category + ":" + safeString(item, "name") + ":" + name, Model: name, Source: "newapi_monitors", Scope: "model_reference", Mode: category, Status: healthStatus(safeString(route, "latest_status")), CollectedAt: now, ObservationKind: "channel_samples", Message: "上游模型线路采样，未限定当前 Key 或具体分组"}
			h.Timeline, h.CheckedAt, h.IntervalSeconds = newAPIMonitorTimeline(route["timeline"])
			if pace, ok := route["pace"].(map[string]any); ok {
				if n := firstNumber(pace, []string{"ms"}); n != nil && *n > 0 {
					v := int(*n)
					h.LatencyMS = &v
				}
				h.ObservationKind = "channel_samples:" + safeString(pace, "kind")
			}
			if category == "text" {
				h.GroupName = name
				h.GroupID = mappedGroup(site, name)
				h.Scope = "channel_reference"
				h.Message = "上游文本线路状态，不代表该线路下每个模型均已检测"
				out = append(out, qualifyHealth(h))
				continue
			}
			o := offers[name]
			out = append(out, offerHealth(h, site, o.groups)...)
		}
	}
	return out
}

func (s *HealthService) newAPIHealthPricing(ctx context.Context, site Site, opts requestOptions) any {
	profile, _ := normalizeMonitorProfile(site.Settings.MonitorProfile)
	source := profile.Template
	if source == "auto" {
		source = "newapi_perf"
	}
	path := healthPath(site, source, "pricing")
	if path == "" {
		return nil
	}
	key := healthCacheKey(site, "newapi_pricing")
	var raw []byte
	if s.db != nil && s.db.QueryRow(ctx, `SELECT payload FROM upstream_health_detail_cache WHERE site_id=$1 AND monitor_id=$2 AND updated_at>now()-interval '5 minutes'`, site.ID, key).Scan(&raw) == nil {
		var payload any
		if json.Unmarshal(raw, &payload) == nil {
			return payload
		}
	}
	r, err := s.platform.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+path, opts)
	if err != nil {
		return nil
	}
	raw, _ = json.Marshal(r.Payload)
	if s.db != nil {
		_, _ = s.db.Exec(ctx, `INSERT INTO upstream_health_detail_cache VALUES($1,$2,$3,now()) ON CONFLICT(site_id,monitor_id) DO UPDATE SET payload=EXCLUDED.payload,updated_at=now()`, site.ID, key, raw)
	}
	return r.Payload
}
func (s *HealthService) fetchNewAPIModels(ctx context.Context, site Site, opts requestOptions, source string) ([]ModelHealth, error) {
	path := "/api/perf-metrics/summary?hours=24"
	if source == "newapi_monitors" {
		path = "/api/channel-monitors"
	}
	if source == "newapi_service" {
		path = "/api/user/channel-monitor/status"
	}
	if configured := healthPath(site, source, ""); configured != "" {
		path = configured
	}
	r, err := s.platform.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+path, opts)
	if err != nil {
		return nil, err
	}
	pricing := s.newAPIHealthPricing(ctx, site, opts)
	if source == "newapi_service" {
		return parseNewAPIServiceStatus(r.Payload, pricing, site, time.Now().UTC()), nil
	}
	if source == "newapi_monitors" {
		return parseNewAPIChannelMonitors(r.Payload, pricing, site, time.Now().UTC()), nil
	}
	r.Payload = s.fetchNewAPIRecentModels(ctx, site, opts, path, r.Payload, pricing)
	base := parseNewAPIPerformance(r.Payload, pricing, site, time.Now().UTC())
	return s.fetchNewAPIPerformanceDetails(ctx, site, opts, r.Payload, pricing, base), nil
}

func (s *HealthService) fetchNewAPIRecentModels(ctx context.Context, site Site, opts requestOptions, path string, payload, pricing any) any {
	offers := newAPIHealthOffers(pricing)
	if len(offers) == 0 {
		return payload
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	type batchResult struct{ payload any }
	results := make(chan batchResult, (len(offers)+39)/40)
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for start := 0; start < len(offers); start += 40 {
		end := start + 40
		if end > len(offers) {
			end = len(offers)
		}
		models := append([]healthModelOffer(nil), offers[start:end]...)
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			u, e := url.Parse(path)
			if e != nil {
				return
			}
			q := u.Query()
			q.Del("recent_model")
			for _, model := range models {
				q.Add("recent_model", model.model)
			}
			u.RawQuery = q.Encode()
			option := opts
			option.Context = ctx
			r, e := s.platform.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+u.String(), option)
			if e == nil {
				results <- batchResult{r.Payload}
			}
		}()
	}
	wg.Wait()
	close(results)
	data := dataRecord(payload)
	merged := map[string]any{}
	for k, v := range data {
		merged[k] = v
	}
	recent := map[string]any{}
	if original, ok := data["recent_success_series"].(map[string]any); ok {
		for k, v := range original {
			recent[k] = v
		}
	}
	allModels := map[string]any{}
	collect := func(payload any) {
		rows, _ := dataRecord(payload)["models"].([]any)
		for _, raw := range rows {
			if m, ok := raw.(map[string]any); ok {
				if name := safeString(m, "model_name"); name != "" {
					allModels[name] = m
				}
			}
		}
	}
	collect(payload)
	for result := range results {
		collect(result.payload)
		if rows, ok := dataRecord(result.payload)["recent_success_series"].(map[string]any); ok {
			for model, points := range rows {
				recent[model] = points
			}
		}
	}
	merged["recent_success_series"] = recent
	names := make([]string, 0, len(allModels))
	for name := range allModels {
		names = append(names, name)
	}
	sort.Strings(names)
	models := make([]any, 0, len(names))
	for _, name := range names {
		models = append(models, allModels[name])
	}
	known := map[string]bool{}
	for _, raw := range models {
		if m, ok := raw.(map[string]any); ok {
			known[safeString(m, "model_name")] = true
		}
	}
	for _, offer := range offers {
		if _, ok := recent[offer.model]; ok && !known[offer.model] {
			models = append(models, map[string]any{"model_name": offer.model})
			known[offer.model] = true
		}
	}
	merged["models"] = models
	return map[string]any{"data": merged}
}

func mergeNewAPIPerformanceDetails(base []ModelHealth, payload any, site Site, pricing any) []ModelHealth {
	data := dataRecord(payload)
	model := safeString(data, "model_name")
	groups, _ := data["groups"].([]any)
	if model == "" || len(groups) == 0 {
		return base
	}
	mode := "unknown"
	for _, o := range newAPIHealthOffers(pricing) {
		if o.model == model {
			mode = o.mode
			break
		}
	}
	exact := []ModelHealth{}
	for _, raw := range groups {
		g, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := safeString(g, "group")
		gid := mappedGroup(site, name)
		if gid == "" {
			continue
		}
		m := map[string]any{}
		for k, v := range g {
			m[k] = v
		}
		m["model_name"] = model
		m["recent_success_series"] = g["series"]
		rows := parseNewAPIPerformance(map[string]any{"models": []any{m}}, nil, site, time.Now().UTC())
		if len(rows) != 1 {
			continue
		}
		h := rows[0]
		h.ID = "newapi_perf:" + model + ":" + gid
		h.GroupID = gid
		h.GroupName = name
		h.Mode = mode
		h.Scope = "group_model_reference"
		h.Message = "上游该分组和模型的调用统计，不是指定 Key 的主动检测"
		exact = append(exact, qualifyHealth(h))
	}
	if len(exact) == 0 {
		return base
	}
	// A precise group result replaces the displayed aggregate for that group.
	out := []ModelHealth{}
	for _, h := range base {
		replace := false
		for _, v := range exact {
			if h.Model == v.Model && h.GroupID == v.GroupID {
				replace = true
				break
			}
		}
		if !replace {
			out = append(out, h)
		}
	}
	return append(out, exact...)
}
func (s *HealthService) fetchNewAPIPerformanceDetails(ctx context.Context, site Site, opts requestOptions, summary, pricing any, base []ModelHealth) []ModelHealth {
	path := healthPath(site, "newapi_perf", "detail")
	if path == "" {
		return base
	}
	// Detail enrichment has its own budget so the parent can still publish the
	// complete summary when a large site's slow details exceed the budget.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	models, _ := dataRecord(summary)["models"].([]any)
	type result struct{ payload any }
	results := make(chan result, len(models))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, raw := range models {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		model := safeString(m, "model_name")
		if model == "" {
			continue
		}
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			key := healthCacheKey(site, "newapi_perf:"+model)
			var raw []byte
			if s.db != nil && s.db.QueryRow(ctx, `SELECT payload FROM upstream_health_detail_cache WHERE site_id=$1 AND monitor_id=$2 AND updated_at>now()-interval '5 minutes'`, site.ID, key).Scan(&raw) == nil {
				var payload any
				if json.Unmarshal(raw, &payload) == nil {
					results <- result{payload}
					return
				}
			}
			run, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			options := opts
			options.Context = run
			r, err := s.platform.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+expandHealthPath(path, "{model}", model), options)
			if err != nil {
				return
			}
			if safeString(dataRecord(r.Payload), "model_name") != model {
				return
			}
			raw, _ = json.Marshal(r.Payload)
			if s.db != nil {
				_, _ = s.db.Exec(ctx, `INSERT INTO upstream_health_detail_cache VALUES($1,$2,$3,now()) ON CONFLICT(site_id,monitor_id) DO UPDATE SET payload=EXCLUDED.payload,updated_at=now()`, site.ID, key, raw)
			}
			results <- result{r.Payload}
		}(model)
	}
	wg.Wait()
	close(results)
	for r := range results {
		base = mergeNewAPIPerformanceDetails(base, r.payload, site, pricing)
	}
	return base
}
