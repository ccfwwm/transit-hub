package settings

import (
	"errors"
	"log"
	"net/http"

	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

type Handler struct {
	service *Service
}

func RegisterRoutes(mux *http.ServeMux, service *Service) {
	handler := &Handler{service: service}
	mux.HandleFunc("GET /api/settings/strategy", handler.getStrategy)
	mux.HandleFunc("PUT /api/settings/strategy", handler.saveStrategy)
	mux.HandleFunc("GET /api/settings/notification-channels", handler.getNotificationChannels)
	mux.HandleFunc("PUT /api/settings/notification-channels", handler.saveNotificationChannels)
	mux.HandleFunc("POST /api/settings/notification-channels/test", handler.testNotificationChannel)
}

func (h *Handler) getStrategy(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	settings, err := h.service.GetStrategy(r.Context(), userID)
	if err != nil {
		if writeWorkspaceError(w, err) {
			return
		}
		log.Printf("get strategy settings: %v", err)
		httpjson.WriteError(w, http.StatusInternalServerError, "admin.settings.errors.request")
		return
	}
	httpjson.Write(w, http.StatusOK, settings)
}

func (h *Handler) saveStrategy(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var dto StrategySettings
	if err := httpjson.Decode(r, &dto); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	settings, err := h.service.SaveStrategy(r.Context(), userID, dto)
	if err != nil {
		if writeWorkspaceError(w, err) {
			return
		}
		log.Printf("save strategy settings: %v", err)
		httpjson.WriteError(w, http.StatusInternalServerError, "admin.settings.errors.request")
		return
	}
	httpjson.Write(w, http.StatusOK, settings)
}

func (h *Handler) getNotificationChannels(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	settings, err := h.service.GetNotificationChannels(r.Context(), userID)
	if err != nil {
		if writeWorkspaceError(w, err) {
			return
		}
		log.Printf("get notification channels: %v", err)
		httpjson.WriteError(w, http.StatusInternalServerError, "admin.settings.errors.request")
		return
	}
	httpjson.Write(w, http.StatusOK, settings)
}

func (h *Handler) saveNotificationChannels(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}
	var dto NotificationChannelSettings
	if err := httpjson.Decode(r, &dto); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	settings, err := h.service.SaveNotificationChannels(r.Context(), userID, dto)
	if err != nil {
		if writeWorkspaceError(w, err) {
			return
		}
		log.Printf("save notification channels: %v", err)
		httpjson.WriteError(w, http.StatusInternalServerError, "admin.settings.errors.request")
		return
	}
	httpjson.Write(w, http.StatusOK, settings)
}

func writeWorkspaceError(w http.ResponseWriter, err error) bool {
	if err != nil && err.Error() == "admin.adminAccounts.errors.noCurrentAccount" {
		httpjson.WriteError(w, http.StatusConflict, err.Error())
		return true
	}
	return false
}

func (h *Handler) testNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if _, ok := authctx.UserID(r.Context()); !ok {
		httpjson.WriteError(w, http.StatusUnauthorized, "auth.errors.unauthorized")
		return
	}

	var dto TestNotificationRequest
	if err := httpjson.Decode(r, &dto); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := h.service.TestNotification(r.Context(), dto); err != nil {
		writeSettingsError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, TestNotificationResponse{Success: true, Message: "admin.settings.sections.channels.testConnectionSuccess"})
}

func writeSettingsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidNotificationChannel), errors.Is(err, ErrMissingWebhook), errors.Is(err, ErrMissingTelegramConfig):
		httpjson.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrSendNotificationFailed):
		log.Printf("send test notification: %v", err)
		httpjson.WriteError(w, http.StatusBadGateway, ErrSendNotificationFailed.Error())
	default:
		log.Printf("test notification: %v", err)
		httpjson.WriteError(w, http.StatusInternalServerError, "admin.settings.errors.request")
	}
}
