package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

var Sub2APIKeyFields = []string{"group_id", "status", "quota", "expires_at", "ip_whitelist", "ip_blacklist", "rate_limit_5h", "rate_limit_1d", "rate_limit_7d"}
var NewAPIKeyFields = []string{"group", "status", "remain_quota", "unlimited_quota", "expired_time", "allow_ips", "model_limits_enabled", "model_limits", "group_config", "preset_id", "sticky_session", "remark", "cross_group_retry", "auto_groups", "allow_user_quota_query"}

func ManagedKeyConfig(platform Platform, record map[string]any) map[string]any {
	fields := Sub2APIKeyFields
	if platform == PlatformNewAPI {
		fields = NewAPIKeyFields
	}
	config := make(map[string]any)
	for _, field := range fields {
		if value := record[field]; value != nil {
			config[field] = value
		}
	}
	return config
}

func (s *PlatformService) KeySecret(ctx context.Context, session Session, id string, record map[string]any) (string, error) {
	if session.Platform == PlatformNewAPI {
		return s.SecurityTokenKey(ctx, session, id)
	}
	key := safeString(record, "key")
	if key == "" || strings.Contains(key, "*") {
		return "", fmt.Errorf("上游未返回完整 Key")
	}
	return key, nil
}

func (s *PlatformService) RenameManagedKey(ctx context.Context, session Session, id, name string) error {
	record, _, err := s.ReadSecurityKey(ctx, session, id)
	if err != nil || safeString(record, "name") == name {
		return err
	}
	update := map[string]any{"name": name}
	if session.Platform == PlatformNewAPI {
		update = ManagedKeyConfig(session.Platform, record)
		update["name"] = name
	}
	if err = s.ConfigureSecurityKey(ctx, session, id, update); err != nil {
		return err
	}
	record, _, err = s.ReadSecurityKey(ctx, session, id)
	if err != nil || safeString(record, "name") != name {
		return fmt.Errorf("上游 Key 名称回读不一致")
	}
	return nil
}

// CreateManagedKey sends one create request. Durable callers use a unique
// operation name and reconcile the inventory before retrying uncertain writes.
func (s *PlatformService) CreateManagedKey(ctx context.Context, session Session, config map[string]any, custom string) (string, string, error) {
	body := make(map[string]any, len(config)+1)
	for name, value := range config {
		body[name] = value
	}
	path := "/api/v1/keys"
	opts := requestOptions{Method: http.MethodPost, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS, Body: body}
	if session.Platform == PlatformNewAPI {
		path = "/api/token/"
		opts = newAPIRequestOptionsWith(session, http.MethodPost, body)
	} else if custom != "" {
		body["custom_key"] = custom
	}
	opts.Context = ctx
	res, err := s.httpClient.requestJSON(session.BaseURL+path, opts)
	if err != nil {
		return "", "", securityRequestError("上游创建 Key 失败或结果待核对", err)
	}
	record := dataRecord(res.Payload)
	id := groupID2(record)
	if session.Platform == PlatformNewAPI {
		if id == "" {
			id, err = s.searchNewAPITokenByName(session, safeString(body, "name"))
			if err != nil {
				return "", "", fmt.Errorf("新 Token 编号暂不可回读")
			}
		}
		// Inventory callers do not need to reveal an upstream-generated token.
		return id, "", nil
	}
	if id == "" {
		return "", "", fmt.Errorf("新 Key 编号暂不可回读")
	}
	return id, safeString(record, "key"), nil
}

func (s *PlatformService) UpdateForwardingAccountIdentity(ctx context.Context, session Session, id, name string, groups []int) error {
	body := map[string]any{}
	if name != "" {
		body["name"] = name
	}
	if groups != nil {
		body["group_ids"] = groups
	}
	_, err := s.httpClient.requestJSON(session.BaseURL+"/api/v1/admin/accounts/"+url.PathEscape(id), requestOptions{
		Context: ctx, Method: http.MethodPut, Body: body, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS,
	})
	return err
}

func (s *PlatformService) CloneForwardingAccount(ctx context.Context, session Session, sourceID, name, oldKey, newKey string) (string, error) {
	record, err := s.exportForwardingAccount(ctx, session, sourceID)
	if err != nil {
		return "", err
	}
	_, key, _, _ := forwardingFields(record)
	if !SameMonitorKey(PlatformNewAPI, oldKey, key) {
		return "", fmt.Errorf("转发账号凭据已变化")
	}
	body := map[string]any{"name": name, "group_ids": []int{}, "skip_default_group_bind": true}
	for _, field := range []string{"platform", "type", "credentials", "extra", "concurrency", "priority", "rate_multiplier", "load_factor", "expires_at", "auto_pause_on_expired"} {
		if value := record[field]; value != nil {
			body[field] = value
		}
	}
	credentials, ok := body["credentials"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("转发账号缺少可复制配置")
	}
	credentials["api_key"] = newKey
	id, err := s.CreateSub2APIAdminAccount(session, body)
	if err != nil {
		return "", err
	}
	if err = s.SetSub2APIAdminAccountSchedulable(session, id, false); err != nil {
		return "", err
	}
	return id, nil
}

type ManagedUsageRecord struct {
	ID      string
	At      int64
	Amount  *float64
	Model   string
	IP      string
	TokenID string
}

func (s *PlatformService) ManagedKeyUsagePage(ctx context.Context, session Session, id, name string, start, end int64, startDate, endDate string, page int) ([]ManagedUsageRecord, bool, error) {
	params := url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}}
	path := "/api/v1/usage"
	opts := sub2APIRequestOptions(session)
	if session.Platform == PlatformNewAPI {
		path = "/api/log/self"
		opts = newAPIRequestOptions(session)
		params.Set("p", strconv.Itoa(page))
		params.Set("type", "2")
		params.Set("start_timestamp", strconv.FormatInt(start, 10))
		params.Set("end_timestamp", strconv.FormatInt(end-1, 10))
		if name != "" {
			params.Set("token_name", name)
		}
	} else {
		params.Set("api_key_id", id)
		params.Set("start_date", startDate)
		params.Set("end_date", endDate)
		params.Set("timezone", "Asia/Shanghai")
	}
	opts.Context = ctx
	response, err := s.httpClient.requestJSON(session.BaseURL+path+"?"+params.Encode(), opts)
	if err != nil {
		return nil, false, err
	}
	items := dataArray(response.Payload)
	pageRecord := dataRecord(response.Payload)
	_, paginated := pageRecord["items"].([]any)
	_, bare := response.Payload.([]any)
	root, _ := response.Payload.(map[string]any)
	if success, known := root["success"].(bool); known && !success {
		return nil, false, fmt.Errorf("上游拒绝读取调用明细")
	}
	_, direct := root["data"].([]any)
	total, known := paginationTotal(response.Payload)
	if !paginated && !bare && !direct && !known {
		return nil, false, fmt.Errorf("上游用量日志格式不可识别")
	}
	result := make([]ManagedUsageRecord, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("上游用量日志格式不可识别")
		}
		r := ManagedUsageRecord{ID: groupID2(m), Model: safeString(m, "model"), IP: safeString(m, "ip_address"), Amount: firstNumber(m, []string{"actual_cost"}), TokenID: id}
		if session.Platform == PlatformNewAPI {
			r.TokenID = ""
			if value := firstNumber(m, []string{"token_id"}); value != nil && *value > 0 {
				r.TokenID = strconv.FormatInt(int64(*value), 10)
			}
			r.Model, r.IP, r.Amount = safeString(m, "model_name"), safeString(m, "ip"), firstNumber(m, []string{"quota"})
			if at := firstNumber(m, []string{"created_at"}); at != nil {
				r.At = int64(*at)
			}
		} else {
			r.At = parseUsageTime(safeString(m, "created_at"))
			if value := firstNumber(m, []string{"api_key_id"}); value != nil && strconv.FormatInt(int64(*value), 10) != id {
				return nil, false, fmt.Errorf("上游未按 Key 筛选用量")
			}
		}
		result = append(result, r)
	}
	size := 100
	if n := firstInt(pageRecord, []string{"page_size", "pageSize", "per_page"}); n != nil && *n > 0 {
		size = *n
	}
	return result, len(items) > 0 && (!known && !bare && !direct || known && page*size < total), nil
}

func parseUsageTime(raw string) int64 {
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return 0
	}
	return at.Unix()
}

func (s *PlatformService) VerifyManagedKeyConfig(ctx context.Context, session Session, id string, want map[string]any) error {
	record, _, err := s.ReadSecurityKey(ctx, session, id)
	if err != nil {
		return err
	}
	for name, value := range want {
		actual, present := record[name]
		if name == "ip_whitelist" || name == "ip_blacklist" || name == "allow_ips" {
			if slices.Equal(keyIPValues(actual), keyIPValues(value)) {
				continue
			}
		}
		if name == "expires_at" {
			a, ea := time.Parse(time.RFC3339Nano, fmt.Sprint(actual))
			b, eb := time.Parse(time.RFC3339Nano, fmt.Sprint(value))
			if ea == nil && eb == nil && a.Equal(b) {
				continue
			}
		}
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(value)
		if !present || !bytes.Equal(a, b) {
			return fmt.Errorf("Key 字段 %s 回读不一致", name)
		}
	}
	return nil
}

func keyIPValues(value any) []string {
	var values []string
	switch typed := value.(type) {
	case []string:
		values = slices.Clone(typed)
	case []any:
		for _, item := range typed {
			if ip, ok := item.(string); ok {
				values = append(values, ip)
			}
		}
	case string:
		values = strings.Fields(typed)
	}
	slices.Sort(values)
	return values
}

func (s *PlatformService) UpdateNewAPIKeyStatus(ctx context.Context, session Session, id string, status any) error {
	number, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("上游 Key ID 无效")
	}
	if state, ok := status.(float64); !ok || (state != 1 && state != 2) {
		return fmt.Errorf("上游 Key 状态无效")
	}
	options := newAPIRequestOptionsWith(session, http.MethodPut, map[string]any{"id": number, "status": status})
	options.Context = ctx
	_, err = s.httpClient.requestJSON(session.BaseURL+"/api/token/?status_only=true", options)
	return err
}

type KeyUsageAggregate struct {
	Time     time.Time
	Label    string
	Requests int
	Amount   float64
}

func (s *PlatformService) FetchManagedKeyAggregates(ctx context.Context, session Session, id, granularity string, start, end time.Time) ([]KeyUsageAggregate, []KeyUsageAggregate, error) {
	return s.fetchManagedUsageAggregates(ctx, session, id, granularity, start, end, true)
}

func (s *PlatformService) FetchManagedUsageTrend(ctx context.Context, session Session, id string, start, end time.Time) ([]KeyUsageAggregate, error) {
	trend, _, err := s.fetchManagedUsageAggregates(ctx, session, id, "day", start, end, false)
	return trend, err
}

func (s *PlatformService) fetchManagedUsageAggregates(ctx context.Context, session Session, id, granularity string, start, end time.Time, includeModels bool) ([]KeyUsageAggregate, []KeyUsageAggregate, error) {
	if granularity != "hour" {
		granularity = "day"
	}
	dates := url.Values{
		"start_date": {start.Format("2006-01-02")},
		"end_date":   {end.AddDate(0, 0, -1).Format("2006-01-02")}, "timezone": {"Asia/Shanghai"},
		"granularity": {granularity}, "include_trend": {"true"}, "include_models": {strconv.FormatBool(includeModels)},
	}
	if id != "" {
		dates.Set("api_key_id", id)
	}
	opts := sub2APIRequestOptions(session)
	opts.Context = ctx
	res, err := s.httpClient.requestJSON(session.BaseURL+"/api/v1/usage/dashboard/snapshot-v2?"+dates.Encode(), opts)
	if err != nil {
		return nil, nil, err
	}
	data := dataRecord(res.Payload)
	if safeString(data, "granularity") != granularity || safeString(data, "start_date") != dates.Get("start_date") || safeString(data, "end_date") != dates.Get("end_date") {
		return nil, nil, fmt.Errorf("上游聚合返回的周期不匹配")
	}
	trend, err := decodeManagedKeyAggregates(data["trend"], "date")
	if err != nil {
		return nil, nil, err
	}
	layout := "2006-01-02"
	if granularity == "hour" {
		layout += " 15:04"
	}
	for i := range trend {
		trend[i].Time, err = time.ParseInLocation(layout, trend[i].Label, start.Location())
		if err != nil || trend[i].Time.Before(start) || !trend[i].Time.Before(end) {
			return nil, nil, fmt.Errorf("上游返回的聚合周期无效")
		}
	}
	if !includeModels {
		return trend, nil, nil
	}
	models, err := decodeManagedKeyAggregates(data["models"], "model")
	return trend, models, err
}

func decodeManagedKeyAggregates(value any, labelField string) ([]KeyUsageAggregate, error) {
	rows, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("上游聚合数据不可用")
	}
	values := make([]KeyUsageAggregate, 0, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("上游聚合格式不可识别")
		}
		requests := firstInt(m, []string{"requests"})
		cost := firstNumber(m, []string{"actual_cost"})
		label := safeString(m, labelField)
		if requests == nil || cost == nil || label == "" {
			return nil, fmt.Errorf("上游聚合字段不完整")
		}
		values = append(values, KeyUsageAggregate{Label: label, Requests: *requests, Amount: *cost})
	}
	return values, nil
}
