package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

type MonitorProfile struct {
	Template    string `json:"template"`
	Path        string `json:"path"`
	DetailPath  string `json:"detailPath,omitempty"`
	PricingPath string `json:"pricingPath,omitempty"`
}
type MonitorTemplate struct {
	MonitorProfile
	Name        string `json:"name"`
	Description string `json:"description"`
}

func monitorTemplates() []MonitorTemplate {
	return []MonitorTemplate{
		{MonitorProfile{Template: "auto"}, "自动识别", "尝试平台已支持的监控接口"},
		{MonitorProfile{Template: "sub2api_v1", Path: "/api/v1/channel-monitors", DetailPath: "/api/v1/channel-monitors/{id}/status"}, "sub2api 模型监控 V1", "主模型、附加模型和历史时间线"},
		{MonitorProfile{Template: "sub2api_image_status", Path: "/__akl-image-status.json"}, "生图分组调用统计（APIKL）", "groups、models 和时间桶；支持自定义报告路径"},
		{MonitorProfile{Template: "sub2api_v2", Path: "/api/v1/channel-monitor-v2/matrix?range=90m&group_by=platform_group_model"}, "sub2api 调用统计 V2", "分组与模型的调用健康统计"},
		{MonitorProfile{Template: "sub2api_marketplace", Path: "/api/v1/marketplace/models"}, "号池模型广场（安域）", "分组最近请求历史"},
		{MonitorProfile{Template: "sub2api_plaza", Path: "/api/v1/model-plaza"}, "模型广场（创想）", "分组和模型的 availability、performance"},
		{MonitorProfile{Template: "newapi_perf", Path: "/api/perf-metrics/summary?hours=24", DetailPath: "/api/perf-metrics?model={model}&hours=24", PricingPath: "/api/pricing"}, "New API 模型性能统计", "成功率、延迟、吞吐及分组明细"},
		{MonitorProfile{Template: "newapi_monitors", Path: "/api/channel-monitors", PricingPath: "/api/pricing"}, "New API 线路监控（沧元）", "routes、状态时间桶和生成耗时"},
		{MonitorProfile{Template: "newapi_service", Path: "/api/user/channel-monitor/status", PricingPath: "/api/pricing"}, "New API 服务状态（Pomo）", "服务组件与每日状态序列"},
		{MonitorProfile{Template: "newapi_kuma", Path: "/api/uptime/status"}, "Uptime Kuma 状态页", "monitors 与心跳历史，保留参考范围"},
		{MonitorProfile{Template: "newapi_channels", Path: "/api/channel/?p={page}&page_size=100"}, "New API 管理员渠道参考", "只读渠道配置，需要管理员权限"},
	}
}
func monitorTemplate(id string) (MonitorProfile, bool) {
	for _, t := range monitorTemplates() {
		if t.Template == id {
			return t.MonitorProfile, true
		}
	}
	return MonitorProfile{}, false
}
func normalizeMonitorProfile(p *MonitorProfile) (MonitorProfile, error) {
	if p == nil || p.Template == "" || p.Template == "auto" {
		return MonitorProfile{Template: "auto"}, nil
	}
	v := *p
	v.Template = strings.TrimSpace(v.Template)
	v.Path = strings.TrimSpace(v.Path)
	v.DetailPath = strings.TrimSpace(v.DetailPath)
	v.PricingPath = strings.TrimSpace(v.PricingPath)
	if _, ok := monitorTemplate(v.Template); !ok {
		return v, fmt.Errorf("不支持的监控返回格式")
	}
	if v.Path == "" {
		return v, fmt.Errorf("请填写监控路径")
	}
	for _, path := range []string{v.Path, v.DetailPath, v.PricingPath} {
		if path == "" {
			continue
		}
		if len(path) > 2048 {
			return v, fmt.Errorf("监控路径过长")
		}
		u, e := url.Parse(path)
		decoded, de := url.PathUnescape(path)
		if e != nil || de != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(decoded, "//") || strings.Contains(decoded, "\\") || u.IsAbs() || u.Host != "" || u.Fragment != "" || strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
			return v, fmt.Errorf("监控路径必须是当前上游内的相对路径，例如 /api/status")
		}
	}
	if v.DetailPath != "" {
		marker := "{id}"
		if v.Template == "newapi_perf" {
			marker = "{model}"
		} else if v.Template != "sub2api_v1" {
			return v, fmt.Errorf("此格式没有模型详情路径")
		}
		if !strings.Contains(v.DetailPath, marker) {
			return v, fmt.Errorf("详情路径需要包含 %s", marker)
		}
	}
	if v.PricingPath != "" && !strings.HasPrefix(v.Template, "newapi_") {
		return v, fmt.Errorf("此格式不需要模型目录路径")
	}
	return v, nil
}
func healthProfileHash(p *MonitorProfile) string {
	v, e := normalizeMonitorProfile(p)
	if e != nil {
		return "invalid"
	}
	if v.Template == "auto" {
		return ""
	}
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func healthPath(site Site, source, which string) string {
	p, _ := normalizeMonitorProfile(site.Settings.MonitorProfile)
	if p.Template == "auto" || p.Template != source {
		p, _ = monitorTemplate(source)
	}
	switch which {
	case "detail":
		return p.DetailPath
	case "pricing":
		return p.PricingPath
	default:
		return p.Path
	}
}
func healthCacheKey(site Site, key string) string {
	if hash := healthProfileHash(site.Settings.MonitorProfile); hash != "" {
		return hash + ":" + key
	}
	return key
}
func (s *HealthService) expectedHealthHash(ctx context.Context, uid, aid, siteID string) string {
	if strings.HasPrefix(siteID, "preview_") {
		if data, _, e := s.preview(ctx, uid, aid, siteID); e == nil {
			return healthProfileHash(data.Request.MonitorProfile)
		}
	}
	if s.sites == nil || s.sites.cache == nil {
		return ""
	}
	if site, e := s.sites.GetSite(ctx, siteID); e == nil && site != nil {
		return healthProfileHash(site.Settings.MonitorProfile)
	}
	return ""
}
func (s *HealthService) registerMonitorTemplates(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/upstream-monitor-templates", func(w http.ResponseWriter, r *http.Request) {
		if _, _, e := s.scope(r); e != nil {
			http.Error(w, "未登录", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(monitorTemplates())
	})
}

// Escape placeholders independently in the path and query portions.
func expandHealthPath(path, marker, value string) string {
	parts := strings.SplitN(path, "?", 2)
	parts[0] = strings.ReplaceAll(parts[0], marker, url.PathEscape(value))
	if len(parts) == 2 {
		parts[1] = strings.ReplaceAll(parts[1], marker, url.QueryEscape(value))
	}
	return strings.Join(parts, "?")
}
