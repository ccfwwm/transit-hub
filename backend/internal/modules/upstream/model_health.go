package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ModelHealth struct {
	CurrentStatus   string        `json:"currentStatus,omitempty"`
	ProfileHash     string        `json:"profileHash,omitempty"`
	Timeline        []HealthPoint `json:"timeline"`
	Availability    *float64      `json:"availability"`
	Throughput      *float64      `json:"throughput"`
	ObservationKind string        `json:"observationKind,omitempty"`
	ID              string        `json:"id"`
	Model           string        `json:"model"`
	GroupID         string        `json:"groupId"`
	GroupName       string        `json:"groupName"`
	Source          string        `json:"source"`
	Scope           string        `json:"scope"`
	Status          string        `json:"status"`
	Mode            string        `json:"mode"`
	CheckedAt       *time.Time    `json:"checkedAt"`
	CollectedAt     time.Time     `json:"collectedAt"`
	LatencyMS       *int          `json:"latencyMs"`
	PingMS          *int          `json:"pingMs,omitempty"`
	Message         string        `json:"message"`
	Fresh           bool          `json:"fresh"`
	Usable          bool          `json:"usable"`
	IntervalSeconds int           `json:"intervalSeconds"`
}
type HealthPoint struct {
	Status     string     `json:"status"`
	CheckedAt  *time.Time `json:"checkedAt"`
	LatencyMS  *int       `json:"latencyMs"`
	Throughput *float64   `json:"throughput"`
	PingMS     *int       `json:"pingMs,omitempty"`
}

func healthTimeline(raw any) []HealthPoint {
	items, _ := raw.([]any)
	points := []HealthPoint{}
	for _, value := range items {
		m, ok := value.(map[string]any)
		if !ok {
			continue
		}
		p := HealthPoint{Status: healthStatus(safeString(m, "status")), CheckedAt: healthTime(m["checked_at"]), Throughput: firstNumber(m, []string{"tokens_per_second", "throughput_tps", "tps"})}
		if n := firstNumber(m, []string{"latency_ms"}); n != nil {
			v := int(*n)
			p.LatencyMS = &v
		}
		if n := firstNumber(m, []string{"ping_latency_ms"}); n != nil {
			v := int(*n)
			p.PingMS = &v
		}
		points = append(points, p)
	}
	sort.SliceStable(points, func(i, j int) bool {
		if points[i].CheckedAt == nil {
			return points[j].CheckedAt != nil
		}
		if points[j].CheckedAt == nil {
			return false
		}
		return points[i].CheckedAt.Before(*points[j].CheckedAt)
	})
	if len(points) > 60 {
		points = points[len(points)-60:]
	}
	return points
}

func observedInterval(points []HealthPoint) int {
	intervals := []int{}
	for i := 1; i < len(points); i++ {
		if points[i].CheckedAt != nil && points[i-1].CheckedAt != nil {
			d := int(points[i].CheckedAt.Sub(*points[i-1].CheckedAt).Seconds())
			if d > 0 {
				intervals = append(intervals, d)
			}
		}
	}
	if len(intervals) < 2 {
		return 0
	}
	sort.Ints(intervals)
	return intervals[len(intervals)/2]
}

type HealthCapability struct {
	ProfileHash string    `json:"profileHash,omitempty"`
	Source      string    `json:"source"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	CheckedAt   time.Time `json:"checkedAt"`
	NextProbe   time.Time `json:"nextProbe"`
}
type HealthPage struct {
	Items        []ModelHealth      `json:"items"`
	Total        int                `json:"total"`
	Page         int                `json:"page"`
	PageSize     int                `json:"pageSize"`
	Capabilities []HealthCapability `json:"capabilities"`
	UpdatedAt    *time.Time         `json:"updatedAt"`
}
type HealthMapping struct {
	SourceID string `json:"sourceId"`
	GroupID  string `json:"groupId"`
	Model    string `json:"model"`
}
type HealthService struct {
	AfterRefresh        func(context.Context, string, string, string)
	AfterBoundModelTest func(context.Context, BoundModelTest) error
	db                  *pgxpool.Pool
	sites               *Service
	platform            *PlatformService
	cipher              *SiteCredentialCipher
}

func NewHealthService(db *pgxpool.Pool, sites *Service, key string) (*HealthService, error) {
	var cipher *SiteCredentialCipher
	var err error
	if strings.TrimSpace(key) != "" {
		cipher, err = NewSiteCredentialCipher(key)
		if err != nil {
			return nil, err
		}
	}
	return &HealthService{db: db, sites: sites, platform: sites.platformService, cipher: cipher}, nil
}
func (s *HealthService) EnsureSchema(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS upstream_health_detail_cache(site_id text NOT NULL,monitor_id text NOT NULL,payload jsonb NOT NULL,updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(site_id,monitor_id));
 CREATE TABLE IF NOT EXISTS upstream_model_health(site_id text NOT NULL,user_id text NOT NULL,admin_account_id text NOT NULL,source_id text NOT NULL,model text NOT NULL,group_id text NOT NULL,source text NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(site_id,source_id));
 CREATE INDEX IF NOT EXISTS idx_upstream_health_lookup ON upstream_model_health(site_id,group_id,model);
 CREATE TABLE IF NOT EXISTS upstream_health_capabilities(site_id text NOT NULL,source text NOT NULL,payload jsonb NOT NULL,next_probe timestamptz NOT NULL,PRIMARY KEY(site_id,source));
 CREATE TABLE IF NOT EXISTS upstream_health_mappings(site_id text NOT NULL,source_id text NOT NULL,group_id text NOT NULL,model text NOT NULL,PRIMARY KEY(site_id,source_id));
 CREATE TABLE IF NOT EXISTS upstream_health_previews(id text PRIMARY KEY,user_id text NOT NULL,admin_account_id text NOT NULL,encrypted text NOT NULL,payload jsonb NOT NULL DEFAULT '{}',expires_at timestamptz NOT NULL DEFAULT now()+interval '10 minutes');`)
	return err
}
func healthStatus(raw string) string {
	switch strings.ToLower(raw) {
	case "operational", "healthy", "available", "up", "1":
		return "healthy"
	case "down", "outage", "major_outage", "critical", "unavailable", "failed", "error", "0":
		return "failed"
	case "degraded", "warning", "partial_outage":
		return "warning"
	}
	return "unknown"
}
func healthString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func healthTime(v any) *time.Time {
	s := healthString(v)
	if s == "" {
		return nil
	}
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return nil
	}
	return &t
}
func healthFresh(h ModelHealth, now time.Time) bool {
	window := 10 * time.Minute
	if h.IntervalSeconds > 0 {
		window = time.Duration(h.IntervalSeconds*2) * time.Second
		if window > 30*time.Minute {
			window = 30 * time.Minute
		}
	}
	return h.CheckedAt != nil && !h.CheckedAt.After(now.Add(time.Minute)) && now.Sub(*h.CheckedAt) <= window
}
func qualifyHealth(h ModelHealth) ModelHealth {
	if h.CurrentStatus != "" {
		h.Status = healthStatus(h.CurrentStatus)
	} else if len(h.Timeline) > 0 {
		last := h.Timeline[len(h.Timeline)-1]
		if (h.CheckedAt == nil || last.CheckedAt == nil || !h.CheckedAt.After(*last.CheckedAt)) && last.Status != "unknown" {
			h.Status = last.Status
		}
	}

	h.Fresh = healthFresh(h, time.Now())
	h.Usable = h.Fresh && h.Scope == "group_model" && h.GroupID != "" && (h.Status == "healthy" || h.Status == "failed")
	return h
}

func v2Point(m map[string]any, checkedAt *time.Time) HealthPoint {
	health, _ := m["health"].(map[string]any)
	metrics, _ := m["metrics"].(map[string]any)
	p := HealthPoint{Status: healthStatus(safeString(health, "overall")), CheckedAt: checkedAt, Throughput: firstNumber(metrics, []string{"tokens_per_second", "throughput_tps", "tps"})}
	if metrics["has_requests"] == false {
		p.Status = "unknown"
	}
	if ttft, ok := metrics["ttft"].(map[string]any); ok {
		if n := firstNumber(ttft, []string{"trimmed_avg_ms", "avg_ms", "p50_ms"}); n != nil {
			v := int(*n)
			p.LatencyMS = &v
		}
	}
	return p
}
func mappedGroup(site Site, name string) string {
	id := ""
	count := 0
	for _, g := range site.Metrics.Groups {
		if strings.TrimSpace(g.Name) == strings.TrimSpace(name) && strings.TrimSpace(name) != "" {
			id = g.ID
			count++
		}
	}
	if count == 1 {
		return id
	}
	return ""
}

// Some upstreams publish a family label rather than their real group name.
// A unique platform/rate match is display-only and never dispatch evidence.
func referenceGroup(site Site, monitor map[string]any) string {
	rates, _ := monitor["group_rate_multipliers"].([]any)
	provider := strings.ToLower(safeString(monitor, "provider"))
	if len(rates) != 1 || provider == "" {
		return ""
	}
	rate, ok := rates[0].(float64)
	if !ok {
		return ""
	}
	ids := []string{}
	for _, g := range site.Metrics.Groups {
		if g.Platform == nil || g.Multiplier == nil || strings.ToLower(*g.Platform) != provider {
			continue
		}
		delta := *g.Multiplier - rate
		if delta > -0.000001 && delta < 0.000001 {
			ids = append(ids, g.ID)
		}
	}
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}
func parseV1Health(payload any, site Site, now time.Time) []ModelHealth {
	data := dataRecord(payload)
	items, _ := data["items"].([]any)
	out := []ModelHealth{}
	for _, v := range items {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		name := safeString(m, "group_name")
		gid := mappedGroup(site, name)
		if gid == "" {
			gid = mappedGroup(site, safeString(m, "name"))
		}
		base := ModelHealth{ID: "sub2api_v1:" + groupID2(m), Model: safeString(m, "primary_model"), GroupID: gid, GroupName: name, Source: "sub2api_v1", Scope: "site_model", Status: healthStatus(safeString(m, "primary_status")), CollectedAt: now, Mode: "unknown"}
		base.Timeline = healthTimeline(m["timeline"])
		base.Availability = firstNumber(m, []string{"availability", "availability_1h", "availability_7d", "uptime_percent"})
		base.CheckedAt = healthTime(m["primary_checked_at"])
		if n := firstNumber(m, []string{"primary_ping_latency_ms"}); n != nil {
			v := int(*n)
			base.PingMS = &v
		}
		base.ObservationKind = safeString(m, "status_source")
		base.Throughput = firstNumber(m, []string{"primary_tokens_per_second", "primary_throughput_tps", "tokens_per_second", "throughput_tps", "tps"})
		if gid != "" {
			base.Scope = "group_model"
		}
		if gid == "" {
			if ref := referenceGroup(site, m); ref != "" {
				base.GroupID = ref
				base.Message = "按类型与倍率唯一关联的参考状态；上游未提供准确分组标识"
			}
		}
		if base.ObservationKind == "request_logs" && safeString(m, "metrics_scope") == "group" {
			base.Scope = "group_model_reference"
			base.Message = "上游分组调用统计，不代表特定模型或 Key 的直连检测"
		}
		if n := firstNumber(m, []string{"primary_latency_ms"}); n != nil {
			x := int(*n)
			base.LatencyMS = &x
		}
		if n := firstNumber(m, []string{"interval_seconds"}); n != nil {
			base.IntervalSeconds = int(*n)
		}
		if base.IntervalSeconds <= 0 {
			base.IntervalSeconds = observedInterval(base.Timeline)
		}
		if timeline, ok := m["timeline"].([]any); ok {
			for _, point := range timeline {
				if p, ok := point.(map[string]any); ok {
					t := healthTime(p["checked_at"])
					if t != nil && (base.CheckedAt == nil || t.After(*base.CheckedAt)) {
						base.CheckedAt = t
						base.Status = healthStatus(safeString(p, "status"))
						if n := firstNumber(p, []string{"latency_ms"}); n != nil {
							x := int(*n)
							base.LatencyMS = &x
						}
					}
				}
			}
		}
		if base.Model != "" {
			out = append(out, qualifyHealth(base))
		}
		if extras, ok := m["extra_models"].([]any); ok {
			for _, v := range extras {
				if e, ok := v.(map[string]any); ok {
					extra := base
					extra.Model = safeString(e, "model")
					extra.Status = healthStatus(safeString(e, "status"))
					extra.CheckedAt = healthTime(e["checked_at"])
					extra.Timeline = healthTimeline(e["timeline"])
					extra.Availability = firstNumber(e, []string{"availability_7d", "availability", "uptime_percent"})
					extra.Throughput = firstNumber(e, []string{"tokens_per_second", "throughput_tps", "tps"})
					extra.LatencyMS = nil
					if n := firstNumber(e, []string{"latency_ms"}); n != nil {
						v := int(*n)
						extra.LatencyMS = &v
					}
					extra.Message = "额外模型未提供独立检查时间时仅供参考"
					extra.ID = base.ID + ":" + extra.Model
					if extra.Model != "" {
						out = append(out, qualifyHealth(extra))
					}
				}
			}
		}
	}
	return out
}
func parseV2Health(payload any, site Site, now time.Time) []ModelHealth {
	data := dataRecord(payload)
	items, _ := data["items"].([]any)
	out := []ModelHealth{}
	for _, v := range items {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		gid := healthString(m["group_id"])
		model := safeString(m, "model")
		if model == "" {
			continue
		}
		h := ModelHealth{ID: "sub2api_v2:" + gid + ":" + safeString(m, "platform") + ":" + model, Model: model, GroupID: gid, GroupName: safeString(m, "group_name"), Source: "sub2api_v2", Scope: "group_model", Status: "unknown", Mode: "unknown", CollectedAt: now, Message: "上游分组的真实调用统计，并非指定 Key 直连检测"}
		if gid == "" {
			h.Scope = "site_model"
		}
		if health, ok := m["health"].(map[string]any); ok {
			h.Status = healthStatus(safeString(health, "overall"))
		}
		latest := v2Point(m, nil)
		h.LatencyMS = latest.LatencyMS
		h.Throughput = latest.Throughput
		if buckets, ok := m["buckets"].([]any); ok {
			for _, v := range buckets {
				if b, ok := v.(map[string]any); ok {
					metrics, _ := b["metrics"].(map[string]any)
					n := firstNumber(metrics, []string{"request_count"})
					t := healthTime(b["bucket_start"])
					if (n != nil && *n > 0 || metrics["has_requests"] == true) && t != nil && (h.CheckedAt == nil || t.After(*h.CheckedAt)) {
						h.CheckedAt = t
					}
					h.Timeline = append(h.Timeline, v2Point(b, t))
				}
			}
		}
		metrics, _ := m["metrics"].(map[string]any)
		if n := firstNumber(metrics, []string{"success_rate"}); n != nil {
			v := *n * 100
			h.Availability = &v
		}
		health, _ := m["health"].(map[string]any)
		count := firstNumber(metrics, []string{"request_count"})
		minimum := 1.0
		if n := firstNumber(health, []string{"minimum_sample"}); n != nil {
			minimum = *n
		}
		if count == nil || *count < minimum {
			h.Scope = "group_model_reference"
			h.Message = "上游报告的调用统计；样本计数不足或已隐藏，仅作参考，不自动判定 Key 可用"
			if metrics["has_requests"] != true {
				h.Status = "unknown"
			}
		}
		if model == "__other__" || model == "*" {
			h.Scope = "group_model_reference"
			h.Message = "上游合并模型统计，仅供参考"
		}
		sort.SliceStable(h.Timeline, func(i, j int) bool {
			if h.Timeline[i].CheckedAt == nil {
				return h.Timeline[j].CheckedAt != nil
			}
			if h.Timeline[j].CheckedAt == nil {
				return false
			}
			return h.Timeline[i].CheckedAt.Before(*h.Timeline[j].CheckedAt)
		})
		if len(h.Timeline) > 60 {
			h.Timeline = h.Timeline[len(h.Timeline)-60:]
		}
		out = append(out, qualifyHealth(h))
	}
	return out
}
func parseKumaHealth(payload any, now time.Time) []ModelHealth {
	out := []ModelHealth{}
	for i, v := range dataArray(payload) {
		g, ok := v.(map[string]any)
		if !ok {
			continue
		}
		monitors, _ := g["monitors"].([]any)
		for j, value := range monitors {
			m, ok := value.(map[string]any)
			if !ok {
				continue
			}
			h := ModelHealth{ID: fmt.Sprintf("newapi_kuma:%d:%d:%s", i, j, safeString(m, "name")), Model: safeString(m, "name"), GroupName: safeString(m, "group"), Source: "newapi_kuma", Scope: "infrastructure", Status: healthStatus(healthString(m["status"])), Mode: "unknown", CollectedAt: now, Message: "上游状态页参考，不能代替模型直连检测", Timeline: []HealthPoint{}}
			if n := firstNumber(m, []string{"uptime"}); n != nil {
				v := *n * 100
				h.Availability = &v
			}
			if model := safeString(m, "model"); model != "" {
				h.Model = model
				h.Scope = "model_reference"
			}
			if h.Scope == "infrastructure" {
				name := safeString(m, "name")
				if parts := strings.Split(name, " / "); len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
					h.Model = strings.TrimSpace(parts[1])
					h.Scope = "model_reference"
					if h.GroupName == "" {
						h.GroupName = strings.TrimSpace(parts[0])
					}
				}
				for _, marker := range []string{"模型id：", "模型ID：", "模型id:", "模型ID:"} {
					if parts := strings.SplitN(name, marker, 2); len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
						h.Model = strings.TrimSpace(parts[1])
						h.Scope = "model_reference"
						break
					}
				}
			}
			if h.Scope == "infrastructure" && (strings.HasPrefix(strings.ToLower(h.Model), "newapi-") || strings.Contains(h.GroupName, "渠道")) {
				h.Scope = "channel_reference"
			}
			heartbeats, _ := m["heartbeats"].([]any)
			for _, raw := range heartbeats {
				b, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				p := HealthPoint{Status: healthStatus(healthString(b["status"])), CheckedAt: healthTime(b["time"])}
				if p.CheckedAt == nil {
					p.CheckedAt = healthTime(b["timestamp"])
				}
				if n := firstNumber(b, []string{"ping", "latency_ms"}); n != nil {
					v := int(*n)
					p.LatencyMS = &v
				}
				h.Timeline = append(h.Timeline, p)
				if h.CheckedAt == nil {
					h.LatencyMS = p.LatencyMS
				}
				if p.CheckedAt != nil && (h.CheckedAt == nil || p.CheckedAt.After(*h.CheckedAt)) {
					h.CheckedAt = p.CheckedAt
					h.LatencyMS = p.LatencyMS
				}
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
			out = append(out, qualifyHealth(h))
		}
	}
	return out
}
func associateKumaGroups(items []ModelHealth, site Site) []ModelHealth {
	normalize := func(v string) string {
		v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "newapi-")
		return strings.ToLower(strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' || r == '_' || r == '　' {
				return -1
			}
			return r
		}, v))
	}
	for i := range items {
		h := &items[i]
		matches := []string{}
		for _, g := range site.Metrics.Groups {
			if normalize(h.Model) == normalize(g.Name) || normalize(h.GroupName) == normalize(g.Name) {
				matches = append(matches, g.ID)
			}
		}
		if len(matches) == 1 {
			h.GroupID = matches[0]
			if h.Scope == "infrastructure" {
				h.Scope = "channel_reference"
				h.Message = "上游渠道状态参考，不代表所有模型或指定 Key 可用"
			}
		}
	}
	return items
}
func (s *HealthService) fetch(ctx context.Context, site Site, source string) ([]ModelHealth, error) {
	if site.Session == nil {
		return nil, fmt.Errorf("未登录上游")
	}
	session := *site.Session
	opts := newAPIRequestOptions(session)
	opts.Context = ctx
	if site.Platform == PlatformSub2API {
		opts = requestOptions{AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: site.SkipTLSVerify || session.InsecureSkipTLS, Context: ctx}
	}
	path := "/api/v1/channel-monitors"
	if source == "sub2api_v2" {
		path = "/api/v1/channel-monitor-v2/matrix?range=90m&group_by=platform_group_model"
	}
	if source == "newapi_channels" {
		return s.fetchChannelReferences(ctx, site, opts)
	}
	if source == "newapi_perf" || source == "newapi_monitors" || source == "newapi_service" {
		return s.fetchNewAPIModels(ctx, site, opts, source)
	}
	if source == "newapi_kuma" {
		path = "/api/uptime/status"
	}
	if source == "sub2api_marketplace" {
		path = "/api/v1/marketplace/models"
	}
	if custom := healthPath(site, source, ""); custom != "" {
		path = custom
	}
	r, err := s.platform.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+path, opts)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	switch source {
	case "sub2api_plaza":
		return parseModelPlazaHealth(r.Payload, site, now), nil
	case "sub2api_image_status":
		return parseImageStatusHealth(r.Payload, site, now), nil
	case "sub2api_marketplace":
		return parseMarketplaceHealth(r.Payload, site, now), nil
	case "sub2api_v1":
		return s.fetchV1Details(ctx, site, opts, r.Payload, parseV1Health(r.Payload, site, now)), nil
	case "sub2api_v2":
		return parseV2Health(r.Payload, site, now), nil
	default:
		return associateKumaGroups(parseKumaHealth(r.Payload, now), site), nil
	}
}
func (s *HealthService) Refresh(ctx context.Context, site Site, force bool) error {
	if s.AfterRefresh != nil {
		defer s.AfterRefresh(ctx, site.UserID, site.AdminAccountID, site.ID)
	}
	sources := []string{"sub2api_image_status", "sub2api_v1", "sub2api_v2", "sub2api_marketplace", "sub2api_plaza"}
	if site.Platform == PlatformNewAPI {
		sources = []string{"newapi_monitors", "newapi_perf", "newapi_service", "newapi_kuma", "newapi_channels"}
	}
	profile, e := normalizeMonitorProfile(site.Settings.MonitorProfile)
	if e != nil {
		return e
	}
	profileHash := healthProfileHash(&profile)
	if profile.Template != "auto" {
		sources = []string{profile.Template}
	}
	for _, source := range sources {
		var next time.Time
		if !force && s.db.QueryRow(ctx, `SELECT next_probe FROM upstream_health_capabilities WHERE site_id=$1 AND source=$2 AND coalesce(payload->>'profileHash','')=$3`, site.ID, source, profileHash).Scan(&next) == nil && time.Now().Before(next) {
			continue
		}
		items, err := s.fetch(ctx, site, source)
		if re, ok := err.(*RequestError); ok && re.HTTPStatus == http.StatusUnauthorized {
			// Refresh is attempted once, only after an authentication response.
			if site.Session != nil {
				if session, e := s.platform.RefreshSession(*site.Session); e == nil {
					previous := *site.Session
					site.Session = &session
					s.sites.writeHealthSession(ctx, site, previous)
					items, err = s.fetch(ctx, site, source)
				}
				if re, ok := err.(*RequestError); ok && re.HTTPStatus == http.StatusUnauthorized {
					if _, attempted, e := s.sites.tryAutomaticRelogin(ctx, &site); attempted && e == nil {
						if current, e := s.sites.GetSite(ctx, site.ID); e == nil && current != nil {
							site = *current
							items, err = s.fetch(ctx, site, source)
						}
					}
				}
			}
		}
		if !strings.HasPrefix(site.ID, "preview_") && s.expectedHealthHash(ctx, site.UserID, site.AdminAccountID, site.ID) != profileHash {
			return nil
		}
		cap := HealthCapability{ProfileHash: profileHash, Source: source, Status: "available", CheckedAt: time.Now().UTC(), NextProbe: time.Now().Add(time.Minute)}
		if len(items) == 0 {
			cap.Status = "empty"
		}
		if err != nil {
			cap.Status = "unavailable"
			cap.Message = "上游状态读取失败"
			if e, ok := err.(*RequestError); ok {
				cap.Message = e.MessageKey
				if e.HTTPStatus == 403 {
					cap.Status = "forbidden"
				}
				if e.HTTPStatus == 401 {
					cap.Status = "unauthorized"
				}
				if e.HTTPStatus == 404 || e.HTTPStatus == 405 {
					cap.Status = "unsupported"
				}
			}
			cap.NextProbe = time.Now().Add(time.Minute)
			if re, ok := err.(*RequestError); ok && (re.HTTPStatus == 401 || re.HTTPStatus == 403 || re.HTTPStatus == 404 || re.HTTPStatus == 405) {
				cap.NextProbe = time.Now().Add(24 * time.Hour)
			}
		}
		b, _ := json.Marshal(cap)
		if _, e := s.db.Exec(ctx, `INSERT INTO upstream_health_capabilities VALUES($1,$2,$3,$4) ON CONFLICT(site_id,source) DO UPDATE SET payload=EXCLUDED.payload,next_probe=EXCLUDED.next_probe`, site.ID, source, b, cap.NextProbe); e != nil {
			return e
		}
		if err != nil {
			continue
		}
		tx, e := s.db.Begin(ctx)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM upstream_model_health WHERE site_id=$1 AND source=$2`, site.ID, source); e != nil {
			tx.Rollback(ctx)
			return e
		}
		for _, item := range items {
			item.ProfileHash = profileHash
			b, _ := json.Marshal(item)
			if _, e = tx.Exec(ctx, `INSERT INTO upstream_model_health VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(site_id,source_id) DO UPDATE SET payload=EXCLUDED.payload,model=EXCLUDED.model,group_id=EXCLUDED.group_id`, site.ID, site.UserID, site.AdminAccountID, item.ID, item.Model, item.GroupID, item.Source, b); e != nil {
				tx.Rollback(ctx)
				return e
			}
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) writeHealthSession(ctx context.Context, site Site, previous Session) {
	latest, e := s.cache.Get(ctx, site.ID)
	if e != nil || latest == nil || latest.UserID != site.UserID || latest.Session == nil {
		return
	}
	if latest.Session.BaseURL != previous.BaseURL || latest.Session.AccessToken != previous.AccessToken || latest.Session.RefreshToken != previous.RefreshToken {
		return
	}
	latest.Session = site.Session
	_ = s.cache.Set(ctx, latest)
	_ = s.saveSite(ctx, latest)
}
func (s *HealthService) Page(ctx context.Context, uid, aid, siteID string, q url.Values) (HealthPage, error) {
	profileHash := s.expectedHealthHash(ctx, uid, aid, siteID)
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("pageSize"))
	if size != 50 && size != 100 {
		size = 25
	}
	out := HealthPage{Items: []ModelHealth{}, Capabilities: []HealthCapability{}, Page: page, PageSize: size}
	rows, err := s.db.Query(ctx, `SELECT h.payload,m.group_id,m.model FROM upstream_model_health h LEFT JOIN upstream_health_mappings m ON m.site_id=h.site_id AND m.source_id=h.source_id WHERE h.site_id=$1 AND h.user_id=$2 AND h.admin_account_id=$3 ORDER BY h.group_id,h.model,h.source_id`, siteID, uid, aid)
	if err != nil {
		return out, err
	}
	all := []ModelHealth{}
	for rows.Next() {
		var raw []byte
		var gid, model *string
		if err = rows.Scan(&raw, &gid, &model); err != nil {
			rows.Close()
			return out, err
		}
		var v ModelHealth
		if err = json.Unmarshal(raw, &v); err != nil {
			rows.Close()
			return out, err
		}
		if v.ProfileHash != profileHash {
			continue
		}
		if gid != nil && model != nil {
			v.GroupID = *gid
			v.Model = *model
			if v.Scope != "infrastructure" && !strings.HasSuffix(v.Scope, "_reference") {
				v.Scope = "group_model"
			}
		}
		v = qualifyHealth(v)
		if out.UpdatedAt == nil || v.CollectedAt.After(*out.UpdatedAt) {
			t := v.CollectedAt
			out.UpdatedAt = &t
		}
		if g := q.Get("groupId"); g != "" && v.GroupID != g && !(v.GroupID == "" && q.Get("groupName") != "" && v.GroupName == q.Get("groupName")) && !(q.Get("includeReferences") == "1" && v.Source == "newapi_kuma" && v.GroupID == "" && v.Scope != "channel_reference") {
			continue
		}
		if st := q.Get("status"); st != "" && st != "all" && v.Status != st {
			continue
		}
		if mode := q.Get("mode"); mode != "" && mode != "all" && v.Mode != mode {
			continue
		}
		if search := strings.ToLower(q.Get("q")); search != "" && !strings.Contains(strings.ToLower(v.Model+" "+v.GroupName), search) {
			continue
		}
		all = append(all, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.Total = len(all)
	start := (page - 1) * size
	if start < len(all) {
		end := start + size
		if end > len(all) {
			end = len(all)
		}
		out.Items = all[start:end]
	}
	rows, err = s.db.Query(ctx, `SELECT payload FROM upstream_health_capabilities WHERE site_id=$1 ORDER BY source`, siteID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var c HealthCapability
		if rows.Scan(&raw) == nil && json.Unmarshal(raw, &c) == nil {
			if c.ProfileHash == profileHash {
				out.Capabilities = append(out.Capabilities, c)
			}
		}
	}
	return out, rows.Err()
}
func (s *HealthService) Match(ctx context.Context, siteID, gid, groupName, model string) (ModelHealth, bool) {
	return s.matchModel(ctx, siteID, gid, groupName, model, false)
}

// The configured monitoring model may explicitly use its exact group/model report.
// Channel/server references and unrelated model successes never affect dispatch.
func (s *HealthService) MatchConfigured(ctx context.Context, siteID, gid, groupName, model string) (ModelHealth, bool) {
	return s.matchModel(ctx, siteID, gid, groupName, model, true)
}
func (s *HealthService) matchModel(ctx context.Context, siteID, gid, groupName, model string, configured bool) (ModelHealth, bool) {
	profileHash := s.expectedHealthHash(ctx, "", "", siteID)
	rows, err := s.db.Query(ctx, `SELECT h.payload,m.group_id,m.model FROM upstream_model_health h LEFT JOIN upstream_health_mappings m ON m.site_id=h.site_id AND m.source_id=h.source_id WHERE h.site_id=$1 AND coalesce(m.model,h.model)=$2 ORDER BY h.source_id`, siteID, model)
	if err != nil {
		return ModelHealth{}, false
	}
	defer rows.Close()
	matches := []ModelHealth{}
	for rows.Next() {
		var raw []byte
		var h ModelHealth
		var mappedGID, mappedModel *string
		if rows.Scan(&raw, &mappedGID, &mappedModel) != nil || json.Unmarshal(raw, &h) != nil {
			continue
		}
		if h.ProfileHash != profileHash {
			continue
		}
		if mappedGID != nil && mappedModel != nil && h.Scope != "infrastructure" && !strings.HasSuffix(h.Scope, "_reference") {
			h.GroupID = *mappedGID
			h.Model = *mappedModel
			h.Scope = "group_model"
		}
		h = qualifyHealth(h)
		selectedReport := configured && h.Scope == "group_model_reference" && h.CheckedAt != nil && time.Since(h.CollectedAt) <= 10*time.Minute && (h.Status == "healthy" || h.Status == "failed")
		if h.GroupID == gid && (h.Usable || selectedReport) {
			matches = append(matches, h)
		}
	}
	if len(matches) == 0 {
		return ModelHealth{}, false
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].CheckedAt.After(*matches[j].CheckedAt) })
	return matches[0], true
}
func (s *HealthService) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			sites, err := s.sites.repository.ListSites(ctx)
			if err == nil {
				sem := make(chan struct{}, 4)
				done := make(chan struct{}, len(sites))
				for _, site := range sites {
					sem <- struct{}{}
					go func(site Site) {
						defer func() { <-sem; done <- struct{}{} }()
						run, cancel := context.WithTimeout(ctx, 20*time.Second)
						defer cancel()
						_ = s.Refresh(run, site, false)
					}(site)
				}
				for range sites {
					<-done
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *HealthService) fetchChannelReferences(ctx context.Context, site Site, opts requestOptions) ([]ModelHealth, error) {
	out := []ModelHealth{}
	now := time.Now().UTC()
	for page := 1; page <= 100; page++ {
		path := strings.ReplaceAll(healthPath(site, "newapi_channels", ""), "{page}", fmt.Sprint(page))
		r, e := s.platform.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+path, opts)
		if e != nil {
			return nil, e
		}
		items := dataArray(r.Payload)
		if len(items) == 0 {
			break
		}
		for _, raw := range items {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			test := safeString(m, "test_model")
			for _, model := range strings.Split(safeString(m, "models"), ",") {
				model = strings.TrimSpace(model)
				if model == "" {
					continue
				}
				h := ModelHealth{ID: "newapi_channels:" + groupID2(m) + ":" + model, Model: model, GroupName: safeString(m, "group"), GroupID: mappedGroup(site, safeString(m, "group")), Source: "newapi_channels", Scope: "channel_reference", Status: "unknown", CollectedAt: now, Mode: "unknown", Message: "管理员渠道配置，仅表示模型已配置；渠道启用或测试耗时不能证明该模型近期成功"}
				if test == model {
					if n := firstNumber(m, []string{"test_time"}); n != nil && *n > 0 {
						tm := time.Unix(int64(*n), 0)
						h.CheckedAt = &tm
					}
				}
				out = append(out, h)
			}
		}
		if total, ok := paginationTotal(r.Payload); ok && page*100 >= total {
			break
		}
		if len(items) < 100 {
			break
		}
	}
	return out, nil
}
