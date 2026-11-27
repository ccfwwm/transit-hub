package my_sites

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

func (s *Service) sub2APIWorkspace(ctx context.Context, userID string) (*State, error) {
	workspace, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return nil, err
	}
	state, err := s.authenticatedState(ctx, userID, workspace)
	if err != nil {
		return nil, err
	}
	if state.Session.Platform != upstream.PlatformSub2API {
		return nil, requestError(ErrorRequest)
	}
	return state, nil
}

func (s *Service) bindingModelSettings(ctx context.Context, userID, id string, update *upstream.ModelAllowlist) (upstream.ForwardingModelSettings, error) {
	if s.connRepository == nil {
		return upstream.ForwardingModelSettings{}, requestError(ErrorRequest)
	}
	state, err := s.sub2APIWorkspace(ctx, userID)
	if err != nil {
		return upstream.ForwardingModelSettings{}, err
	}
	conn, err := s.connRepository.GetRealConnection(ctx, id, userID, state.AdminAccountID)
	if err != nil {
		return upstream.ForwardingModelSettings{}, err
	}
	if conn == nil || !validSub2APIGroupID(conn.AdminAccountID) {
		return upstream.ForwardingModelSettings{}, requestError(ErrorRequest)
	}
	if update == nil {
		return s.platformService.FetchForwardingModelSettings(ctx, state.Session, conn.AdminAccountID)
	}
	if s.keyManagement != nil {
		unlock, err := s.keyManagement.lockBindingMutation(ctx, *conn)
		if err != nil {
			return upstream.ForwardingModelSettings{}, err
		}
		defer unlock()
		fresh, err := s.connRepository.GetRealConnection(ctx, id, userID, state.AdminAccountID)
		if err != nil {
			return upstream.ForwardingModelSettings{}, err
		}
		if fresh == nil || fresh.AdminAccountID != conn.AdminAccountID || fresh.UpstreamKeyID != conn.UpstreamKeyID {
			return upstream.ForwardingModelSettings{}, requestError(ErrorRequest)
		}
	}
	return s.platformService.UpdateForwardingModelSettings(ctx, state.Session, conn.AdminAccountID, *update)
}

func validSub2APIGroupID(id string) bool {
	number, err := strconv.ParseInt(id, 10, 64)
	return err == nil && number > 0 && strconv.FormatInt(number, 10) == id
}

func normalizeAllowlist(input *upstream.ModelAllowlist, foldCase bool) bool {
	if len(input.Models) > 5000 {
		return false
	}
	seen := make(map[string]bool, len(input.Models))
	models := make([]string, 0, len(input.Models))
	for _, model := range input.Models {
		model = strings.TrimSpace(model)
		if len(model) > 256 || strings.ContainsAny(model, "\r\n\x00") {
			return false
		}
		key := model
		if foldCase {
			key = strings.ToLower(key)
		}
		if model != "" && !seen[key] {
			seen[key] = true
			models = append(models, model)
		}
	}
	input.Models = models
	return true
}

func (h *Handler) bindingModelAllowlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var update *upstream.ModelAllowlist
	if r.Method == http.MethodPut {
		var body struct {
			Enabled *bool    `json:"enabled"`
			Models  []string `json:"models"`
		}
		if err := httpjson.Decode(r, &body); err != nil || body.Enabled == nil || body.Models == nil {
			writeError(w, requestError(ErrorRequest))
			return
		}
		update = &upstream.ModelAllowlist{Enabled: *body.Enabled, Models: body.Models}
		if !normalizeAllowlist(update, false) {
			writeError(w, requestError(ErrorRequest))
			return
		}
	}
	settings, err := h.service.bindingModelSettings(r.Context(), userID, r.PathValue("id"), update)
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, settings)
}

func (h *Handler) ownGroupSettings(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	id := r.PathValue("id")
	if !validSub2APIGroupID(id) {
		writeError(w, requestError(ErrorInvalidGroup))
		return
	}
	state, err := h.service.sub2APIWorkspace(r.Context(), userID)
	if err != nil {
		writeError(w, err)
		return
	}
	var settings upstream.Sub2APIGroupSettings
	if r.Method == http.MethodPatch {
		var update upstream.Sub2APIGroupSettings
		if err := httpjson.Decode(r, &update); err != nil || update.Status == "" && update.Allowlist == nil ||
			update.Status != "" && update.Status != "active" && update.Status != "inactive" ||
			update.Allowlist != nil && (!normalizeAllowlist(update.Allowlist, true) || update.Allowlist.Enabled && len(update.Allowlist.Models) == 0) {
			writeError(w, requestError(ErrorRequest))
			return
		}
		settings, err = h.service.platformService.UpdateSub2APIGroupSettings(r.Context(), state.Session, id, update)
	} else {
		settings, err = h.service.platformService.FetchSub2APIGroupSettings(r.Context(), state.Session, id, true)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, settings)
}
