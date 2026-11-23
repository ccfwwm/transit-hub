package my_sites

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/httpjson"
)

type keyUsageSummary struct {
	State string        `json:"state"`
	Unit  string        `json:"unit"`
	Today keyUsagePoint `json:"today"`
	Week  keyUsagePoint `json:"week"`
}

func (m *KeyManagement) cachedUsageSummary(ctx context.Context, key string, load func(context.Context) any) (any, error) {
	return m.cached(ctx, key, func(run context.Context) (any, error) {
		select {
		case m.usageSlots <- struct{}{}:
			defer func() { <-m.usageSlots }()
			return load(run), nil
		case <-run.Done():
			return nil, run.Err()
		}
	})
}

func (m *KeyManagement) keyUsageSummary(w http.ResponseWriter, r *http.Request) {
	uid, aid, err := m.scope(r)
	if err != nil {
		httpjson.WriteError(w, 401, "工作区会话不可用")
		return
	}
	var req struct {
		Keys []keyRef `json:"keys"`
	}
	if httpjson.Decode(r, &req) != nil || len(req.Keys) == 0 || len(req.Keys) > 25 {
		httpjson.WriteError(w, 400, "请选择当前页的 Key（最多 25 个）")
		return
	}
	ctx := r.Context()
	sites, err := m.workspaceSites(ctx, uid, aid)
	if err != nil {
		httpjson.WriteError(w, 500, "无法读取上游站点")
		return
	}
	now := time.Now().In(usageZone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, usageZone)
	perSite := map[string][]keyRef{}
	seen := map[keyRef]bool{}
	for _, ref := range req.Keys {
		if !ref.valid() || sites[ref.SiteID] == nil {
			httpjson.WriteError(w, 400, "存在无效或不属于当前工作区的 Key")
			return
		}
		if !seen[ref] {
			seen[ref] = true
			perSite[ref.SiteID] = append(perSite[ref.SiteID], ref)
		}
	}
	// Uncached work is bounded to the current page. New API uses one shared
	// account log scan per site, even when multiple pages are opened.
	type summaryResult struct {
		id   string
		data keyUsageSummary
	}
	results := make(chan summaryResult, len(seen))
	var group errgroup.Group
	group.SetLimit(4)
	for siteID, refs := range perSite {
		site := sites[siteID]
		if site.Platform == upstream.PlatformNewAPI {
			group.Go(func() error {
				state := "unavailable"
				var byID map[string]keyUsageSummary
				if site.Session != nil {
					key := fmt.Sprintf("%s/usage-summary/account/%d", inventoryCacheKey(site), today.Unix())
					value, err := m.cachedUsageSummary(ctx, key, func(run context.Context) any {
						return m.newAPIUsageSummary(run, site, today)
					})
					if err == nil {
						summary := value.(accountKeyUsageSummary)
						state, byID = summary.state, summary.byID
					}
				}
				for _, ref := range refs {
					row := byID[ref.KeyID]
					row.State, row.Unit = state, "quota"
					row.fillZeroes()
					results <- summaryResult{ref.itemID(), row}
				}
				return nil
			})
			continue
		}
		for _, ref := range refs {
			group.Go(func() error {
				row := keyUsageSummary{State: "unavailable", Unit: "USD"}
				if site.Session != nil {
					key := fmt.Sprintf("%s/usage-summary/%s/%d", inventoryCacheKey(site), ref.KeyID, today.Unix())
					value, err := m.cachedUsageSummary(ctx, key, func(run context.Context) any {
						return m.sub2UsageSummary(run, site, ref.KeyID, today)
					})
					if err == nil {
						row = value.(keyUsageSummary)
					}
				}
				results <- summaryResult{ref.itemID(), row}
				return nil
			})
		}
	}
	_ = group.Wait()
	close(results)
	out := make(map[string]keyUsageSummary, len(seen))
	for result := range results {
		out[result.id] = result.data
	}
	httpjson.Write(w, 200, out)
}

func (row *keyUsageSummary) fillZeroes() {
	if row.State != "complete" {
		return
	}
	for _, point := range []*keyUsagePoint{&row.Today, &row.Week} {
		if point.Amount == nil {
			point.Amount = new(float64)
		}
	}
}

func (m *KeyManagement) sub2UsageSummary(ctx context.Context, site *upstream.Site, id string, today time.Time) keyUsageSummary {
	out := keyUsageSummary{State: "unavailable", Unit: "USD"}
	start, end := today.AddDate(0, 0, -6), today.AddDate(0, 0, 1)
	series, err := m.platform.FetchManagedUsageTrend(ctx, *site.Session, id, start, end)
	if err != nil {
		// Older deployments may expose only the paginated key usage endpoint.
		logs, more, logErr := m.platform.ManagedKeyUsagePage(ctx, *site.Session, id, "", start.Unix(), end.Unix(), start.Format("2006-01-02"), today.Format("2006-01-02"), 1)
		if logErr != nil {
			return out
		}
		out.State = "complete"
		if more {
			out.State = "partial"
		}
		seen := map[string]bool{}
		for _, log := range logs {
			if log.At < start.Unix() || log.At >= end.Unix() || log.ID == "" || seen[log.ID] {
				out.State = "partial"
				continue
			}
			seen[log.ID] = true
			if log.Amount == nil {
				out.State = "partial"
			}
			out.addLog(log, today.Unix())
		}
	} else {
		out.State = "complete"
		for _, point := range series {
			addAggregate(&out.Week, point)
			if !point.Time.Before(today) {
				addAggregate(&out.Today, point)
			}
		}
	}
	out.fillZeroes()
	return out
}

type accountKeyUsageSummary struct {
	state string
	byID  map[string]keyUsageSummary
}

func (row *keyUsageSummary) addLog(log upstream.ManagedUsageRecord, today int64) {
	usageAmount(&row.Week, log.Amount)
	if log.At >= today {
		usageAmount(&row.Today, log.Amount)
	}
}

func (m *KeyManagement) newAPIUsageSummary(ctx context.Context, site *upstream.Site, today time.Time) accountKeyUsageSummary {
	out := accountKeyUsageSummary{state: "unavailable", byID: map[string]keyUsageSummary{}}
	start, end := today.AddDate(0, 0, -6).Unix(), today.AddDate(0, 0, 1).Unix()
	seen := map[string]bool{}
	const maxPages = 8
	for page := 1; page <= maxPages; page++ {
		logs, more, err := m.platform.ManagedKeyUsagePage(ctx, *site.Session, "", "", start, end, "", "", page)
		if err != nil {
			break
		}
		if page == 1 {
			out.state = "complete"
		}
		for _, log := range logs {
			if log.TokenID == "" || log.At < start || log.At >= end || log.ID == "" || seen[log.ID] {
				out.state = "partial"
				continue
			}
			seen[log.ID] = true
			row := out.byID[log.TokenID]
			row.addLog(log, today.Unix())
			out.byID[log.TokenID] = row
			if log.Amount == nil {
				out.state = "partial"
			}
		}
		if !more {
			return out
		}
	}
	if out.state != "unavailable" {
		out.state = "partial"
	}
	return out
}
