package channel_monitor

import (
	"errors"
	"net/http"
	"strings"

	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

type Handler struct {
	service *Service
}

func RegisterRoutes(mux *http.ServeMux, service *Service) {
	handler := &Handler{service: service}
	mux.HandleFunc("GET /api/channel-monitor/summary", handler.summary)
	mux.HandleFunc("POST /api/channel-monitor/rules/{id}/run", handler.runRule)
	mux.HandleFunc("POST /api/channel-monitor/rules/{id}/pause", handler.pauseRule)
	mux.HandleFunc("POST /api/channel-monitor/rules/{id}/resume", handler.resumeRule)
	mux.HandleFunc("POST /api/channel-monitor/rules/{id}/schedulable", handler.setSchedulable)
	mux.HandleFunc("POST /api/channel-monitor/rules/{id}/priority", handler.setPriority)
	mux.HandleFunc("PATCH /api/channel-monitor/rules/{id}", handler.updateRule)
	mux.HandleFunc("PATCH /api/channel-monitor/rules/bulk", handler.bulkUpdateRules)
	mux.HandleFunc("POST /api/channel-monitor/rules/bulk/run", handler.bulkRunRules)
	mux.HandleFunc("POST /api/channel-monitor/rules/bulk/schedulable", handler.bulkSetSchedulable)
	mux.HandleFunc("GET /api/channel-monitor/rate-rule", handler.rateRule)
	mux.HandleFunc("PATCH /api/channel-monitor/rate-rule", handler.updateRateRule)
	mux.HandleFunc("POST /api/channel-monitor/rate-rule/preview", handler.previewRateRule)
	mux.HandleFunc("POST /api/channel-monitor/rate-rule/apply", handler.applyRateRule)
	mux.HandleFunc("PATCH /api/channel-monitor/test-model-config", handler.updateTestModelConfig)
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	response, err := h.service.Summary(r.Context(), userID)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, response)
}

func (h *Handler) runRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	result, err := h.service.RunRuleForUser(r.Context(), userID, r.PathValue("id"), "manual")
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, result)
}

func (h *Handler) pauseRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	rule, err := h.service.PauseRule(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, rule)
}

func (h *Handler) resumeRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	result, err := h.service.ResumeRule(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, result)
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var req UpdateRuleRequest
	if err := httpjson.Decode(r, &req); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "admin.channelMonitor.errors.request")
		return
	}
	rule, err := h.service.UpdateRule(r.Context(), userID, r.PathValue("id"), req)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, rule)
}

func (h *Handler) setSchedulable(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var req SetSchedulableRequest
	if err := httpjson.Decode(r, &req); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "admin.channelMonitor.errors.request")
		return
	}
	if err := h.service.SetRuleSchedulable(r.Context(), userID, r.PathValue("id"), req.Schedulable); err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) setPriority(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var req SetPriorityRequest
	if err := httpjson.Decode(r, &req); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "admin.channelMonitor.errors.request")
		return
	}
	if err := h.service.SetRulePriority(r.Context(), userID, r.PathValue("id"), req.Priority); err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) bulkUpdateRules(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var req BulkUpdateRuleRequest
	if err := httpjson.Decode(r, &req); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "admin.channelMonitor.errors.request")
		return
	}
	rules, err := h.service.BulkUpdateRules(r.Context(), userID, req)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, rules)
}

func (h *Handler) bulkRunRules(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var req BulkRunRequest
	if err := httpjson.Decode(r, &req); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "admin.channelMonitor.errors.request")
		return
	}
	results, err := h.service.BulkRunRules(r.Context(), userID, req)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, results)
}

func (h *Handler) bulkSetSchedulable(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var req BulkSchedulableRequest
	if err := httpjson.Decode(r, &req); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "admin.channelMonitor.errors.request")
		return
	}
	if err := h.service.BulkSetSchedulable(r.Context(), userID, req); err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) rateRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	response, err := h.service.RateRuleView(r.Context(), userID)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, response)
}

func (h *Handler) updateRateRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var req UpdateRateRuleRequest
	if err := httpjson.Decode(r, &req); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "admin.channelMonitor.errors.request")
		return
	}
	rule, err := h.service.UpdateRateRule(r.Context(), userID, req)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, rule)
}

func (h *Handler) previewRateRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	response, err := h.service.PreviewRateRule(r.Context(), userID)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, response)
}

func (h *Handler) applyRateRule(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	result, err := h.service.ApplyRateRule(r.Context(), userID, "manual")
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, result)
}

func (h *Handler) updateTestModelConfig(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var req UpdateTestModelConfigRequest
	if err := httpjson.Decode(r, &req); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "admin.channelMonitor.errors.request")
		return
	}
	config, err := h.service.UpdateTestModelConfig(r.Context(), userID, req)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, config)
}

func writeMonitorError(w http.ResponseWriter, err error) {
	var requestErr requestError
	if errors.As(err, &requestErr) {
		key := requestErr.Error()
		status := http.StatusBadRequest
		if strings.Contains(key, "noCurrentAccount") {
			status = http.StatusConflict
		}
		if strings.Contains(key, "notFound") {
			status = http.StatusNotFound
		}
		httpjson.WriteError(w, status, key)
		return
	}
	httpjson.WriteError(w, http.StatusInternalServerError, "admin.channelMonitor.errors.unknown")
}
