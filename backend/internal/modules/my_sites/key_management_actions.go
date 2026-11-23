package my_sites

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/httpjson"
)

func (m *KeyManagement) keyInput(w http.ResponseWriter, r *http.Request, platform upstream.Platform, create bool) (map[string]any, string, bool) {
	var input struct {
		Name   string         `json:"name"`
		Custom string         `json:"customKey"`
		Config map[string]any `json:"config"`
	}
	if err := httpjson.Decode(r, &input); err != nil {
		httpjson.WriteError(w, 400, "Key 配置格式无效")
		return nil, "", false
	}
	supported := upstream.Sub2APIKeyFields
	if platform == upstream.PlatformNewAPI {
		supported = upstream.NewAPIKeyFields
	} else if create {
		supported = append(slices.Clone(supported), "expires_in_days")
	}
	for name := range input.Config {
		if !slices.Contains(supported, name) {
			httpjson.WriteError(w, 400, "上游不支持配置字段 "+name)
			return nil, "", false
		}
	}
	if input.Config == nil {
		input.Config = map[string]any{}
	}
	if create {
		delete(input.Config, "status")
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" || platform == upstream.PlatformNewAPI && len(input.Name) > 50 {
			httpjson.WriteError(w, 400, "Key 名称无效或超长")
			return nil, "", false
		}
		input.Config["name"] = input.Name
	} else if input.Name != "" || input.Custom != "" {
		httpjson.WriteError(w, 400, "现有 Key 名称和凭据由对接流程管理")
		return nil, "", false
	}
	if platform == upstream.PlatformNewAPI && input.Custom != "" {
		httpjson.WriteError(w, 400, "New API 由上游生成 Token")
		return nil, "", false
	}
	return input.Config, input.Custom, true
}

func (m *KeyManagement) createKey(w http.ResponseWriter, r *http.Request) {
	_, _, site := m.requestSite(w, r)
	if site == nil {
		return
	}
	config, custom, ok := m.keyInput(w, r, site.Platform, true)
	if !ok {
		return
	}
	if site.Platform == upstream.PlatformNewAPI {
		if _, exists := config["unlimited_quota"]; !exists {
			config["unlimited_quota"] = true
		}
		if _, exists := config["expired_time"]; !exists {
			config["expired_time"] = -1
		}
	}
	ctx := r.Context()
	keys, err := m.platform.SecurityKeys(ctx, *site.Session)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	for _, key := range keys {
		if strings.EqualFold(keyString(key["name"]), keyString(config["name"])) {
			httpjson.WriteError(w, 409, "上游已存在同名 Key")
			return
		}
	}
	id, _, err := m.platform.CreateManagedKey(ctx, *site.Session, config, custom)
	m.invalidate(site)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	record, _, err := m.platform.ReadSecurityKey(ctx, *site.Session, id)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	value, _ := m.platform.KeySecret(ctx, *site.Session, id, record)
	if site.Platform == upstream.PlatformNewAPI && value != "" && !strings.HasPrefix(value, "sk-") {
		value = "sk-" + value
	}
	w.Header().Set("Cache-Control", "no-store")
	httpjson.Write(w, 201, map[string]any{"item": safeKeyEntry(site, record), "createdKey": value})
}

func (m *KeyManagement) detailKey(w http.ResponseWriter, r *http.Request) {
	_, _, site := m.requestSite(w, r)
	if site == nil {
		return
	}
	record, _, err := m.platform.ReadSecurityKey(r.Context(), *site.Session, r.PathValue("key"))
	apiResult(w, safeKeyEntry(site, record), err)
}

func (m *KeyManagement) editKey(w http.ResponseWriter, r *http.Request) {
	uid, aid, site := m.requestSite(w, r)
	if site == nil {
		return
	}
	change, _, ok := m.keyInput(w, r, site.Platform, false)
	if !ok {
		return
	}
	ctx, id := r.Context(), r.PathValue("key")
	lock, err := m.lockOperation(ctx, "key-security|"+site.ID+"|"+id)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	defer lock()
	bound, err := m.isBound(ctx, uid, aid, site.ID, id)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	if bound {
		pending, err := m.pendingKeySplit(ctx, uid, aid, site.ID, id)
		if err != nil || pending {
			httpjson.WriteError(w, 409, "此 Key 有未完成的拆分")
			return
		}
		for _, name := range []string{"group", "group_id", "group_config", "preset_id", "auto_groups", "status"} {
			if _, exists := change[name]; exists {
				httpjson.WriteError(w, 409, "已对接 Key 的分组和状态由对接流程管理")
				return
			}
		}
	}
	record, _, err := m.platform.ReadSecurityKey(ctx, *site.Session, id)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	var override []string
	hasOverride := false
	var whitelistField string
	if site.Platform == upstream.PlatformNewAPI {
		whitelistField = "allow_ips"
	} else {
		whitelistField = "ip_whitelist"
	}
	if value, ok := change[whitelistField]; bound && ok {
		hasOverride = true
		switch values := value.(type) {
		case string:
			override = strings.FieldsFunc(values, func(c rune) bool { return c == '\n' || c == '\r' || c == ',' })
		case []any:
			for _, item := range values {
				str, ok := item.(string)
				if !ok {
					httpjson.WriteError(w, 400, "IP 格式无效")
					return
				}
				override = append(override, str)
			}
		default:
			httpjson.WriteError(w, 400, "IP 格式无效")
			return
		}
		if len(override) > 0 {
			override, err = upstream.NormalizeAllowedIPs(override)
			if err != nil {
				apiResult(w, nil, err)
				return
			}
		}
		if override == nil {
			override = []string{}
		}
		if site.Platform == upstream.PlatformNewAPI {
			change[whitelistField] = strings.Join(override, "\n")
		} else {
			change[whitelistField] = override
		}
	}
	want := maps.Clone(change)
	status, updateStatus := change["status"]
	if site.Platform == upstream.PlatformNewAPI {
		delete(change, "status")
		if len(change) > 0 {
			all := upstream.ManagedKeyConfig(site.Platform, record)
			all["name"] = keyString(record["name"])
			maps.Copy(all, change)
			change = all
		}
	}
	if len(change) > 0 {
		if err = m.platform.ConfigureSecurityKey(ctx, *site.Session, id, change); err != nil {
			apiResult(w, nil, err)
			return
		}
	}
	if site.Platform == upstream.PlatformNewAPI && updateStatus {
		if err = m.platform.UpdateNewAPIKeyStatus(ctx, *site.Session, id, status); err != nil {
			apiResult(w, nil, err)
			return
		}
	}
	if err = m.platform.VerifyManagedKeyConfig(ctx, *site.Session, id, want); err != nil {
		apiResult(w, nil, err)
		return
	}
	if hasOverride {
		policies, ok := m.sites.keyPolicies.(*upstream.KeyPolicies)
		if ok {
			if err = m.platform.VerifyManagedKeyConfig(ctx, *site.Session, id, map[string]any{whitelistField: change[whitelistField]}); err != nil {
				apiResult(w, nil, err)
				return
			}
			if err = policies.SaveKeyIPOverride(ctx, uid, aid, site.ID, id, override); err != nil {
				apiResult(w, nil, fmt.Errorf("上游白名单已保存，自动巡检配置尚待同步，请重试"))
				return
			}
		}
	}
	m.invalidate(site)
	record, _, err = m.platform.ReadSecurityKey(ctx, *site.Session, id)
	apiResult(w, safeKeyEntry(site, record), err)
}

func (m *KeyManagement) isBound(ctx context.Context, uid, aid, site, id string) (bool, error) {
	var exists bool
	err := m.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_key_id=$4)
	OR EXISTS(SELECT 1 FROM managed_binding_progress WHERE user_id=$1 AND workspace_id=$2 AND key_id=$4 AND site_id=$3 AND step<>'done')`, uid, aid, site, id).Scan(&exists)
	return exists, err
}

func (m *KeyManagement) deleteKey(w http.ResponseWriter, r *http.Request) {
	uid, aid, site := m.requestSite(w, r)
	if site == nil {
		return
	}
	if err := m.removeIndependentKey(r.Context(), uid, aid, site, r.PathValue("key")); err != nil {
		if err == errBoundKeyCleanup {
			httpjson.WriteError(w, http.StatusConflict, err.Error())
		} else {
			apiResult(w, nil, err)
		}
		return
	}
	httpjson.Write(w, 200, map[string]bool{"deleted": true})
}

func (m *KeyManagement) testKey(w http.ResponseWriter, r *http.Request) {
	_, _, site := m.requestSite(w, r)
	if site == nil {
		return
	}
	var input struct {
		Model         string `json:"model"`
		Execute       bool   `json:"execute"`
		ModelPlatform string `json:"modelPlatform"`
	}
	if httpjson.Decode(r, &input) != nil {
		httpjson.WriteError(w, 400, "检测参数无效")
		return
	}
	ctx, id := r.Context(), r.PathValue("key")
	record, _, err := m.platform.ReadSecurityKey(ctx, *site.Session, id)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	secret, err := m.platform.KeySecret(ctx, *site.Session, id, record)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	group := upstream.SecurityKeyGroup(site.Platform, record)
	options := upstream.APIKeyProbeOptions{
		BaseURL: upstream.EffectiveAPIURL(*site, group, ""), APIKey: secret,
		InsecureSkipTLS: site.SkipTLSVerify || site.Session.InsecureSkipTLS, ModelID: input.Model, Platform: input.ModelPlatform, Mode: "text",
	}
	if input.Execute {
		if strings.TrimSpace(input.Model) == "" {
			httpjson.WriteError(w, 400, "请选择要实际调用的模型")
			return
		}
		result, err := m.platform.ProbeAPIKey(ctx, options)
		apiResult(w, result, err)
		return
	}
	models, err := m.platform.FetchAPIKeyModelIDs(ctx, options)
	apiResult(w, map[string]any{"models": models}, err)
}

func keyLogIP(value string) string {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return ""
	}
	return ip.String()
}

func (m *KeyManagement) lockOperation(ctx context.Context, key string) (func(), error) {
	conn, err := m.db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", key).Scan(&acquired)
	if err != nil || !acquired {
		conn.Release()
		return nil, fmt.Errorf("该绑定或 Key 正在处理，请稍后重试")
	}
	return func() {
		defer conn.Release()
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock(hashtextextended($1,0))", key)
	}, nil
}
