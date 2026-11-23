package channel_monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/httpjson"
)

type modelBrowser struct {
	MonitoredModel   string                      `json:"monitoredModel"`
	ModelSummary     *upstream.ModelHealth       `json:"modelSummary,omitempty"`
	ManualChecks     map[string]ManualModelCheck `json:"manualChecks,omitempty"`
	ReportedTotal    int                         `json:"reportedTotal"`
	References       []upstream.ModelHealth      `json:"references"`
	Multiplier       *float64                    `json:"multiplier,omitempty"`
	Items            []upstream.ModelHealth      `json:"items"`
	Total            int                         `json:"total"`
	Source           string                      `json:"source"`
	CatalogUpdatedAt *time.Time                  `json:"catalogUpdatedAt"`
	CatalogError     string                      `json:"catalogError,omitempty"`
}

func (h *Handler) registerModelBrowser(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channel-monitor/status-source", h.statusSource)
	mux.HandleFunc("PATCH /api/channel-monitor/status-source", h.statusSource)
	mux.HandleFunc("GET /api/channel-monitor/rules/{id}/models", h.browseModels)
	mux.HandleFunc("POST /api/channel-monitor/rules/{id}/models/refresh", h.refreshModels)
	mux.HandleFunc("POST /api/channel-monitor/rules/{id}/models/check", h.checkModel)
}
func (h *Handler) statusSource(w http.ResponseWriter, r *http.Request) {
	uid, _, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	if r.Method == http.MethodPatch {
		var req struct {
			Source string `json:"source"`
		}
		if httpjson.Decode(r, &req) != nil || (req.Source != "upstream" && req.Source != "active") {
			httpjson.WriteError(w, 400, "无效状态来源")
			return
		}
		if _, e = h.service.UpdateTestModelConfig(r.Context(), uid, UpdateTestModelConfigRequest{StatusSource: &req.Source}); e != nil {
			writeMonitorError(w, e)
			return
		}
	}
	httpjson.Write(w, 200, map[string]any{"source": upstreamStatusSource})
}
func (s *Service) registerCatalogJob(j *jobs.Service) {
	j.Register("model_catalog", func(ctx context.Context, uid, aid, id string, _ json.RawMessage) (jobs.Outcome, error) {
		repo, ok := s.store.(*Repository)
		if !ok {
			return jobs.Outcome{}, fmt.Errorf("模型目录不可用")
		}
		if _, e := s.requireRule(ctx, id, uid, aid); e != nil {
			return jobs.Outcome{}, e
		}
		conn, e := s.conns.GetRealConnection(ctx, id, uid, aid)
		if e != nil || conn == nil {
			return jobs.Outcome{}, fmt.Errorf("绑定不存在")
		}
		site, e := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
		if e != nil || site == nil {
			return jobs.Outcome{}, fmt.Errorf("上游不存在")
		}
		fetcher, ok := s.platform.(interface {
			FetchKeyModels(context.Context, upstream.Site, my_sites.RealConnection) ([]string, error)
		})
		if !ok {
			return jobs.Outcome{}, fmt.Errorf("目录接口不可用")
		}
		ids, e := fetcher.FetchKeyModels(ctx, *site, *conn)
		fingerprint := configFingerprint(*conn, AccountTestOptions{})
		if e != nil {
			_, _ = repo.db.Exec(ctx, `INSERT INTO channel_monitor_model_catalog(rule_id,user_id,admin_account_id,fingerprint,models,error) VALUES($1,$2,$3,$4,'[]',$5) ON CONFLICT(rule_id) DO UPDATE SET error=EXCLUDED.error`, id, uid, aid, fingerprint, "Key 模型目录暂不可读取")
			return jobs.Outcome{}, e
		}
		raw, _ := json.Marshal(ids)
		_, e = repo.db.Exec(ctx, `INSERT INTO channel_monitor_model_catalog(rule_id,user_id,admin_account_id,fingerprint,models,updated_at) VALUES($1,$2,$3,$4,$5,now()) ON CONFLICT(rule_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,models=EXCLUDED.models,updated_at=now(),error=''`, id, uid, aid, fingerprint, raw)
		return jobs.Outcome{Value: map[string]any{"models": len(ids)}}, e
	})
}
func (h *Handler) refreshModels(w http.ResponseWriter, r *http.Request) {
	uid, aid, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	id := r.PathValue("id")
	if _, e = h.service.requireRule(r.Context(), id, uid, aid); e != nil {
		writeMonitorError(w, e)
		return
	}
	j, e := h.service.jobs.Submit(r.Context(), uid, aid, "model_catalog", []jobs.Item{{ID: id}})
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	httpjson.Write(w, 202, j)
}
func (h *Handler) browseModels(w http.ResponseWriter, r *http.Request) {
	uid, aid, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	id := r.PathValue("id")
	if _, e = h.service.requireRule(r.Context(), id, uid, aid); e != nil {
		writeMonitorError(w, e)
		return
	}
	conn, e := h.service.conns.GetRealConnection(r.Context(), id, uid, aid)
	if e != nil || conn == nil {
		httpjson.WriteError(w, 404, "绑定不存在")
		return
	}
	repo, ok := h.service.store.(*Repository)
	if !ok {
		httpjson.WriteError(w, 503, "模型目录不可用")
		return
	}
	config, e := repo.GetTestModelConfig(r.Context(), uid, aid)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	out := modelBrowser{Items: []upstream.ModelHealth{}, References: []upstream.ModelHealth{}, Source: upstreamStatusSource}
	if rule, err := h.service.requireRule(r.Context(), id, uid, aid); err == nil {
		if state, err := h.service.states.Get(r.Context(), uid, aid); err == nil && config != nil {
			gid := r.URL.Query().Get("monitorGroupId")
			if gid == "" && len(conn.OwnGroupIDs) > 0 {
				gid = conn.OwnGroupIDs[0]
			}
			out.MonitoredModel = effectiveTestOptionsForGroup(rule, *conn, state, *config, gid).ModelID
		}
	}

	if site, err := h.service.upstreams.GetSite(r.Context(), conn.UpstreamSiteID); err == nil && site != nil {
		for _, g := range site.Metrics.Groups {
			if g.ID == conn.UpstreamGroupID && g.Multiplier != nil {
				v := *g.Multiplier * site.RechargeRate
				out.Multiplier = &v
			}
		}
	}
	byModel := map[string]upstream.ModelHealth{}
	var raw []byte
	e = repo.db.QueryRow(r.Context(), `SELECT models,updated_at,error FROM channel_monitor_model_catalog WHERE rule_id=$1 AND user_id=$2 AND admin_account_id=$3 AND fingerprint=$4`, id, uid, aid, configFingerprint(*conn, AccountTestOptions{})).Scan(&raw, &out.CatalogUpdatedAt, &out.CatalogError)
	if e == nil {
		var ids []string
		_ = json.Unmarshal(raw, &ids)
		for _, model := range ids {
			byModel[model] = upstream.ModelHealth{ID: model, Model: model, GroupID: conn.UpstreamGroupID, GroupName: conn.UpstreamGroupName, Status: StatusUnknown, Source: "catalog", Timeline: []upstream.HealthPoint{}}
		}
	}
	var observations []upstream.ModelHealth
	if h.service.health != nil {
		q := r.URL.Query()
		q.Set("pageSize", "100")
		q.Set("groupId", conn.UpstreamGroupID)
		q.Set("groupName", conn.UpstreamGroupName)
		q.Set("includeReferences", "1")
		for page := 1; page <= 100; page++ {
			q.Set("page", fmt.Sprint(page))
			v, err := h.service.health.Page(r.Context(), uid, aid, conn.UpstreamSiteID, q)
			if err != nil {
				writeMonitorError(w, err)
				return
			}
			observations = append(observations, v.Items...)
			for _, m := range v.Items {
				if m.Scope == "channel_reference" {
					out.References = append(out.References, displayChannelReference(m))
					continue
				}
				if m.Scope == "infrastructure" || m.Source == "newapi_kuma" && m.GroupID != conn.UpstreamGroupID {
					out.References = append(out.References, m)
					continue
				}
				previous, exists := byModel[m.Model]
				if !exists || previous.Source == "catalog" || m.CheckedAt != nil && (previous.CheckedAt == nil || m.CheckedAt.After(*previous.CheckedAt)) {
					byModel[m.Model] = m
				}
			}
			if page*100 >= v.Total {
				break
			}
		}
	}
	out.ManualChecks, e = repo.manualChecks(r.Context(), uid, aid, id)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	summary := selectedModelSummary(observations, out.ManualChecks, out.MonitoredModel, conn.UpstreamGroupID)
	out.ModelSummary = &summary
	if out.MonitoredModel != "" {
		if _, exists := byModel[out.MonitoredModel]; !exists {
			byModel[out.MonitoredModel] = upstream.ModelHealth{ID: out.MonitoredModel, Model: out.MonitoredModel, Source: "catalog", Status: StatusUnknown, Timeline: []upstream.HealthPoint{}}
		}
	}
	for _, m := range byModel {
		// A Key catalog advertises callable IDs, not monitoring observations.
		// The status view lists only models the selected source actually reports.
		if m.Source != "catalog" {
			out.ReportedTotal++
		}
		if m.Source == "catalog" && m.Model != out.MonitoredModel && r.URL.Query().Get("includeCatalog") != "1" {
			continue
		}
		if q := strings.ToLower(r.URL.Query().Get("q")); q != "" && !strings.Contains(strings.ToLower(m.Model), q) {
			continue
		}
		out.Items = append(out.Items, m)
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Model < out.Items[j].Model })
	out.Total = len(out.Items)
	httpjson.Write(w, 200, out)
}
