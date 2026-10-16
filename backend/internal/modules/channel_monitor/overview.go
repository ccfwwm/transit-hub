package channel_monitor

import (
	"context"
	"strings"

	"transithub/backend/internal/modules/upstream"
)

type monitorSitesKey struct{}
type channelViewContext struct {
	groupID string
	recent  map[string][]Result
	targets map[string]TargetState
}

func (s *Service) monitorSite(ctx context.Context, id string) (*upstream.Site, error) {
	if sites, ok := ctx.Value(monitorSitesKey{}).(map[string]*upstream.Site); ok {
		return sites[id], nil
	}
	return s.upstreams.GetSite(ctx, id)
}

func applyTargetToChannel(row *ChannelStatus, t TargetState) {
	row.Status = t.Status
	row.ConsecutiveFailures = t.ConsecutiveFailures
	row.LastMessage = t.Message
	row.LastLatencyMS = t.LatencyMS
	row.LastCheckedAt = t.CheckedAt
	row.NextCheckAt = t.NextCheckAt
	row.MediaTaskKey = t.MediaTaskKey
	row.EffectiveTestModelID = t.Model
	row.EffectiveTestMode = t.Mode
	row.DetectionGroupID = t.GroupID
}

type ChannelPageQuery struct {
	GroupID, GroupStatus, Status, Search, RuleID string
	Page, PageSize                               int
}
type ChannelPage struct {
	Channels []ChannelStatus `json:"channels"`
	Total    int             `json:"total"`
	RuleIDs  []string        `json:"ruleIds"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

func (s *Service) ChannelPage(ctx context.Context, userID string, q ChannelPageQuery) (ChannelPage, error) {
	overview, err := s.summaryInGroup(ctx, userID, q.GroupID, true)
	if err != nil {
		return ChannelPage{}, err
	}
	return s.channelPageFromSummary(ctx, overview, q)
}

func (s *Service) channelPageFromSummary(ctx context.Context, overview SummaryResponse, q ChannelPageQuery) (ChannelPage, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize != 50 && q.PageSize != 100 {
		q.PageSize = 25
	}
	allowedGroups := map[string]bool{}
	for _, group := range overview.Groups {
		allowedGroups[group.GroupID] = (q.GroupStatus == "" || q.GroupStatus == "all" || (q.GroupStatus == "available" && group.Available > 0) || (q.GroupStatus == "unavailable" && group.Available == 0))
	}
	response := ChannelPage{Channels: []ChannelStatus{}, RuleIDs: []string{}, Page: q.Page, PageSize: q.PageSize}
	matches := []ChannelStatus{}
	search := strings.ToLower(strings.TrimSpace(q.Search))
	for _, row := range overview.Channels {
		if q.RuleID != "" && row.RuleID != q.RuleID {
			continue
		}
		if q.GroupID != "" && !containsString(row.OwnGroupIDs, q.GroupID) {
			continue
		}
		groupAllowed := q.GroupStatus == "" || q.GroupStatus == "all"
		for _, id := range row.OwnGroupIDs {
			if allowedGroups[id] && (q.GroupID == "" || id == q.GroupID) {
				groupAllowed = true
			}
		}
		if !groupAllowed {
			continue
		}
		if q.Status != "" && q.Status != "all" && row.Status != q.Status && !(q.Status == "monitor_paused" && !row.Enabled) && !(q.Status == "dispatch_paused" && row.Schedulable != nil && !*row.Schedulable) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(strings.Join([]string{row.SiteName, row.UpstreamGroupName, row.AdminAccountName, row.AdminAccountID, strings.Join(row.OwnGroups, " ")}, " ")), search) {
			continue
		}
		matches = append(matches, row)
		response.RuleIDs = append(response.RuleIDs, row.RuleID)
	}
	response.Total = len(matches)
	start := (q.Page - 1) * q.PageSize
	if start >= len(matches) {
		return response, nil
	}
	end := start + q.PageSize
	if end > len(matches) {
		end = len(matches)
	}
	response.Channels = matches[start:end]
	if store, ok := s.store.(interface {
		PageHistory(context.Context, []string, []string) (map[string][]Result, error)
	}); ok {
		ids, groups := []string{}, []string{}
		for _, row := range response.Channels {
			ids = append(ids, row.RuleID)
			groups = append(groups, row.DetectionGroupID)
		}
		histories, err := store.PageHistory(ctx, ids, groups)
		if err != nil {
			return ChannelPage{}, err
		}
		for i := range response.Channels {
			row := &response.Channels[i]
			row.RecentResults = histories[row.RuleID+"|"+row.DetectionGroupID]
			if row.RecentResults == nil {
				row.RecentResults = []Result{}
			}
			row.RecentTotal = len(row.RecentResults)
			row.RecentSuccess = 0
			for _, r := range row.RecentResults {
				if r.Success {
					row.RecentSuccess++
				}
			}
			if row.RecentTotal > 0 {
				row.UptimePercent = float64(row.RecentSuccess) / float64(row.RecentTotal) * 100
			}
		}
	}
	return response, nil
}
