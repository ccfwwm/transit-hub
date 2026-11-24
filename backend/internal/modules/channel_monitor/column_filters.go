package channel_monitor

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
)

type columnRange struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

func channelPageQuery(values url.Values) (ChannelPageQuery, error) {
	query := ChannelPageQuery{
		Page: intQuery(values.Get("page"), 1), PageSize: intQuery(values.Get("pageSize"), 25),
		GroupID: values.Get("groupId"), GroupStatus: values.Get("groupStatus"), Status: values.Get("status"),
		Dispatch: values.Get("dispatch"), Search: values.Get("q"), RuleID: values.Get("ruleId"),
		SiteID: values.Get("siteId"), ModelStatus: values.Get("modelStatus"),
	}
	if ranges := values.Get("ranges"); ranges != "" {
		if len(ranges) > 1024 || json.Unmarshal([]byte(ranges), &query.Ranges) != nil {
			return query, fmt.Errorf("数值筛选格式无效")
		}
	}
	return query, query.validateColumns()
}

var rangeColumns = []string{"upstreamEffectiveMultiplier", "ownGroupMultiplier", "accountPriority", "accountRateMultiplier", "balance"}

func (q ChannelPageQuery) validateColumns() error {
	for key, limits := range q.Ranges {
		if !slices.Contains(rangeColumns, key) {
			return fmt.Errorf("不支持筛选该列")
		}
		for _, value := range []*float64{limits.Min, limits.Max} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
				return fmt.Errorf("请输入有效的数值")
			}
		}
		if limits.Min != nil && limits.Max != nil && *limits.Min > *limits.Max {
			return fmt.Errorf("最小值不能大于最大值")
		}
	}
	return nil
}

func (q ChannelPageQuery) matchesColumns(row ChannelStatus) bool {
	if q.ModelStatus != "" {
		status := row.Status
		if m := row.ModelSummary; m != nil {
			status = m.Status
			if m.Scope == "channel_reference" && !m.Fresh {
				status = StatusUnknown
			}
		}
		if status == StatusAutoPaused {
			status = StatusFailed
		}
		if status == "" {
			status = StatusUnknown
		}
		if status != q.ModelStatus {
			return false
		}
	}
	for key, limits := range q.Ranges {
		if limits.Min == nil && limits.Max == nil {
			continue
		}
		var value *float64
		switch key {
		case "upstreamEffectiveMultiplier":
			value = row.UpstreamEffectiveMultiplier
		case "ownGroupMultiplier":
			value = row.OwnGroupMultiplier
		case "accountPriority":
			if row.AccountPriority != nil {
				priority := float64(*row.AccountPriority)
				value = &priority
			}
		case "accountRateMultiplier":
			value = row.AccountRateMultiplier
		case "balance":
			value = row.Balance
		}
		if value == nil || limits.Min != nil && *value < *limits.Min || limits.Max != nil && *value > *limits.Max {
			return false
		}
	}
	return true
}
