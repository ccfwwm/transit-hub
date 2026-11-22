package my_sites

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

// Primary model settings have one owner: the account's monitor rule.
type BindingMonitor interface {
	ConfigureBinding(context.Context, RealConnection, *string, *bool) error
	PopulatePrimaryModels(context.Context, string, string, []RealConnection) error
	BindingModelCatalog(context.Context, upstream.Site, string) ([]string, error)
	MembershipChanged(context.Context, string, string)
}

func (s *Service) SetBindingMonitor(m BindingMonitor) { s.bindingMonitor = m }
func (s *Service) configureBindingModel(ctx context.Context, c *RealConnection, model *string, allow *bool) error {
	if model == nil && allow == nil {
		return nil
	}
	if s.bindingMonitor == nil {
		return fmt.Errorf("监控服务尚未就绪")
	}
	if err := s.bindingMonitor.ConfigureBinding(ctx, *c, model, allow); err != nil {
		return err
	}
	if model != nil {
		c.TestModelID = strings.TrimSpace(*model)
	}
	if allow != nil {
		c.AllowWhenUpstreamRateGteOwn = *allow
	}
	return nil
}
func (s *Service) membershipChanged(ctx context.Context, c RealConnection, _ []string) {
	if s.bindingMonitor != nil {
		s.bindingMonitor.MembershipChanged(ctx, c.UserID, c.WorkspaceAdminAccountID)
	}
}

type BindingModels struct {
	Items   []string `json:"items"`
	Source  string   `json:"source"`
	Warning string   `json:"warning,omitempty"`
}

func (s *Service) BindingModels(ctx context.Context, uid, siteID, groupID, keyID, connectionID string) (BindingModels, error) {
	out := BindingModels{Items: []string{}}
	aid, err := s.currentAdminAccountID(ctx, uid)
	if err != nil {
		return out, err
	}
	site, err := s.upstreamLookup.GetSite(ctx, siteID)
	if err != nil || site == nil || site.UserID != uid || site.AdminAccountID != aid || site.Session == nil {
		return out, requestError(ErrorRequest)
	}
	var key, apiURL string
	if connectionID != "" {
		c, e := s.connRepository.GetRealConnection(ctx, connectionID, uid, aid)
		if e != nil || c == nil || c.UpstreamSiteID != siteID || c.UpstreamGroupID != groupID {
			return out, requestError(ErrorRequest)
		}
		key, apiURL = c.UpstreamKey, c.UpstreamAPIURL
	} else {
		keys, e := s.ListUpstreamKeys(ctx, uid, siteID)
		if e == nil {
			for _, k := range keys {
				if k.GroupID != groupID || keyID != "" && k.ID != keyID {
					continue
				}
				key = k.Key
				if site.Platform == upstream.PlatformNewAPI {
					key, e = s.platformService.FetchNewAPITokenKey(*site.Session, k.ID)
				}
				if e == nil && key != "" {
					break
				}
			}
		}
	}
	if key != "" && !strings.Contains(key, "*") {
		ids, e := s.platformService.FetchAPIKeyModelIDs(ctx, upstream.APIKeyProbeOptions{BaseURL: upstream.EffectiveAPIURL(*site, groupID, apiURL), APIKey: key, InsecureSkipTLS: site.SkipTLSVerify || site.Session.InsecureSkipTLS})
		if e == nil && len(ids) > 0 {
			out.Items, out.Source = ids, "key"
			return out, nil
		}
		out.Warning = "Key 目录暂不可用，已显示分组目录"
	}
	ids, err := s.platformService.FetchGroupModelIDs(ctx, *site, groupID)
	if err == nil && len(ids) > 0 {
		out.Items, out.Source = ids, "group"
		return out, nil
	}
	if s.bindingMonitor != nil {
		ids, err = s.bindingMonitor.BindingModelCatalog(ctx, *site, groupID)
	}
	if err == nil && len(ids) > 0 {
		sort.Strings(ids)
		out.Items, out.Source = ids, "reported"
		return out, nil
	}
	out.Warning = "上游未提供可读取的模型目录，可手动输入模型 ID"
	return out, nil
}
func (h *Handler) bindingModels(w http.ResponseWriter, r *http.Request) {
	uid, ok := authctx.UserID(r.Context())
	if !ok {
		httpjson.WriteError(w, 401, "auth.errors.unauthorized")
		return
	}
	q := r.URL.Query()
	v, e := h.service.BindingModels(r.Context(), uid, q.Get("siteId"), q.Get("groupId"), q.Get("keyId"), q.Get("connectionId"))
	if e != nil {
		writeError(w, e)
		return
	}
	httpjson.Write(w, 200, v)
}
