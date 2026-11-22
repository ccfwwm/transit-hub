package upstream

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Match only the API origin and its configured base path. /v1 is the protocol
// suffix; other path prefixes remain significant for reverse proxies.
func MonitorAPIBase(raw string) string {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	path = strings.TrimSuffix(path, "/v1")
	path = strings.TrimSuffix(path, "/v1beta")
	return strings.ToLower(u.Scheme+"://"+u.Host) + path
}

// Related hosts only select candidates. A full Key match in the authenticated
// upstream account is still required; DNS resemblance never establishes a binding.
func MonitorAPICandidate(a, b string) bool {
	a, b = MonitorAPIBase(a), MonitorAPIBase(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	u, _ := url.Parse(a)
	v, _ := url.Parse(b)
	if u.Hostname() == v.Hostname() || u.EscapedPath() != v.EscapedPath() || net.ParseIP(u.Hostname()) != nil || net.ParseIP(v.Hostname()) != nil {
		return false
	}
	x, y := strings.Split(u.Hostname(), "."), strings.Split(v.Hostname(), ".")
	return len(x) >= 2 && len(y) >= 2 && strings.Join(x[len(x)-2:], ".") == strings.Join(y[len(y)-2:], ".")
}
func SameMonitorKey(platform Platform, a, b string) bool {
	clean := func(v string) string {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(strings.ToLower(v), "bearer ") {
			v = strings.TrimSpace(v[7:])
		}
		if platform == PlatformNewAPI {
			v = strings.TrimPrefix(v, "sk-")
		}
		return v
	}
	a, b = clean(a), clean(b)
	return a != "" && b != "" && !strings.Contains(a, "*") && !strings.Contains(b, "*") && a == b
}

// FindAPIKeyGroup never creates a token or calls a generation endpoint. Masked
// New API tokens are revealed through its existing read-key action before any
// comparison; neither names nor key prefixes are accepted as identity proof.
func (s *PlatformService) FindAPIKeyGroup(ctx context.Context, site Site, key string) (Sub2APIKeyItem, error) {
	if site.Session == nil {
		return Sub2APIKeyItem{}, fmt.Errorf("上游尚未登录")
	}
	session := *site.Session
	seen := map[string]bool{}
	for page := 1; page <= 1000; page++ {
		path := "/api/v1/keys?page=" + strconv.Itoa(page) + "&page_size=100"
		opts := requestOptions{AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS, Context: ctx}
		if session.Platform == PlatformNewAPI {
			path = "/api/token/?p=" + strconv.Itoa(page) + "&page_size=100"
			opts = newAPIRequestOptions(session)
			opts.Context = ctx
		}
		response, e := s.httpClient.requestJSON(session.BaseURL+path, opts)
		if e != nil {
			return Sub2APIKeyItem{}, fmt.Errorf("无法读取上游 Key 列表")
		}
		items := dataArray(response.Payload)
		total, hasTotal := paginationTotal(response.Payload)
		added := 0
		for _, raw := range items {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := groupID2(m)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			added++
			value := safeString(m, "key")
			if value == "" {
				if v := firstString(m, []string{"token", "api_key"}); v != nil {
					value = *v
				}
			}
			if session.Platform == PlatformNewAPI && !SameMonitorKey(session.Platform, value, key) {
				if !maskedKeyCouldMatch(value, key) {
					continue
				}
				r, e := s.httpClient.requestJSON(session.BaseURL+"/api/token/"+url.PathEscape(id)+"/key", func() requestOptions {
					o := newAPIRequestOptionsWith(session, http.MethodPost, nil)
					o.Context = ctx
					return o
				}())
				if e != nil {
					continue
				}
				value = safeString(dataRecord(r.Payload), "key")
			}
			if !SameMonitorKey(session.Platform, value, key) {
				continue
			}
			found := Sub2APIKeyItem{ID: id, Key: key, Name: safeString(m, "name")}
			if session.Platform == PlatformNewAPI {
				found.GroupID = safeString(m, "group")
				found.GroupName = found.GroupID
			} else {
				if n := firstNumber(m, []string{"group_id"}); n != nil {
					found.GroupID = strconv.FormatInt(int64(*n), 10)
				} else {
					found.GroupID = safeString(m, "group_id")
				}
				if g, ok := m["group"].(map[string]any); ok {
					if found.GroupID == "" {
						found.GroupID = groupID2(g)
					}
					found.GroupName = safeString(g, "name")
				}
			}
			if found.GroupID == "" || found.GroupID == "auto" {
				return found, fmt.Errorf("此 Key 没有固定分组，请明确选择上游分组")
			}
			return found, nil
		}
		if len(items) == 0 || hasTotal && len(seen) >= total {
			break
		}
		if added == 0 {
			return Sub2APIKeyItem{}, fmt.Errorf("上游 Key 分页重复，未能完成核对")
		}
	}
	return Sub2APIKeyItem{}, fmt.Errorf("未在已登录上游账号中找到完全匹配的 Key")
}
func maskedKeyCouldMatch(masked, key string) bool {
	masked = strings.TrimPrefix(strings.TrimSpace(masked), "sk-")
	key = strings.TrimPrefix(strings.TrimSpace(key), "sk-")
	if masked == "" {
		return true
	}
	first := strings.IndexByte(masked, '*')
	if first < 0 {
		return false
	}
	last := strings.LastIndexByte(masked, '*')
	return strings.HasPrefix(key, masked[:first]) && strings.HasSuffix(key, masked[last+1:])
}

func forwardingFields(record map[string]any) (base, key, platform string, groups []string) {
	platform = safeString(record, "platform")
	if c, ok := record["credentials"].(map[string]any); ok {
		base = safeString(c, "base_url")
		key = safeString(c, "api_key")
	}
	if values, ok := record["group_ids"].([]any); ok {
		for _, v := range values {
			if n, ok := v.(float64); ok {
				groups = append(groups, strconv.FormatInt(int64(n), 10))
			} else if s, ok := v.(string); ok {
				groups = append(groups, s)
			}
		}
	}
	if len(groups) == 0 {
		if values, ok := record["groups"].([]any); ok {
			for _, v := range values {
				if g, ok := v.(map[string]any); ok {
					if id := groupID2(g); id != "" {
						groups = append(groups, id)
					}
				}
			}
		}
	}
	return
}
func (s *PlatformService) FetchForwardingAccount(ctx context.Context, session Session, id string) (Sub2APIAdminAccountStatus, error) {
	r, e := s.httpClient.requestJSON(session.BaseURL+"/api/v1/admin/accounts/"+url.PathEscape(id), requestOptions{Context: ctx, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS})
	if e != nil {
		return Sub2APIAdminAccountStatus{}, fmt.Errorf("无法读取自有渠道凭据: %w", e)
	}
	record := dataRecord(r.Payload)
	if groupID2(record) != id {
		return Sub2APIAdminAccountStatus{}, fmt.Errorf("自有渠道身份不一致")
	}
	a := Sub2APIAdminAccountStatus{ID: id, Name: safeString(record, "name"), Type: safeString(record, "type")}
	a.BaseURL, a.APIKey, a.Platform, a.GroupIDs = forwardingFields(record)
	if a.APIKey == "" || strings.Contains(a.APIKey, "*") {
		exported, err := s.httpClient.requestJSON(session.BaseURL+"/api/v1/admin/accounts/data?ids="+url.QueryEscape(id)+"&include_proxies=false", requestOptions{Context: ctx, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS})
		if err != nil {
			return a, fmt.Errorf("自有站隐藏渠道 Key，当前会话无权读取；可通过 API 和 Key 手动接入")
		}
		items, _ := dataRecord(exported.Payload)["accounts"].([]any)
		if len(items) != 1 {
			return a, fmt.Errorf("无法确认自有渠道导出的唯一凭据")
		}
		if raw, ok := items[0].(map[string]any); ok {
			base, key, platform, _ := forwardingFields(raw)
			if MonitorAPIBase(base) != MonitorAPIBase(a.BaseURL) || platform != a.Platform {
				return a, fmt.Errorf("自有渠道导出信息不一致")
			}
			a.APIKey = key
		}
	}
	return a, nil
}
