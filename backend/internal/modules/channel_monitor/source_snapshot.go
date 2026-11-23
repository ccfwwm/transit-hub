package channel_monitor

import (
	"context"
	"net/url"
	"strconv"

	"transithub/backend/internal/modules/upstream"
)

// The read snapshot uses the selected display source. This never modifies the
// rule's dispatch evidence or promotes a reference report into automation.
func (s *Service) applyStatusSource(ctx context.Context, uid, aid string, base *SummaryResponse) error {
	if s.health == nil {
		return nil
	}
	cache := map[string][]upstream.ModelHealth{}
	for i := range base.Channels {
		row := &base.Channels[i]
		key := row.SiteID + "|" + row.UpstreamGroupID
		models, exists := cache[key]
		if !exists {
			q := url.Values{"groupId": {row.UpstreamGroupID}, "groupName": {row.UpstreamGroupName}, "pageSize": {"100"}}
			for page := 1; page <= 100; page++ {
				q.Set("page", strconv.Itoa(page))
				v, e := s.health.Page(ctx, uid, aid, row.SiteID, q)
				if e != nil {
					return e
				}
				models = append(models, v.Items...)
				if page*100 >= v.Total {
					break
				}
			}
			cache[key] = models
		}
		checks := map[string]ManualModelCheck{}
		if repo, ok := s.store.(*Repository); ok {
			if v, e := repo.manualChecks(ctx, uid, aid, row.RuleID); e == nil {
				checks = v
			}
		}
		row.ModelSummaries = map[string]upstream.ModelHealth{}
		row.ModelSummaries[row.EffectiveTestModelID] = selectedModelSummary(models, checks, row.EffectiveTestModelID)
		for _, group := range row.GroupStates {
			row.ModelSummaries[group.Model] = selectedModelSummary(models, checks, group.Model)
		}
		if row.ManualPaused || row.Status == StatusBalancePaused {
			continue
		}
		row.Status = selectedStatusWithManual(models, checks, row.EffectiveTestModelID)
		for j := range row.GroupStates {
			row.GroupStates[j].Status = selectedStatusWithManual(models, checks, row.GroupStates[j].Model)
		}

	}
	for i := range base.Groups {
		group := &base.Groups[i]
		total := SummaryStats{}
		for _, row := range base.Channels {
			if containsString(row.OwnGroupIDs, group.GroupID) {
				applyStats(&total, row)
			}
		}
		group.Total = total.Total
		group.Available = total.Available
		group.Failed = total.Failed
		group.BalancePaused = total.BalancePaused
		group.ManualPaused = total.ManualPaused
		group.MonitorPaused = total.MonitorPaused
		group.DispatchPaused = total.DispatchPaused
	}
	return nil
}

// Preserve the latest observation separately from history; a failed old point
// must never override the most recent status when rendering the compact row.
func selectedModelSummary(models []upstream.ModelHealth, checks map[string]ManualModelCheck, model string) upstream.ModelHealth {
	result := upstream.ModelHealth{ID: model, Model: model, Status: StatusUnknown, Source: "catalog", Timeline: []upstream.HealthPoint{}}
	for _, m := range models {
		if m.Model != model || m.Scope == "infrastructure" || m.Scope == "channel_reference" {
			continue
		}
		if result.Source == "catalog" || m.CheckedAt != nil && (result.CheckedAt == nil || m.CheckedAt.After(*result.CheckedAt)) {
			result = m
		}
	}
	if check, ok := checks[model]; ok && check.CheckedAt != nil && (result.CheckedAt == nil || !result.CheckedAt.After(*check.CheckedAt)) {
		result.Status, result.CheckedAt, result.Message, result.LatencyMS = check.Status, check.CheckedAt, check.Message, check.LatencyMS
		if check.Status == StatusHealthy || check.Status == StatusFailed {
			points := append([]upstream.HealthPoint(nil), result.Timeline...)
			points = append(points, upstream.HealthPoint{Status: check.Status, CheckedAt: check.CheckedAt, LatencyMS: check.LatencyMS})
			if len(points) > 60 {
				points = points[len(points)-60:]
			}
			result.Timeline = points
		}
	}
	return result
}
func reportedStatus(models []upstream.ModelHealth) string {
	count, failed := 0, 0
	for _, m := range models {
		if !m.Fresh || m.Scope == "infrastructure" || m.Source == "newapi_kuma" && m.Scope != "group_model" {
			continue
		}
		count++
		if m.Status == StatusHealthy {
			return StatusHealthy
		}
		if m.Status == StatusFailed {
			failed++
		}
	}
	if count > 0 && failed == count {
		return StatusFailed
	}
	return StatusUnknown
}

func selectedReportedStatus(models []upstream.ModelHealth, model string) string {
	var selected *upstream.ModelHealth
	for i := range models {
		m := &models[i]
		if m.Model != model || m.Scope == "infrastructure" || m.Scope == "channel_reference" {
			continue
		}
		if selected == nil || m.CheckedAt != nil && (selected.CheckedAt == nil || m.CheckedAt.After(*selected.CheckedAt)) {
			selected = m
		}
	}
	if selected == nil {
		return StatusUnknown
	}
	return selected.Status
}

func selectedStatusWithManual(models []upstream.ModelHealth, checks map[string]ManualModelCheck, model string) string {
	status := selectedReportedStatus(models, model)
	if v, ok := checks[model]; ok && v.CheckedAt != nil {
		newer := true
		for _, m := range models {
			if m.Model == model && m.CheckedAt != nil && m.CheckedAt.After(*v.CheckedAt) {
				newer = false
			}
		}
		if newer {
			return v.Status
		}
	}
	return status
}
