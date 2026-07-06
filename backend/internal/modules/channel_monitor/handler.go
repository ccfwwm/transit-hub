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
	mux.HandleFunc("PATCH /api/channel-monitor/rules/{id}", handler.updateRule)
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
