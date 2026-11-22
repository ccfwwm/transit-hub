package upstream

import (
	"net/http"

	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

// Explicit group configuration wins over the site default. Legacy account
// routes remain intact until an API route is explicitly configured.
func EffectiveAPIURL(site Site, groupID, accountURL string) string {
	if v := site.Settings.GroupAPIURLs[groupID]; v != "" {
		return v
	}
	if site.Settings.DefaultAPIURL != "" {
		return site.Settings.DefaultAPIURL
	}
	if site.Settings.APIURLManaged {
		return site.BaseURL
	}
	if _, configured := site.Settings.GroupAPIURLs[groupID]; configured {
		return site.BaseURL
	}
	if accountURL != "" {
		return accountURL
	}
	return site.BaseURL
}

func (h *Handler) groupAPIURL(w http.ResponseWriter, r *http.Request) {
	uid, _ := authctx.UserID(r.Context())
	aid, e := h.requireWorkspace(r.Context(), uid)
	if e != nil {
		writeWorkspaceError(w, e)
		return
	}
	id, group := r.PathValue("id"), r.URL.Query().Get("groupId")
	site, e := h.service.cache.Get(r.Context(), id)
	if e != nil || site == nil || site.UserID != uid || site.AdminAccountID != aid {
		httpjson.WriteError(w, 404, "上游不存在")
		return
	}
	if group == "" {
		httpjson.WriteError(w, 400, "请选择分组")
		return
	}
	if r.Method == http.MethodPut {
		var req struct {
			APIURL string `json:"apiUrl"`
		}
		if httpjson.Decode(r, &req) != nil {
			httpjson.WriteError(w, 400, "请求格式错误")
			return
		}
		var err error
		req.APIURL, err = NormalizeRoutingURL(req.APIURL)
		if err != nil {
			httpjson.WriteError(w, 400, "请填写有效的 HTTP 或 HTTPS API 地址")
			return
		}
		known := false
		for _, g := range site.Metrics.Groups {
			if g.ID == group {
				known = true
				break
			}
		}
		if !known {
			httpjson.WriteError(w, 400, "上游分组不存在，请刷新分组")
			return
		}
		// Clone the map before editing a cached site snapshot.
		values := map[string]string{}
		for k, v := range site.Settings.GroupAPIURLs {
			values[k] = v
		}
		values[group] = req.APIURL
		site.Settings.GroupAPIURLs = values
		if e = h.service.saveSite(r.Context(), site); e != nil {
			httpjson.WriteError(w, 500, "保存 API 地址失败")
			return
		}
		if e = h.service.cache.Set(r.Context(), site); e != nil {
			httpjson.WriteError(w, 500, "API 地址已保存，请刷新页面")
			return
		}
	}
	httpjson.Write(w, 200, map[string]any{"managementUrl": site.BaseURL, "apiUrl": EffectiveAPIURL(*site, group, ""), "customApiUrl": site.Settings.GroupAPIURLs[group]})
}
