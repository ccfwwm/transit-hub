package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Export preserves provider-specific credentials such as model mappings and
// passthrough options. Only base_url is changed; the bound full Key is verified.
func (s *PlatformService) UpdateForwardingAPIURL(ctx context.Context, session Session, id, boundKey, apiURL string) error {
	if session.Platform != PlatformSub2API {
		return fmt.Errorf("当前自有平台不支持自动同步 API 地址")
	}
	apiURL, e := NormalizeRoutingURL(apiURL)
	if e != nil || apiURL == "" {
		return fmt.Errorf("API 地址无效")
	}
	account, a, e := s.fetchForwardingAccount(ctx, session, id)
	if e != nil {
		return fmt.Errorf("无法核对渠道身份")
	}
	if boundKey == "" || !SameMonitorKey(PlatformNewAPI, account.APIKey, boundKey) {
		return fmt.Errorf("渠道 Key 与绑定不一致，未修改 API 地址")
	}
	accountURL, _ := NormalizeRoutingURL(account.BaseURL)
	if accountURL == apiURL {
		return nil
	}
	opts := requestOptions{Context: ctx, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS}
	if a == nil {
		a, e = s.exportForwardingAccount(ctx, session, id)
		if e != nil {
			return e
		}
	}
	base, key, platform, _ := forwardingFields(a)
	exportURL, _ := NormalizeRoutingURL(base)
	if exportURL != accountURL || platform != account.Platform {
		return fmt.Errorf("渠道配置已变化，请重试同步")
	}
	if boundKey == "" || !SameMonitorKey(PlatformNewAPI, key, boundKey) {
		return fmt.Errorf("渠道 Key 与绑定不一致，未修改 API 地址")
	}
	c, ok := a["credentials"].(map[string]any)
	if !ok {
		return fmt.Errorf("渠道凭据不完整")
	}
	currentURL, _ := NormalizeRoutingURL(safeString(c, "base_url"))
	if currentURL == apiURL {
		return nil
	}
	c["base_url"] = apiURL
	opts.Method, opts.Body = http.MethodPut, map[string]any{"credentials": c}
	if _, e = s.httpClient.requestJSON(session.BaseURL+"/api/v1/admin/accounts/"+url.PathEscape(id), opts); e != nil {
		return fmt.Errorf("渠道 API 地址更新失败")
	}
	got, e := s.FetchForwardingAccount(ctx, session, id)
	gotURL, _ := NormalizeRoutingURL(got.BaseURL)
	if e != nil || gotURL != apiURL || !SameMonitorKey(PlatformNewAPI, got.APIKey, boundKey) {
		return fmt.Errorf("渠道 API 地址回读不一致，请重试")
	}
	return nil
}
