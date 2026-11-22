package my_sites

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/httpjson"
)

var usageZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type keyUsagePoint struct {
	Label    string   `json:"label"`
	Requests int      `json:"requests"`
	Amount   *float64 `json:"amount"`
}

type keyLogItem struct {
	Time   int64    `json:"time"`
	Model  string   `json:"model"`
	IP     string   `json:"ip,omitempty"`
	Amount *float64 `json:"amount"`
}

type keyUsageReport struct {
	State  string           `json:"state"`
	Unit   string           `json:"unit"`
	Total  keyUsagePoint    `json:"total"`
	Trend  []*keyUsagePoint `json:"trend"`
	Models []*keyUsagePoint `json:"models"`
	Logs   []keyLogItem     `json:"logs"`
}

func (m *KeyManagement) keyUsage(w http.ResponseWriter, r *http.Request) {
	uid, aid, site := m.requestSite(w, r)
	if site == nil {
		return
	}
	ctx, id := r.Context(), r.PathValue("key")
	q := r.URL.Query()
	interval := q.Get("interval")
	if interval == "" {
		interval = "day"
	}
	if interval != "hour" && interval != "day" && interval != "week" {
		httpjson.WriteError(w, 400, "无效的用量周期")
		return
	}
	now := time.Now().In(usageZone)
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, usageZone)
	start := end.AddDate(0, 0, -7)
	if interval == "hour" {
		start = end.AddDate(0, 0, -1)
	}
	if q.Get("start") != "" || q.Get("end") != "" {
		var err error
		start, err = time.ParseInLocation("2006-01-02", q.Get("start"), usageZone)
		if err != nil {
			httpjson.WriteError(w, 400, "开始日期无效")
			return
		}
		end, err = time.ParseInLocation("2006-01-02", q.Get("end"), usageZone)
		if err != nil {
			httpjson.WriteError(w, 400, "结束日期无效")
			return
		}
		end = end.AddDate(0, 0, 1)
	}
	days := end.Sub(start).Hours() / 24
	if days < 1 || days > 90 || interval == "hour" && days > 7 {
		httpjson.WriteError(w, 400, "请选择有效日期范围，按小时最多 7 天，其余最多 90 天")
		return
	}
	cacheKey := fmt.Sprintf("%s/usage/%s/%s/%d/%d", inventoryCacheKey(site), id, interval, start.Unix(), end.Unix())
	report, err := m.cached(ctx, cacheKey, func(run context.Context) (any, error) {
		return m.readKeyUsage(run, uid, aid, site, id, interval, start, end)
	})
	apiResult(w, report, err)
}

func usageBucket(at time.Time, interval string) string {
	at = at.In(usageZone)
	if interval == "hour" {
		return at.Format("2006-01-02 15:00")
	}
	if interval == "week" {
		at = at.AddDate(0, 0, -((int(at.Weekday()) + 6) % 7))
	}
	return at.Format("2006-01-02")
}

func usageAmount(point *keyUsagePoint, amount *float64) {
	point.Requests++
	if amount != nil {
		if point.Amount == nil {
			point.Amount = new(float64)
		}
		*point.Amount += *amount
	}
}

func (m *KeyManagement) readKeyUsage(ctx context.Context, uid, aid string, site *upstream.Site, id, interval string, start, end time.Time) (keyUsageReport, error) {
	out := keyUsageReport{State: "complete", Unit: "USD", Trend: []*keyUsagePoint{}, Models: []*keyUsagePoint{}, Logs: []keyLogItem{}}
	type queryKey struct{ id, name string }
	queries := []queryKey{{id: id}}
	identities := map[string]bool{id: true}
	rows, err := m.db.Query(ctx, "WITH RECURSIVE chain(id) AS (SELECT $4::text UNION SELECT l.previous_id FROM managed_key_lineage l JOIN chain c ON c.id=l.successor_id WHERE l.user_id=$1 AND l.workspace_id=$2 AND l.site_id=$3) SELECT id FROM chain WHERE id<>$4 ORDER BY id LIMIT 31", uid, aid, site.ID, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var previous string
		if err = rows.Scan(&previous); err != nil {
			rows.Close()
			return out, err
		}
		identities[previous] = true
		queries = append(queries, queryKey{id: previous})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if site.Platform == upstream.PlatformNewAPI {
		out.Unit = "quota"
		record, _, err := m.platform.ReadSecurityKey(ctx, *site.Session, id)
		if err != nil {
			return out, err
		}
		queries = queries[:1]
		queries[0].name = keyString(record["name"])
		rows, err := m.db.Query(ctx, "SELECT key_id,name FROM managed_key_aliases WHERE user_id=$1 AND workspace_id=$2 AND site_id=$3 AND key_id=ANY($4) ORDER BY first_seen DESC LIMIT 31", uid, aid, site.ID, identityIDs(identities))
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var query queryKey
			if err = rows.Scan(&query.id, &query.name); err != nil {
				rows.Close()
				return out, err
			}
			if query.name != queries[0].name {
				queries = append(queries, query)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
	}
	seen := map[string]bool{}
	models := map[string]*keyUsagePoint{}
	trend := map[string]*keyUsagePoint{}
	step := 24 * time.Hour
	if interval == "hour" {
		step = time.Hour
	}
	for at := start; at.Before(end); at = at.Add(step) {
		label := usageBucket(at, interval)
		if trend[label] == nil {
			p := &keyUsagePoint{Label: label}
			trend[label] = p
			out.Trend = append(out.Trend, p)
		}
	}
	fetched := false
	budget := 20
	queried := map[queryKey]bool{}
	for _, query := range queries {
		if site.Platform == upstream.PlatformNewAPI {
			query.id = id
		}
		if queried[query] {
			continue
		}
		queried[query] = true
		for page := 1; budget > 0; page++ {
			budget--
			rows, more, err := m.platform.ManagedKeyUsagePage(ctx, *site.Session, query.id, query.name, start.Unix(), end.Unix(), start.Format("2006-01-02"), end.AddDate(0, 0, -1).Format("2006-01-02"), page)
			if err != nil {
				out.State = "partial"
				break
			}
			fetched = true
			added := 0
			for _, row := range rows {
				if row.TokenID == "" {
					out.State = "partial"
					continue
				}
				if !identities[row.TokenID] {
					continue
				}
				if row.At == 0 || row.ID == "" {
					out.State = "partial"
					continue
				}
				if row.At < start.Unix() || row.At >= end.Unix() || seen[row.ID] {
					continue
				}
				seen[row.ID] = true
				added++
				if row.Amount == nil {
					out.State = "partial"
				}
				point := trend[usageBucket(time.Unix(row.At, 0), interval)]
				usageAmount(point, row.Amount)
				usageAmount(&out.Total, row.Amount)
				model := strings.TrimSpace(row.Model)
				if model == "" {
					model = "—"
				}
				if models[model] == nil {
					models[model] = &keyUsagePoint{Label: model}
				}
				usageAmount(models[model], row.Amount)
				out.Logs = append(out.Logs, keyLogItem{Time: row.At, Model: model, IP: keyLogIP(row.IP), Amount: row.Amount})
			}
			if !more {
				break
			}
			if added == 0 && page > 1 {
				out.State = "partial"
				break
			}
		}
		if budget == 0 {
			out.State = "partial"
			break
		}
	}
	if !fetched {
		out.State = "unavailable"
	}
	if out.State == "complete" {
		if out.Total.Amount == nil {
			out.Total.Amount = new(float64)
		}
		for _, point := range out.Trend {
			if point.Amount == nil {
				point.Amount = new(float64)
			}
		}
	}
	for _, point := range models {
		out.Models = append(out.Models, point)
	}
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].Requests > out.Models[j].Requests })
	sort.Slice(out.Logs, func(i, j int) bool { return out.Logs[i].Time > out.Logs[j].Time })
	if len(out.Logs) > 100 {
		out.Logs = out.Logs[:100]
	}
	return out, nil
}

func identityIDs(ids map[string]bool) []string {
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	return result
}
