package channel_monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

type operationRequest struct {
	Waiting     bool              `json:"waiting"`
	PlanVersion string            `json:"planVersion"`
	Kind        string            `json:"kind"`
	IDs         []string          `json:"ids"`
	Filter      *ChannelPageQuery `json:"filter"`
	GroupID     string            `json:"groupId"`
	Priority    int               `json:"priority"`
	Schedulable bool              `json:"schedulable"`
	Replace     bool              `json:"replace"`
	Settings    UpdateRuleRequest `json:"settings"`
}

func (s *Service) SetOperations(j *jobs.Service, p *upstream.KeyPolicies) {
	s.jobs = j
	s.registerCatalogJob(j)
	s.registerMembershipPriority(j)
	s.registerManualModelChecks(j)
	s.registerBindingSyncJob(j)
	s.keyPolicies = p
	j.Register("monitor", func(ctx context.Context, uid, aid, id string, raw json.RawMessage) (jobs.Outcome, error) {
		var req operationRequest
		if e := json.Unmarshal(raw, &req); e != nil {
			return jobs.Outcome{}, e
		}
		ctx = context.WithValue(ctx, readWorkspaceKey{}, aid)
		if req.Kind == "refresh" {
			s.invalidateAccountCache(uid, aid)
			s.invalidateGroupCache(uid, aid)
			return jobs.Outcome{}, s.RefreshReadModel(ctx, uid, aid)
		}
		if req.Kind == "rate_preview" {
			v, e := s.PreviewRateRule(ctx, uid)
			if e != nil {
				return jobs.Outcome{}, e
			}
			v.PlanVersion = ratePlanVersion(v)
			return jobs.Outcome{Value: v}, nil
		}
		if req.Kind == "rate_apply" {
			s.invalidateAccountCache(uid, aid)
			s.invalidateGroupCache(uid, aid)
			v, e := s.PreviewRateRule(ctx, uid)
			if e != nil {
				return jobs.Outcome{}, e
			}
			if req.PlanVersion == "" || req.PlanVersion != ratePlanVersion(v) {
				return jobs.Outcome{}, fmt.Errorf("调度数据已变化，请重新预览后应用")
			}
			v2, e := s.ApplyPriorityRule(ctx, uid, req.GroupID, "manual-priority")
			if e == nil {
				_ = s.RefreshReadModel(ctx, uid, aid)
			}
			return jobs.Outcome{Value: v2}, e
		}
		rule, e := s.requireRule(ctx, id, uid, aid)
		if e != nil {
			return jobs.Outcome{}, e
		}
		var value any
		switch req.Kind {
		case "run":
			rule.MonitorGroupID = req.GroupID
			reason := "manual"
			if req.Waiting {
				reason = "operation_poll"
			}
			value, e = s.runRule(ctx, rule, reason)
			if e == nil {
				if repo, ok := s.store.(*Repository); ok {
					states, err := repo.ListTargetStates(ctx, uid, aid)
					if err != nil {
						return jobs.Outcome{}, err
					}
					pending := false
					for _, state := range states[rule.ID] {
						if req.GroupID != "" && req.GroupID != state.GroupID {
							continue
						}
						if state.Status == StatusGenerating {
							pending = true
						}
						for _, model := range state.Models {
							if model.Status == StatusGenerating {
								pending = true
							}
						}
					}
					if pending {
						req.Waiting = true
						raw, _ := json.Marshal(req)
						return jobs.Outcome{Value: value, Pending: true, Delay: 10 * time.Second, NextPayload: raw}, nil
					}
				}
			}
		case "takeover":
			value, e = s.SyncAndTakeOverRule(ctx, uid, id)
		case "schedulable":
			e = s.SetRuleSchedulable(ctx, uid, id, req.Schedulable)
		case "priority":
			e = s.SetRulePriorityInGroup(ctx, uid, id, req.Priority, req.GroupID)
		case "update":
			value, e = s.UpdateRule(ctx, uid, id, req.Settings)
		case "whitelist":
			conn, err := s.conns.GetRealConnection(ctx, rule.ConnectionID, uid, aid)
			if err != nil {
				return jobs.Outcome{}, err
			}
			if conn == nil {
				return jobs.Outcome{}, fmt.Errorf("绑定不存在")
			}
			site, err := s.upstreams.GetSite(ctx, conn.UpstreamSiteID)
			if err != nil {
				return jobs.Outcome{}, err
			}
			if site == nil {
				return jobs.Outcome{}, fmt.Errorf("上游不存在")
			}
			value, e = p.ApplyForGroup(ctx, uid, aid, *site, conn.UpstreamKeyID, conn.UpstreamGroupID, req.Replace)
		default:
			return jobs.Outcome{}, fmt.Errorf("不支持的操作")
		}
		if e == nil {
			_ = s.RefreshReadModel(ctx, uid, aid)
		}
		return jobs.Outcome{Value: value}, e
	})
}
func (h *Handler) registerOperations(mux *http.ServeMux) {
	h.registerBindingSync(mux)
	mux.HandleFunc("GET /api/channel-monitor/operations", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := h.scope(r)
		if e != nil {
			writeMonitorError(w, e)
			return
		}
		v, e := h.service.jobs.List(r.Context(), uid, aid)
		if e != nil {
			httpjson.WriteError(w, 500, "无法读取任务")
			return
		}
		httpjson.Write(w, 200, v)
	})
	mux.HandleFunc("POST /api/channel-monitor/operations", h.submitOperation)
	mux.HandleFunc("GET /api/channel-monitor/operations/{id}", h.operation)
	mux.HandleFunc("POST /api/channel-monitor/operations/{id}/retry", h.retryOperation)
	mux.HandleFunc("GET /api/channel-monitor/key-policy", h.keyPolicy)
	mux.HandleFunc("PUT /api/channel-monitor/key-policy", h.keyPolicy)
	mux.HandleFunc("GET /api/channel-monitor/page-history", h.pageHistory)
	mux.HandleFunc("GET /api/channel-monitor/stats", h.summary)
	mux.HandleFunc("GET /api/channel-monitor/groups", h.summary)
}
func (h *Handler) scope(r *http.Request) (string, string, error) {
	uid, _ := authctx.UserID(r.Context())
	if uid == "" {
		return "", "", fmt.Errorf("未登录")
	}
	aid, e := h.service.currentAdminAccountID(r.Context(), uid)
	return uid, aid, e
}
func (h *Handler) submitOperation(w http.ResponseWriter, r *http.Request) {
	uid, aid, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	var req operationRequest
	if e = httpjson.Decode(r, &req); e != nil {
		httpjson.WriteError(w, 400, "无效操作")
		return
	}
	if h.service.jobs == nil {
		httpjson.WriteError(w, 503, "任务服务未就绪")
		return
	}
	if req.Filter != nil {
		page, err := h.service.ChannelPage(r.Context(), uid, *req.Filter)
		if err != nil {
			writeMonitorError(w, err)
			return
		}
		req.IDs = page.RuleIDs
	}
	if req.Kind == "refresh" || req.Kind == "rate_preview" || req.Kind == "rate_apply" {
		req.IDs = []string{"workspace"}
	}
	validKinds := map[string]bool{"refresh": true, "run": true, "takeover": true, "schedulable": true, "priority": true, "update": true, "whitelist": true, "rate_preview": true, "rate_apply": true}
	if !validKinds[req.Kind] {
		httpjson.WriteError(w, 400, "不支持的操作")
		return
	}
	ids := uniqueRuleIDs(req.IDs)
	if len(ids) == 0 {
		httpjson.WriteError(w, 400, "请选择操作目标")
		return
	}
	if repo, ok := h.service.store.(*Repository); ok && req.Kind != "refresh" && req.Kind != "rate_preview" && req.Kind != "rate_apply" {
		var count int
		e = repo.db.QueryRow(r.Context(), `SELECT count(*) FROM channel_monitor_rules WHERE user_id=$1 AND admin_account_id=$2 AND id=ANY($3::text[])`, uid, aid, ids).Scan(&count)
		if e != nil || count != len(ids) {
			httpjson.WriteError(w, 404, "操作目标不属于当前工作区")
			return
		}
	} else {
		for _, id := range ids {
			if req.Kind != "refresh" && req.Kind != "rate_preview" && req.Kind != "rate_apply" {
				if _, e = h.service.requireRule(r.Context(), id, uid, aid); e != nil {
					writeMonitorError(w, e)
					return
				}
			}
		}
	}
	items := []jobs.Item{}
	req.IDs = nil
	req.Filter = nil
	raw, _ := json.Marshal(req)
	for _, id := range ids {
		items = append(items, jobs.Item{ID: id, Payload: raw})
	}
	j, e := h.service.jobs.Submit(r.Context(), uid, aid, "monitor", items)
	if e != nil {
		httpjson.WriteError(w, 400, e.Error())
		return
	}
	httpjson.Write(w, 202, j)
}

func ratePlanVersion(v RateRuleView) string {
	raw, _ := json.Marshal(v.Rows)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (h *Handler) operation(w http.ResponseWriter, r *http.Request) {
	uid, aid, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	j, e := h.service.jobs.Get(r.Context(), uid, aid, r.PathValue("id"))
	if e != nil {
		httpjson.WriteError(w, 404, "任务不存在")
		return
	}
	httpjson.Write(w, 200, j)
}
func (h *Handler) retryOperation(w http.ResponseWriter, r *http.Request) {
	uid, aid, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	j, e := h.service.jobs.Retry(r.Context(), uid, aid, r.PathValue("id"))
	if e != nil {
		httpjson.WriteError(w, 404, "任务不存在")
		return
	}
	httpjson.Write(w, 202, j)
}
func (h *Handler) keyPolicy(w http.ResponseWriter, r *http.Request) {
	uid, aid, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	p := h.service.keyPolicies
	if p == nil {
		httpjson.WriteError(w, 503, "白名单服务未就绪")
		return
	}
	var v upstream.KeyPolicy
	if r.Method == http.MethodPut {
		if e = httpjson.Decode(r, &v); e != nil {
			httpjson.WriteError(w, 400, "无效白名单")
			return
		}
		v, e = p.Save(r.Context(), uid, aid, v)
	} else {
		v, e = p.Get(r.Context(), uid, aid)
	}
	if e != nil {
		httpjson.WriteError(w, 400, e.Error())
		return
	}
	httpjson.Write(w, 200, v)
}
func (h *Handler) pageHistory(w http.ResponseWriter, r *http.Request) {
	uid, _, e := h.scope(r)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	q := r.URL.Query()
	res, e := h.service.ChannelPage(r.Context(), uid, ChannelPageQuery{Page: intQuery(q.Get("page"), 1), PageSize: intQuery(q.Get("pageSize"), 25), GroupID: q.Get("groupId"), GroupStatus: q.Get("groupStatus"), Status: q.Get("status"), Dispatch: q.Get("dispatch"), Search: q.Get("q")})
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	ids, groups := []string{}, []string{}
	for _, ch := range res.Channels {
		ids = append(ids, ch.RuleID)
		groups = append(groups, ch.DetectionGroupID)
	}
	repo, ok := h.service.store.(*Repository)
	if !ok {
		httpjson.Write(w, 200, map[string]any{})
		return
	}
	hist, e := repo.PageHistory(r.Context(), ids, groups)
	if e != nil {
		writeMonitorError(w, e)
		return
	}
	httpjson.Write(w, 200, map[string]any{"snapshotVersion": res.SnapshotVersion, "histories": hist, "updatedAt": time.Now()})
}

func intQuery(s string, fallback int) int {
	n, e := strconv.Atoi(s)
	if e != nil {
		return fallback
	}
	return n
}
