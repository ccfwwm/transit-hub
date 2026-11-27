package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type ModelAllowlist struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models"`
}

type ModelSettingsError string

func (e ModelSettingsError) Error() string { return string(e) }

type Sub2APIGroupSettings struct {
	Status     string          `json:"status"`
	Allowlist  *ModelAllowlist `json:"allowlist,omitempty"`
	Candidates []string        `json:"candidates,omitempty"`
}

func (s *PlatformService) sub2APIAdminGroupRequest(ctx context.Context, session Session, id, suffix, method string, body any) (map[string]any, error) {
	options := sub2APIRequestOptions(session)
	options.Context, options.Method, options.Body = ctx, method, body
	response, err := s.httpClient.requestJSON(session.BaseURL+"/api/v1/admin/groups/"+url.PathEscape(id)+suffix, options)
	if err != nil {
		return nil, err
	}
	return dataRecord(response.Payload), nil
}

func (s *PlatformService) FetchSub2APIGroupSettings(ctx context.Context, session Session, id string, includeCandidates bool) (Sub2APIGroupSettings, error) {
	record, err := s.sub2APIAdminGroupRequest(ctx, session, id, "", http.MethodGet, nil)
	if err != nil {
		return Sub2APIGroupSettings{}, err
	}
	if groupID2(record) != id {
		return Sub2APIGroupSettings{}, ModelSettingsError("自有分组身份不一致")
	}
	result := Sub2APIGroupSettings{Status: safeString(record, "status")}
	if raw, ok := record["model_allowlist"].(map[string]any); ok {
		result.Allowlist = &ModelAllowlist{Enabled: raw["enabled"] == true, Models: modelStrings(raw["models"])}
	}
	if includeCandidates && result.Allowlist != nil {
		if candidates, err := s.sub2APIAdminGroupRequest(ctx, session, id, "/model-allowlist-candidates", http.MethodGet, nil); err == nil {
			result.Candidates = modelStrings(candidates["models"])
		}
	}
	return result, nil
}

func (s *PlatformService) UpdateSub2APIGroupSettings(ctx context.Context, session Session, id string, settings Sub2APIGroupSettings) (Sub2APIGroupSettings, error) {
	if settings.Allowlist != nil {
		current, err := s.FetchSub2APIGroupSettings(ctx, session, id, false)
		if err != nil {
			return Sub2APIGroupSettings{}, err
		}
		if current.Allowlist == nil {
			return Sub2APIGroupSettings{}, ModelSettingsError("当前 Sub2API 不支持分组模型白名单")
		}
	}
	body := map[string]any{}
	if settings.Status != "" {
		body["status"] = settings.Status
	}
	if settings.Allowlist != nil {
		body["model_allowlist"] = settings.Allowlist
	}
	if _, err := s.sub2APIAdminGroupRequest(ctx, session, id, "", http.MethodPut, body); err != nil {
		return Sub2APIGroupSettings{}, err
	}
	saved, err := s.FetchSub2APIGroupSettings(ctx, session, id, false)
	if err != nil {
		return Sub2APIGroupSettings{}, err
	}
	if settings.Status != "" && settings.Status != saved.Status ||
		settings.Allowlist != nil && (saved.Allowlist == nil || saved.Allowlist.Enabled != settings.Allowlist.Enabled || !slices.Equal(saved.Allowlist.Models, settings.Allowlist.Models)) {
		return Sub2APIGroupSettings{}, ModelSettingsError("Sub2API 分组设置回读不一致")
	}
	return saved, nil
}

func modelStrings(value any) []string {
	items, _ := value.([]any)
	models := make([]string, 0, len(items))
	for _, item := range items {
		if model, ok := item.(string); ok && model != "" {
			models = append(models, model)
		}
	}
	return models
}

type ForwardingModelSettings struct {
	ModelAllowlist
	MediaModels *[]string `json:"mediaModels,omitempty"`
}

func (s *PlatformService) FetchForwardingModelSettings(ctx context.Context, session Session, id string) (ForwardingModelSettings, error) {
	record, err := s.forwardingAccountRecord(ctx, session, id)
	if err != nil {
		return ForwardingModelSettings{}, ModelSettingsError(securityRequestError("读取 Sub2API 转发账号模型设置失败", err).Error())
	}
	return forwardingModelSettings(record)
}

func (s *PlatformService) UpdateForwardingModelSettings(ctx context.Context, session Session, id string, input ModelAllowlist) (ForwardingModelSettings, error) {
	record, err := s.forwardingAccountRecord(ctx, session, id)
	if err != nil {
		return ForwardingModelSettings{}, ModelSettingsError(securityRequestError("读取 Sub2API 转发账号模型设置失败", err).Error())
	}
	current, err := forwardingModelSettings(record)
	if err != nil {
		return ForwardingModelSettings{}, err
	}
	credentials, _ := record["credentials"].(map[string]any)
	extra, _ := record["extra"].(map[string]any)
	body := make(map[string]any)
	models := slices.Clone(input.Models)
	if current.MediaModels != nil {
		for _, model := range models {
			if !slices.Contains(*current.MediaModels, model) {
				return ForwardingModelSettings{}, ModelSettingsError(fmt.Sprintf("媒体模型 %q 尚未在 Sub2API 账号中配置协议", model))
			}
		}
		caps, _ := extra["media_models"].([]any)
		if len(caps) == 0 {
			return ForwardingModelSettings{}, ModelSettingsError("请先在 Sub2API 转发账号中配置媒体模型和协议模板")
		}
		for _, item := range caps {
			capability := item.(map[string]any)
			capability["enabled"] = slices.Contains(models, safeString(capability, "model"))
		}
		body["extra"] = extra
		input.Enabled = true
	} else {
		if input.Enabled && len(models) == 0 {
			return ForwardingModelSettings{}, ModelSettingsError("启用账号模型白名单时至少选择一个模型")
		}
		passthrough := openAIPassthrough(record, extra)
		if input.Enabled || !passthrough {
			existing, _ := credentials["model_mapping"].(map[string]any)
			mapping := make(map[string]string, len(models))
			if input.Enabled {
				for _, model := range models {
					target := safeString(existing, model)
					if target == "" {
						target = model
					}
					mapping[model] = target
				}
			} else {
				models = nil
			}
			credentials["model_mapping"] = mapping
			body["credentials"] = credentials
		}
		if passthrough {
			if extra == nil {
				extra = make(map[string]any)
			}
			extra["openai_passthrough_model_filter"] = input.Enabled
			body["extra"] = extra
		}
	}
	options := sub2APIRequestOptions(session)
	options.Context, options.Method, options.Body = ctx, http.MethodPut, body
	if _, err := s.httpClient.requestJSON(session.BaseURL+"/api/v1/admin/accounts/"+url.PathEscape(id), options); err != nil {
		return ForwardingModelSettings{}, ModelSettingsError(securityRequestError("Sub2API 拒绝保存转发账号模型设置，请检查模型和协议配置", err).Error())
	}
	saved, err := s.FetchForwardingModelSettings(ctx, session, id)
	if err != nil {
		return ForwardingModelSettings{}, err
	}
	slices.Sort(models)
	checkModels := current.MediaModels != nil || input.Enabled || !openAIPassthrough(record, extra)
	if saved.Enabled != input.Enabled || checkModels && !slices.Equal(saved.Models, models) {
		return ForwardingModelSettings{}, ModelSettingsError("Sub2API 账号模型设置回读不一致")
	}
	return saved, nil
}

func forwardingModelSettings(record map[string]any) (ForwardingModelSettings, error) {
	out := ForwardingModelSettings{ModelAllowlist: ModelAllowlist{Models: []string{}}}
	if safeString(record, "type") != "apikey" {
		return out, ModelSettingsError("仅支持 API Key 类型的 Sub2API 转发账号")
	}
	credentials, ok := record["credentials"].(map[string]any)
	if !ok {
		return out, ModelSettingsError("Sub2API 未返回可编辑的转发账号配置")
	}
	extra, _ := record["extra"].(map[string]any)
	if safeString(record, "platform") == "imagevideo" {
		models := []string{}
		out.Enabled, out.MediaModels = true, &models
		if raw := extra["media_models"]; raw != nil {
			capabilities, ok := raw.([]any)
			if !ok {
				return out, ModelSettingsError("Sub2API 媒体模型配置格式无效")
			}
			for _, item := range capabilities {
				capability, _ := item.(map[string]any)
				model := safeString(capability, "model")
				if model == "" {
					return out, ModelSettingsError("Sub2API 媒体模型配置缺少模型 ID")
				}
				models = append(models, model)
				if capability["enabled"] == true {
					out.Models = append(out.Models, model)
				}
			}
		}
		slices.Sort(models)
	} else {
		mapping, _ := credentials["model_mapping"].(map[string]any)
		for model := range mapping {
			if safeString(mapping, model) != "" {
				out.Models = append(out.Models, model)
			}
		}
		out.Enabled = len(out.Models) > 0 && (!openAIPassthrough(record, extra) || extra["openai_passthrough_model_filter"] == true)
	}
	slices.Sort(out.Models)
	return out, nil
}

func openAIPassthrough(record, extra map[string]any) bool {
	if !strings.EqualFold(safeString(record, "platform"), "openai") {
		return false
	}
	if enabled, ok := extra["openai_passthrough"].(bool); ok {
		return enabled
	}
	return extra["openai_oauth_passthrough"] == true
}
