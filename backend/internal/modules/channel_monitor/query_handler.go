package channel_monitor

import (
	"net/http"
	"strconv"

	media "transithub/backend/internal/modules/monitor_media"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

func (h *Handler) channels(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	response, err := h.service.ChannelPage(r.Context(), userID, ChannelPageQuery{Page: page, PageSize: size, GroupID: q.Get("groupId"), GroupStatus: q.Get("groupStatus"), Status: q.Get("status"), Search: q.Get("q"), RuleID: q.Get("ruleId")})
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, response)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	adminID, err := h.service.currentAdminAccountID(r.Context(), userID)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	rule, err := h.service.requireRule(r.Context(), r.PathValue("id"), userID, adminID)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	store, ok := h.service.store.(*Repository)
	if !ok {
		httpjson.Write(w, http.StatusOK, []Result{})
		return
	}
	response, err := store.TargetHistory(r.Context(), rule.ID, r.URL.Query().Get("groupId"), 60)
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, response)
}

func (h *Handler) mediaPresets(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, http.StatusOK, media.Presets())
}
