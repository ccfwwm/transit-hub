package upstream

import (
	"fmt"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"
)

const defaultDisplay = "-"

// hostDomainPattern 校验域名：至少包含一个点，各 label 由字母数字与连字符组成，
// 顶级域为 2-63 位字母。用于在登录时拦截明显写错的站点地址。
var hostDomainPattern = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)

// isValidHost 判断 host 是否为合法的 IP（IPv4/IPv6）或域名。
// 不限制只能用域名：纯 IP 部署同样放行；额外放行 localhost 方便本地自托管调试。
func isValidHost(host string) bool {
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	return hostDomainPattern.MatchString(host)
}

func defaultMetrics() Metrics {
	return Metrics{
		Balance:         metric(nil),
		TodayConsume:    metric(nil),
		HistoryRecharge: metric(nil),
		Group: GroupInfo{
			Name:              defaultDisplay,
			Platform:          nil,
			Multiplier:        nil,
			MultiplierDisplay: defaultDisplay,
		},
		Groups: []GroupInfo{},
	}
}

func dataRecord(value any) map[string]any {
	record, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	if data, ok := record["data"].(map[string]any); ok {
		return data
	}
	return record
}

func dataArray(value any) []any {
	record, ok := value.(map[string]any)
	if !ok {
		return []any{}
	}
	if items, ok := record["data"].([]any); ok {
		return items
	}
	if items, ok := record["items"].([]any); ok {
		return items
	}
	if data, ok := record["data"].(map[string]any); ok {
		for _, key := range []string{"items", "list", "records", "orders", "groups", "platform_quotas"} {
			if items, ok := data[key].([]any); ok {
				return items
			}
		}
	}
	return []any{}
}

func paginationTotal(value any) (int, bool) {
	data := dataRecord(value)
	if total := firstNumber(data, []string{"total", "count"}); total != nil {
		return int(*total), true
	}
	return 0, false
}

func safeString(record map[string]any, key string) string {
	if text, ok := record[key].(string); ok {
		return text
	}
	return ""
}

func firstString(value any, keys []string) *string {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range keys {
		if text, ok := record[key].(string); ok && text != "" {
			return &text
		}
	}
	return nil
}

func firstNumber(value any, keys []string) *float64 {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range keys {
		if number := readNumber(record[key]); number != nil {
			return number
		}
	}
	return nil
}

func groupID(value any) string {
	if number := firstNumber(value, []string{"id", "group_id", "groupId"}); number != nil {
		return strconv.FormatInt(int64(*number), 10)
	}
	if text := firstString(value, []string{"id", "group_id", "groupId"}); text != nil {
		return strings.TrimSpace(*text)
	}
	return ""
}

func readNumber(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		if isFinite(typed) {
			return &typed
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err == nil && isFinite(parsed) {
			return &parsed
		}
	}
	return nil
}

func metric(value *float64) MetricValue {
	if value == nil {
		return MetricValue{Value: nil, Display: defaultDisplay}
	}
	return MetricValue{Value: value, Display: formatNumber(*value)}
}

func multiplier(value *float64) string {
	if value == nil {
		return defaultDisplay
	}
	return formatNumber(*value) + "x"
}

func formatNumber(value float64) string {
	formatted := strconv.FormatFloat(value, 'f', 4, 64)
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")
	if formatted == "-0" {
		return "0"
	}
	return formatted
}

func sumPaidAmounts(items []any) *float64 {
	if len(items) == 0 {
		return nil
	}
	total := 0.0
	for _, item := range items {
		status := firstString(item, []string{"status", "state"})
		paid := status == nil
		if status != nil {
			switch strings.ToLower(*status) {
			case "paid", "success", "completed", "complete":
				paid = true
			}
		}
		amount := firstNumber(item, []string{"amount", "money", "quota", "total_amount"})
		if paid && amount != nil {
			total += *amount
		}
	}
	if isFinite(total) {
		return &total
	}
	return nil
}

// defaultQuotaPerUnit 是 new-api 默认的 quota 换算单位。
const defaultQuotaPerUnit = 500000

func quotaToUSD(value *float64) *float64 {
	return quotaToUSDWithUnit(value, defaultQuotaPerUnit)
}

func quotaToUSDValue(value *float64) float64 {
	return quotaToUSDValueWithUnit(value, defaultQuotaPerUnit)
}

// quotaToUSDWithUnit 使用指定的 quotaPerUnit 将 quota 换算为 USD。
// quotaPerUnit <= 0 时回退到默认值。
func quotaToUSDWithUnit(value *float64, quotaPerUnit float64) *float64 {
	if value == nil {
		return nil
	}
	if quotaPerUnit <= 0 {
		quotaPerUnit = defaultQuotaPerUnit
	}
	converted := *value / quotaPerUnit
	if !isFinite(converted) {
		return nil
	}
	return &converted
}

func quotaToUSDValueWithUnit(value *float64, quotaPerUnit float64) float64 {
	converted := quotaToUSDWithUnit(value, quotaPerUnit)
	if converted == nil {
		return 0
	}
	return *converted
}

func newAPIGroups(groupsPayload any, pricingPayload any) []GroupInfo {
	groupsRecord := dataRecord(groupsPayload)
	pricingRecord := dataRecord(pricingPayload)
	groupRatios := map[string]*float64{}
	for name, source := range groupsRecord {
		if ratio := firstNumber(source, []string{"ratio", "rate", "multiplier"}); ratio != nil {
			groupRatios[name] = ratio
		}
	}
	if values, ok := pricingRecord["group_ratio"].(map[string]any); ok {
		for name, source := range values {
			if ratio := readNumber(source); ratio != nil {
				groupRatios[name] = ratio
			}
		}
	}
	usableGroups := map[string]string{}
	if values, ok := pricingRecord["usable_group"].(map[string]any); ok {
		for name, source := range values {
			if desc, ok := source.(string); ok {
				usableGroups[name] = desc
			}
		}
	}
	platforms := newAPIGroupPlatforms(pricingPayload)
	seen := map[string]struct{}{}
	result := make([]GroupInfo, 0, len(groupRatios))
	appendGroup := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		ratio := groupRatios[name]
		if ratio == nil {
			return
		}
		platform := platforms[name]
		result = append(result, GroupInfo{ID: name, Name: name, Platform: platform, Multiplier: ratio, MultiplierDisplay: multiplier(ratio)})
		seen[name] = struct{}{}
	}
	for name := range usableGroups {
		appendGroup(name)
	}
	for name := range groupRatios {
		appendGroup(name)
	}
	return result
}

func newAPIGroupPlatforms(pricingPayload any) map[string]*string {
	platformSets := map[string]map[string]struct{}{}
	for _, item := range dataArray(pricingPayload) {
		record := dataRecord(item)
		platform := firstString(record, []string{"owner_by", "ownerBy", "platform", "vendor"})
		if platform == nil || strings.TrimSpace(*platform) == "" {
			continue
		}
		for _, group := range readStringArray(record["enable_groups"]) {
			addGroupPlatform(platformSets, group, *platform)
		}
		for _, group := range readStringArray(record["enable_group"]) {
			addGroupPlatform(platformSets, group, *platform)
		}
	}
	result := map[string]*string{}
	for group, platforms := range platformSets {
		if len(platforms) == 0 {
			continue
		}
		names := make([]string, 0, len(platforms))
		for name := range platforms {
			names = append(names, name)
		}
		value := strings.Join(names, ", ")
		result[group] = &value
	}
	return result
}

func addGroupPlatform(groups map[string]map[string]struct{}, group string, platform string) {
	group = strings.TrimSpace(group)
	platform = strings.TrimSpace(platform)
	if group == "" || platform == "" {
		return
	}
	if groups[group] == nil {
		groups[group] = map[string]struct{}{}
	}
	groups[group][platform] = struct{}{}
}

func readStringArray(value any) []string {
	switch typed := value.(type) {
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				items = append(items, text)
			}
		}
		return items
	case []string:
		return typed
	case string:
		parts := strings.Split(typed, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	}
	return nil
}

func newAPIGroupRatio(name string, groups []GroupInfo) *float64 {
	for _, group := range groups {
		if group.Name == name {
			return group.Multiplier
		}
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsInf(value, 0) && !math.IsNaN(value)
}

func invalidBodyError(fields ...string) error {
	return fmt.Errorf("missing or invalid fields: %s", strings.Join(fields, ", "))
}
