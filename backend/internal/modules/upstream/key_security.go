package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Key inventory is authenticated and fully paginated. No secret is returned to
// the browser or job results; records stay inside the security worker.
func (s *PlatformService) SecurityKeys(ctx context.Context, session Session) ([]map[string]any, error) {
	out := []map[string]any{}
	seen := map[string]bool{}
	for page := 1; page <= 1000; page++ {
		path := "/api/v1/keys?page=" + strconv.Itoa(page) + "&page_size=100"
		opts := requestOptions{Context: ctx, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS}
		if session.Platform == PlatformNewAPI {
			path = "/api/token/?p=" + strconv.Itoa(page) + "&page_size=100"
			opts = newAPIRequestOptions(session)
			opts.Context = ctx
		}
		r, e := s.httpClient.requestJSON(session.BaseURL+path, opts)
		if e != nil {
			return nil, fmt.Errorf("无法读取上游 Key 列表")
		}
		if payload, ok := r.Payload.(map[string]any); ok {
			if success, ok := payload["success"].(bool); ok && !success {
				return nil, fmt.Errorf("上游拒绝读取 Key 列表")
			}
		}
		items := dataArray(r.Payload)
		added := 0
		for _, v := range items {
			if m, ok := v.(map[string]any); ok {
				id := groupID2(m)
				if id != "" && !seen[id] {
					seen[id] = true
					added++
					out = append(out, m)
				}
			}
		}
		total, hasTotal := paginationTotal(r.Payload)
		if len(items) == 0 || hasTotal && len(seen) >= total {
			return out, nil
		}
		if _, ok := r.Payload.([]any); ok {
			return out, nil
		}
		if payload, ok := r.Payload.(map[string]any); ok {
			if _, ok := payload["data"].([]any); ok {
				return out, nil
			}
		}
		if added == 0 {
			return nil, fmt.Errorf("上游 Key 分页重复，已停止")
		}
	}
	return nil, fmt.Errorf("上游 Key 数量超过读取上限")
}

func SecurityKeyID(m map[string]any) string { return groupID2(m) }

var ErrSecurityKeyMissing = errors.New("上游 Key 不存在")

func SecurityKeyGroup(p Platform, m map[string]any) string {
	if p == PlatformNewAPI {
		return safeString(m, "group")
	}
	if v := firstNumber(m, []string{"group_id"}); v != nil {
		return strconv.FormatInt(int64(*v), 10)
	}
	return safeString(m, "group_id")
}
func SecurityKeyGroupMatches(p Platform, m map[string]any, group string) bool {
	if p == PlatformNewAPI {
		if _, modern := m["group_config"]; modern {
			_, legacy := m["group"]
			if !legacy {
				return true
			}
		}
	}
	return SecurityKeyGroup(p, m) == group
}
func (s *PlatformService) ReadSecurityKey(ctx context.Context, session Session, id string) (map[string]any, []string, error) {
	m, ips, e := s.readKeyPolicy(ctx, session, id)
	if e != nil {
		return nil, nil, fmt.Errorf("%s: %w", securityRequestError("无法读取上游 Key 配置", e).Error(), e)
	}
	if session.Platform == PlatformNewAPI && (groupID2(m) == "" || groupID2(m) == "0") {
		return nil, nil, ErrSecurityKeyMissing
	}
	if groupID2(m) != id {
		return nil, nil, fmt.Errorf("上游 Key 身份不一致")
	}
	return m, ips, nil
}

func (p *KeyPolicies) VerifyExact(ctx context.Context, uid, aid string, site Site, key, group string) (bool, error) {
	if site.UserID != uid || site.AdminAccountID != aid || site.Session == nil {
		return false, fmt.Errorf("上游凭据不可用")
	}
	want, e := p.DesiredIPs(ctx, uid, aid, site, key, group)
	if e != nil {
		return false, e
	}
	_, ips, e := p.platform.ReadSecurityKey(ctx, *site.Session, key)
	if e != nil {
		return false, e
	}
	if e = p.observe(ctx, uid, aid, site.ID, key, ips); e != nil {
		return false, e
	}
	if len(want) == 0 {
		return len(ips) == 0, nil
	}
	got, e := NormalizeAllowedIPs(ips)
	if e != nil {
		return false, nil
	}
	return strings.Join(want, "\n") == strings.Join(got, "\n"), nil
}

// Rotation copies only documented writable fields. Old usage is deducted from
// a limited quota, so daily rotation cannot refill a token's spending budget.
func SecurityCloneConfig(platform Platform, old map[string]any, name string, ips []string) (map[string]any, error) {
	out := map[string]any{"name": name}
	fields := []string{"group_id", "ip_blacklist", "rate_limit_5h", "rate_limit_1d", "rate_limit_7d", "expires_at", "status"}
	if platform == PlatformNewAPI {
		fields = []string{"status", "expired_time", "remain_quota", "unlimited_quota", "model_limits_enabled", "model_limits", "group", "group_config", "preset_id", "sticky_session", "remark", "cross_group_retry", "auto_groups", "allow_user_quota_query"}
	}
	for _, k := range fields {
		if v, ok := old[k]; ok && v != nil {
			out[k] = v
		}
	}
	if platform == PlatformNewAPI {
		out["allow_ips"] = strings.Join(ips, "\n")
	} else {
		out["ip_whitelist"] = ips
		if q := firstNumber(old, []string{"quota"}); q != nil && *q > 0 {
			remain := *q
			if used := firstNumber(old, []string{"quota_used"}); used != nil {
				remain -= *used
			}
			if remain <= 0 {
				return nil, fmt.Errorf("Key 配额已耗尽，未创建可绕过配额的新 Key")
			}
			out["quota"] = remain
		} else {
			out["quota"] = 0
		}
	}
	_, hasGroupConfig := out["group_config"]
	if SecurityKeyGroup(platform, out) == "" && !(platform == PlatformNewAPI && hasGroupConfig) {
		return nil, fmt.Errorf("Key 分组配置不完整")
	}
	return out, nil
}

func securityRequestError(message string, e error) error {
	var req *RequestError
	if errors.As(e, &req) && req.HTTPStatus > 0 {
		return fmt.Errorf("%s（HTTP %d）", message, req.HTTPStatus)
	}
	return fmt.Errorf("%s", message)
}

func (s *PlatformService) SecurityTokenKey(ctx context.Context, session Session, id string) (string, error) {
	// Older New API forks expose full values in detail/list and have no /key
	// action; newer releases mask them and require the authenticated action.
	m, _, e := s.ReadSecurityKey(ctx, session, id)
	if e == nil {
		key := safeString(m, "key")
		if len(key) >= 16 && !strings.Contains(key, "*") {
			return key, nil
		}
	}
	key, e := s.FetchNewAPITokenKey(session, id)
	if e != nil || len(key) < 16 || strings.Contains(key, "*") {
		return "", fmt.Errorf("无法读取完整 Token，请核对上游权限")
	}
	return key, nil
}

// customKey is generated and encrypted before the network request. A lost
// response is recovered by this exact operation name; it is never blindly
// retried with a fresh token. New API generates its own token and is recovered
// by the same unique name.
func (s *PlatformService) CreateSecurityKey(ctx context.Context, session Session, config map[string]any, customKey string) (string, string, error) {
	name := safeString(config, "name")
	keys, e := s.SecurityKeys(ctx, session)
	if e != nil {
		return "", "", e
	}
	for _, m := range keys {
		if safeString(m, "name") == name {
			id := groupID2(m)
			key := safeString(m, "key")
			if session.Platform == PlatformNewAPI {
				key, e = s.SecurityTokenKey(ctx, session, id)
			} else if key == "" || strings.Contains(key, "*") {
				key = customKey
			}
			if e != nil || key == "" {
				return "", "", fmt.Errorf("新 Key 已创建但无法回读，请重试核对")
			}
			return id, key, nil
		}
	}
	body := map[string]any{}
	for k, v := range config {
		body[k] = v
	}
	path := "/api/v1/keys"
	opts := requestOptions{Context: ctx, Method: http.MethodPost, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS}
	if session.Platform == PlatformNewAPI {
		path = "/api/token/"
		opts = newAPIRequestOptionsWith(session, http.MethodPost, body)
		opts.Context = ctx
	} else {
		body["custom_key"] = customKey
		delete(body, "status")
		delete(body, "expires_at")
	}
	opts.Body = body
	r, e := s.httpClient.requestJSON(session.BaseURL+path, opts)
	if e != nil {
		return "", "", securityRequestError("新 Key 提交结果需回读核对，请重试同一任务", e)
	}
	if session.Platform == PlatformNewAPI {
		id, e := s.searchNewAPITokenByName(session, name)
		if e != nil {
			return "", "", fmt.Errorf("新 Token 待回读，请重试同一任务")
		}
		key, e := s.SecurityTokenKey(ctx, session, id)
		if e != nil {
			return "", "", fmt.Errorf("新 Token 完整值待回读，请重试同一任务")
		}
		return id, key, nil
	}
	m := dataRecord(r.Payload)
	id := groupID2(m)
	if id == "" {
		return "", "", fmt.Errorf("新 Key 编号待回读，请重试同一任务")
	}
	key := safeString(m, "key")
	if key == "" {
		key = customKey
	}
	return id, key, nil
}

func (s *PlatformService) ConfigureSecurityKey(ctx context.Context, session Session, id string, config map[string]any) error {
	body := map[string]any{}
	for k, v := range config {
		body[k] = v
	}
	opts := requestOptions{Context: ctx, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS, Method: http.MethodPut, Body: body}
	path := "/api/v1/keys/" + url.PathEscape(id)
	if session.Platform == PlatformNewAPI {
		path = "/api/token/"
		n, e := strconv.Atoi(id)
		if e != nil {
			return fmt.Errorf("无效 Token ID")
		}
		body["id"] = n
		opts = newAPIRequestOptionsWith(session, http.MethodPut, body)
		opts.Context = ctx
	}
	_, e := s.httpClient.requestJSON(session.BaseURL+path, opts)
	if e != nil {
		return fmt.Errorf("新 Key 配置保存失败")
	}
	return nil
}

func (s *PlatformService) VerifySecurityConfig(ctx context.Context, session Session, id string, want map[string]any) error {
	got, _, e := s.ReadSecurityKey(ctx, session, id)
	if e != nil {
		return e
	}
	for key, value := range want {
		if key == "ip_whitelist" || key == "allow_ips" {
			continue
		} // checked with normalized IPs separately
		actual, exists := got[key]
		if !exists {
			return fmt.Errorf("新 Key 的 %s 未能回读，旧 Key 尚未撤销", key)
		}
		if key == "expires_at" {
			a, _ := actual.(string)
			b, _ := value.(string)
			ta, ea := time.Parse(time.RFC3339Nano, a)
			tb, eb := time.Parse(time.RFC3339Nano, b)
			if ea == nil && eb == nil && ta.Equal(tb) {
				continue
			}
		}
		// Normalize JSON numbers and string arrays to their wire representation.
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(value)
		if !reflect.DeepEqual(a, b) {
			return fmt.Errorf("新 Key 的 %s 回读不一致，旧 Key 尚未撤销", key)
		}
	}
	return nil
}

func (s *PlatformService) DeleteSecurityKey(ctx context.Context, session Session, id string) error {
	path := "/api/v1/keys/" + url.PathEscape(id)
	opts := requestOptions{Context: ctx, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS, Method: http.MethodDelete}
	if session.Platform == PlatformNewAPI {
		path = "/api/token/" + url.PathEscape(id)
		opts = newAPIRequestOptionsWith(session, http.MethodDelete, nil)
		opts.Context = ctx
	}
	// A previous attempt may have deleted the key but lost its response.
	_, e := s.httpClient.requestJSON(session.BaseURL+path, opts)
	if e != nil {
		var req *RequestError
		if !errors.As(e, &req) || req.HTTPStatus != http.StatusNotFound {
			return fmt.Errorf("旧 Key 撤销失败，请重试当前轮换")
		}
	}
	keys, e := s.SecurityKeys(ctx, session)
	if e != nil {
		return fmt.Errorf("旧 Key 撤销后无法回读核验，请重试当前轮换")
	}
	for _, m := range keys {
		if groupID2(m) == id {
			return fmt.Errorf("旧 Key 仍存在，请重试当前轮换")
		}
	}
	return nil
}

func (s *PlatformService) ReplaceForwardingKey(ctx context.Context, session Session, id, oldKey, newKey string) error {
	a, e := s.FetchForwardingAccount(ctx, session, id)
	if e != nil {
		return e
	}
	if SameMonitorKey(PlatformNewAPI, a.APIKey, newKey) {
		return nil
	}
	if !SameMonitorKey(PlatformNewAPI, a.APIKey, oldKey) {
		return fmt.Errorf("自有渠道 Key 已变化，未覆盖人工修改")
	}
	exported, e := s.httpClient.requestJSON(session.BaseURL+"/api/v1/admin/accounts/data?ids="+url.QueryEscape(id)+"&include_proxies=false", requestOptions{Context: ctx, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS})
	if e != nil {
		return fmt.Errorf("无法读取完整渠道配置，旧 Key 尚未撤销")
	}
	items, _ := dataRecord(exported.Payload)["accounts"].([]any)
	if len(items) != 1 {
		return fmt.Errorf("渠道导出结果不唯一")
	}
	record, ok := items[0].(map[string]any)
	if !ok {
		return fmt.Errorf("渠道配置无效")
	}
	base, key, platform, _ := forwardingFields(record)
	if MonitorAPIBase(base) != MonitorAPIBase(a.BaseURL) || platform != a.Platform || !SameMonitorKey(PlatformNewAPI, key, oldKey) {
		return fmt.Errorf("渠道完整配置与当前绑定不一致")
	}
	credentials, ok := record["credentials"].(map[string]any)
	if !ok {
		return fmt.Errorf("渠道凭据不完整")
	}
	credentials["api_key"] = newKey
	_, e = s.httpClient.requestJSON(session.BaseURL+"/api/v1/admin/accounts/"+url.PathEscape(id), requestOptions{Context: ctx, Method: http.MethodPut, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS, Body: map[string]any{"credentials": credentials}})
	if e != nil {
		return fmt.Errorf("自有渠道 Key 更新失败，旧 Key 尚未撤销")
	}
	a, e = s.FetchForwardingAccount(ctx, session, id)
	if e != nil || !SameMonitorKey(PlatformNewAPI, a.APIKey, newKey) {
		return fmt.Errorf("自有渠道新 Key 回读不一致，旧 Key 尚未撤销")
	}
	return nil
}
