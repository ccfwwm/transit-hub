package upstream

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// API routes may include a provider path prefix, but never credentials, queries,
// or fragments. The host bridge independently enforces the deployment policy.
func NormalizeRoutingURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("请填写不含凭据、查询参数的 API 地址")
	}
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}

func NormalizeUpstreamHost(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if strings.Contains(raw, "://") {
		u, e := url.Parse(raw)
		if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Port() != "" {
			return "", fmt.Errorf("请输入域名，不含端口或路径")
		}
		raw = u.Hostname()
	}
	raw = strings.TrimSuffix(raw, ".")
	if len(raw) > 253 || !strings.Contains(raw, ".") || net.ParseIP(raw) != nil {
		return "", fmt.Errorf("请输入有效公网域名")
	}
	for _, label := range strings.Split(raw, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("域名格式错误")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", fmt.Errorf("域名格式错误")
			}
		}
	}
	if strings.HasSuffix(raw, ".local") || strings.HasSuffix(raw, ".localhost") || strings.HasSuffix(raw, ".internal") {
		return "", fmt.Errorf("不允许内网域名")
	}
	return raw, nil
}
