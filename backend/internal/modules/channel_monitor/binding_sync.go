package channel_monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/shared/httpjson"
)

type monitorBindingSync interface {
	SyncMonitorBindings(context.Context, string, string, my_sites.MonitorBindingRequest) (my_sites.MonitorBindingResult, error)
}

func (s *Service) SetBindingSync(provider monitorBindingSync) { s.bindingSync = provider }
func (s *Service) registerBindingSyncJob(j *jobs.Service) {
	j.Register("monitor_bindings", func(ctx context.Context, uid, aid, id string, _ json.RawMessage) (jobs.Outcome, error) {
		v, e := s.syncMonitoringBindings(ctx, uid, aid, my_sites.MonitorBindingRequest{})
		return jobs.Outcome{Value: v}, e
	})
}
func (s *Service) syncMonitoringBindings(ctx context.Context, uid, aid string, req my_sites.MonitorBindingRequest) (my_sites.MonitorBindingResult, error) {
	if s.bindingSync == nil {
		return my_sites.MonitorBindingResult{}, fmt.Errorf("监控同步尚未就绪")
	}
	result, e := s.bindingSync.SyncMonitorBindings(ctx, uid, aid, req)
	if e != nil {
		return result, e
	}
	ctx = context.WithValue(ctx, readWorkspaceKey{}, aid)
	for i := range result.Items {
		item := &result.Items[i]
		if item.ConnectionID == "" {
			continue
		}
		conn, e := s.conns.GetRealConnection(ctx, item.ConnectionID, uid, aid)
		if e != nil || conn == nil {
			item.Status = "failed"
			item.Message = "绑定读取失败"
			continue
		}
		rule, e := s.store.EnsureRuleForConnection(ctx, uid, aid, *conn)
		if e != nil {
			item.Status = "failed"
			item.Message = "监控规则创建失败"
			continue
		}
		if item.NeedsTakeover {
			// Imported accounts retain their original account/Key and remote groups.
			// Follow the existing takeover gate until the configured model recovers.
			state, e := s.workspaceState(ctx, uid, aid)
			if e != nil {
				item.Status = "failed"
				item.Message = "自有站会话不可用"
				continue
			}
			unlock, e := s.lockRuleAccount(ctx, rule)
			if e != nil {
				return result, e
			}
			accounts, e := s.platform.ListSub2APIAdminAccounts(state.Session)
			if e == nil {
				_, e = s.syncAndTakeOverRule(ctx, rule, state, conn, accounts)
			}
			unlock()
			if e != nil {
				item.Status = "failed"
				item.Message = "已绑定，但接管失败，请重试同步接管"
			} else {
				item.Message = "已同步并接管"
			}
		}
	}
	s.invalidateAccountCache(uid, aid)
	s.invalidateGroupCache(uid, aid)
	if e = s.RefreshReadModel(ctx, uid, aid); e != nil {
		return result, e
	}
	return result, nil
}
func (h *Handler) registerBindingSync(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/channel-monitor/bindings/sync", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := h.scope(r)
		if e != nil {
			writeMonitorError(w, e)
			return
		}
		job, e := h.service.jobs.Submit(r.Context(), uid, aid, "monitor_bindings", []jobs.Item{{ID: "workspace"}})
		if e != nil {
			writeMonitorError(w, e)
			return
		}
		httpjson.Write(w, 202, job)
	})
	mux.HandleFunc("POST /api/channel-monitor/bindings/key", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := h.scope(r)
		if e != nil {
			writeMonitorError(w, e)
			return
		}
		var req my_sites.MonitorBindingRequest
		if httpjson.Decode(r, &req) != nil || req.APIURL == "" || req.APIKey == "" {
			httpjson.WriteError(w, 400, "请填写 API 地址和 Key")
			return
		}
		v, e := h.service.syncMonitoringBindings(r.Context(), uid, aid, req)
		if e != nil {
			httpjson.WriteError(w, 400, e.Error())
			return
		}
		httpjson.Write(w, 200, v)
	})
}
