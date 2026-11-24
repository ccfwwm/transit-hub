package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"transithub/backend/internal/modules/jobs"
	media "transithub/backend/internal/modules/monitor_media"
	"transithub/backend/internal/shared/httpjson"
)

const manualModelJob = "manual_upstream_model"

type manualModelSelection struct {
	GroupID      string `json:"groupId"`
	KeyID        string `json:"keyId"`
	ConnectionID string `json:"connectionId"`
	Model        string `json:"model"`
	Platform     string `json:"platform"`
	Mode         string `json:"mode"`
	Adapter      string `json:"adapter"`
	Seconds      int    `json:"seconds"`
	CatalogOnly  bool   `json:"catalogOnly"`
}

type manualModelTask struct {
	Selection  manualModelSelection
	SiteID     string
	PreviewID  string
	Phase      string
	KeyID      string
	TaskID     string
	Result     manualModelResult
	StartedAt  time.Time
	FinishedAt time.Time
	Probe      *APIKeyProbeOptions
	Draft      *manualModelDraft
}

type manualModelDraft struct {
	Session  Session
	Settings SiteSettings
	SkipTLS  bool
}

type BoundModelTest struct {
	UserID, WorkspaceID, ConnectionID, SiteID, KeyID, ID string
	Model, Mode, Adapter                                 string
	LatencyMS                                            int
	CheckedAt                                            time.Time
}

type manualModelResult struct {
	APIKeyProbeResult
	Models []string `json:"models,omitempty"`
}

func (s *HealthService) manualModelSite(ctx context.Context, uid, aid, siteID, previewID string) (*Site, error) {
	if previewID == "" {
		return s.ownedSite(ctx, uid, aid, siteID)
	}
	data, view, err := s.preview(ctx, uid, aid, previewID)
	if err != nil || data.Login == nil {
		return nil, fmt.Errorf("请先完成连接预览")
	}
	if view.CommittedSiteID != "" {
		return s.ownedSite(ctx, uid, aid, view.CommittedSiteID)
	}
	return &Site{ID: previewID, UserID: uid, AdminAccountID: aid, BaseURL: data.Login.Session.BaseURL,
		Platform: data.Login.Platform, Session: &data.Login.Session, Metrics: data.Login.Metrics,
		SkipTLSVerify: data.Request.SkipTLSVerify, Settings: SiteSettings{DefaultAPIURL: data.Request.APIURL}}, nil
}

func (s *HealthService) manualConnection(ctx context.Context, uid, aid string, site *Site, sel manualModelSelection) (string, string, error) {
	if sel.ConnectionID == "" {
		return sel.KeyID, "", nil
	}
	var keyID, baseURL string
	err := s.db.QueryRow(ctx, `SELECT upstream_key_id,upstream_api_url FROM real_connections
	WHERE id=$1 AND user_id=$2 AND workspace_admin_account_id=$3 AND upstream_site_id=$4 AND upstream_group_id=$5`,
		sel.ConnectionID, uid, aid, site.ID, sel.GroupID).Scan(&keyID, &baseURL)
	if err != nil || keyID == "" {
		return "", "", fmt.Errorf("当前渠道的绑定 Key 不可用")
	}
	return keyID, baseURL, nil
}

func (s *HealthService) manualModelCatalog(w http.ResponseWriter, r *http.Request, uid, aid string, site *Site, sel manualModelSelection) {
	ctx := r.Context()
	keyID, accountURL, err := s.manualConnection(ctx, uid, aid, site, sel)
	if err != nil {
		httpjson.WriteError(w, 400, err.Error())
		return
	}
	keys := []map[string]string{}
	models := []string{}
	var warning string
	switch {
	case keyID != "":
		record, _, err := s.platform.ReadSecurityKey(ctx, *site.Session, keyID)
		if err != nil || sel.GroupID != "" && !SecurityKeyGroupMatches(site.Platform, record, sel.GroupID) {
			warning = "读取所选 Key 失败，可手动输入模型 ID"
			break
		}
		secret, err := s.platform.KeySecret(ctx, *site.Session, keyID, record)
		if err == nil {
			models, err = s.platform.FetchAPIKeyModelIDs(ctx, APIKeyProbeOptions{APIKey: secret,
				BaseURL: EffectiveAPIURL(*site, SecurityKeyGroup(site.Platform, record), accountURL), InsecureSkipTLS: site.SkipTLSVerify || site.Session.InsecureSkipTLS})
		}
		if err != nil {
			warning = "所选 Key 无法读取模型目录，可手动输入模型 ID"
		}
	default:
		records, err := s.platform.SecurityKeys(ctx, *site.Session)
		if err != nil {
			warning = "无法读取该分组的已有 Key"
		}
		for _, record := range records {
			if id := SecurityKeyID(record); id != "" && !strings.HasPrefix(safeString(record, "name"), "THcheck-") && SecurityKeyGroupMatches(site.Platform, record, sel.GroupID) {
				keys = append(keys, map[string]string{"id": id, "name": safeString(record, "name")})
			}
		}
		// On a new site, the observed model list is available before a Key exists.
		seen := map[string]bool{}
		for page := 1; page <= 10; page++ {
			args := r.URL.Query()
			args.Set("pageSize", "100")
			args.Set("page", strconv.Itoa(page))
			health, err := s.Page(ctx, uid, aid, site.ID, args)
			if err != nil {
				break
			}
			for _, row := range health.Items {
				if row.GroupID == sel.GroupID && row.Scope != "infrastructure" && row.Scope != "channel_reference" && row.Model != "" && row.Model != "__other__" && !seen[row.Model] {
					seen[row.Model] = true
					models = append(models, row.Model)
				}
			}
			if page*100 >= health.Total {
				break
			}
		}
		sort.Strings(models)
	}
	httpjson.Write(w, 200, map[string]any{"keys": keys, "models": models, "warning": warning})
}

func (s *HealthService) registerManualModelTest(mux *http.ServeMux, runner *jobs.Service, policies *KeyPolicies) {
	runner.Register(manualModelJob, func(ctx context.Context, uid, aid, id string, _ json.RawMessage) (jobs.Outcome, error) {
		return s.runManualModelTest(ctx, uid, aid, id, policies)
	})
	for _, endpoint := range []struct {
		pattern string
		preview bool
	}{
		{"/api/upstream-sites/{id}/manual-model-test", false},
		{"/api/upstream-previews/{id}/manual-model-test", true},
	} {
		handler := func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			uid, aid, err := s.scope(r)
			if err != nil {
				httpjson.WriteError(w, 401, "未登录")
				return
			}
			task := manualModelTask{}
			if endpoint.preview {
				task.PreviewID = r.PathValue("id")
			} else {
				task.SiteID = r.PathValue("id")
			}
			site, err := s.manualModelSite(ctx, uid, aid, task.SiteID, task.PreviewID)
			if err != nil || site.Session == nil {
				httpjson.WriteError(w, 404, "上游未连接或预览已过期")
				return
			}
			if r.Method == http.MethodGet {
				query := r.URL.Query()
				s.manualModelCatalog(w, r, uid, aid, site, manualModelSelection{GroupID: query.Get("groupId"), KeyID: query.Get("keyId"), ConnectionID: query.Get("connectionId")})
				return
			}
			if httpjson.Decode(r, &task.Selection) != nil || !validateManualModelSelection(site, &task.Selection) {
				httpjson.WriteError(w, 400, "请选择有效的分组、模型和调用协议")
				return
			}
			if _, _, err = s.manualConnection(ctx, uid, aid, site, task.Selection); err != nil {
				httpjson.WriteError(w, 400, err.Error())
				return
			}
			if endpoint.preview && site.ID == task.PreviewID {
				task.Draft = &manualModelDraft{Session: *site.Session, Settings: site.Settings, SkipTLS: site.SkipTLSVerify}
			}
			id, err := randomID()
			if err == nil {
				task.Phase, task.StartedAt = "new", time.Now().UTC()
				var sealed string
				sealed, err = s.seal(task)
				if err == nil {
					_, err = s.db.Exec(ctx, `INSERT INTO upstream_preview_probes(id,preview_id,user_id,admin_account_id,encrypted) VALUES($1,$2,$3,$4,$5)`, id, site.ID, uid, aid, sealed)
				}
			}
			if err != nil {
				httpjson.WriteError(w, 500, "无法保存模型测试任务")
				return
			}
			job, err := runner.Submit(ctx, uid, aid, manualModelJob, []jobs.Item{{ID: id}})
			if err != nil {
				_, _ = s.db.Exec(ctx, `DELETE FROM upstream_preview_probes WHERE id=$1 AND user_id=$2 AND admin_account_id=$3`, id, uid, aid)
				httpjson.WriteError(w, 500, "无法提交模型测试任务")
				return
			}
			httpjson.Write(w, 202, job)
		}
		mux.HandleFunc("GET "+endpoint.pattern, handler)
		mux.HandleFunc("POST "+endpoint.pattern, handler)
	}
}

func validateManualModelSelection(site *Site, selection *manualModelSelection) bool {
	selection.Model = strings.TrimSpace(selection.Model)
	if selection.Seconds < 0 || selection.Seconds > 300 {
		return false
	}
	if !selection.CatalogOnly && (selection.Model == "" || len(selection.Model) > 200 || strings.ContainsAny(selection.Model, "\r\n\x00")) {
		return false
	}
	if selection.CatalogOnly && (selection.GroupID == "" || selection.KeyID != "" || selection.ConnectionID != "") {
		return false
	}
	if selection.KeyID != "" && selection.ConnectionID != "" {
		return false
	}
	if selection.GroupID != "" {
		found := false
		for _, group := range site.Metrics.Groups {
			if group.ID == selection.GroupID {
				found = true
				if selection.Platform == "" && group.Platform != nil {
					selection.Platform = *group.Platform
				}
				break
			}
		}
		if !found && selection.KeyID == "" && selection.ConnectionID == "" {
			return false
		}
	} else if selection.KeyID == "" && selection.ConnectionID == "" {
		return false
	}
	if selection.Platform == "" {
		selection.Platform = "openai"
	}
	switch selection.Mode {
	case "", "auto":
		selection.Mode = "text"
		if preset, ok := media.ModelPreset(selection.Model); ok {
			selection.Mode, selection.Adapter = preset.MediaType, preset.Adapter
		}
	case "text", "image", "video", "audio":
	default:
		return false
	}
	if selection.Adapter == "" && selection.Mode != "text" {
		if _, known := media.ModelPreset(selection.Model); !known {
			switch selection.Mode {
			case "image":
				selection.Adapter = "openai_image"
				if normalizeGroupPlatform(selection.Platform) == "gemini" {
					selection.Adapter = "gemini_image"
				}
			case "video":
				selection.Adapter = "openai_video"
			case "audio":
				selection.Adapter = "openai_tts"
			}
		}
	}
	if selection.CatalogOnly || selection.Mode == "text" {
		return selection.Adapter == "" && selection.Seconds == 0
	}
	_, _, err := media.Resolve(media.Options{Model: selection.Model, Mode: selection.Mode, Adapter: selection.Adapter, Seconds: selection.Seconds})
	return err == nil
}

func (s *HealthService) runManualModelTest(ctx context.Context, uid, aid, id string, policies *KeyPolicies) (jobs.Outcome, error) {
	var task manualModelTask
	var sealed string
	if err := s.db.QueryRow(ctx, `SELECT encrypted FROM upstream_preview_probes WHERE id=$1 AND user_id=$2 AND admin_account_id=$3`, id, uid, aid).Scan(&sealed); err != nil {
		return jobs.Outcome{}, err
	}
	if err := s.open(sealed, &task); err != nil {
		return jobs.Outcome{}, err
	}
	if task.Phase == "done" {
		return jobs.Outcome{Value: task.Result}, nil
	}
	site, err := s.manualModelSite(ctx, uid, aid, task.SiteID, task.PreviewID)
	if err != nil && task.Draft != nil && time.Since(task.StartedAt) < time.Hour {
		draft := task.Draft
		site = &Site{ID: task.PreviewID, BaseURL: draft.Session.BaseURL, Session: &draft.Session,
			Platform: draft.Session.Platform, Settings: draft.Settings, SkipTLSVerify: draft.SkipTLS}
		err = nil
	}
	if err != nil || site.Session == nil {
		return jobs.Outcome{}, fmt.Errorf("上游未连接，请恢复连接后重试任务")
	}
	save := func(phase string) error {
		task.Phase = phase
		value, err := s.seal(task)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(ctx, `UPDATE upstream_preview_probes SET encrypted=$2,updated_at=now() WHERE id=$1`, id, value)
		return err
	}
	sel := task.Selection
	temporary := sel.KeyID == "" && sel.ConnectionID == ""
	name := previewKeyName(id)
	if task.Phase == "creating_key" {
		// Creation may have reached the upstream before the worker stopped.
		// Recover the exact temporary Key by name and clean it up without submitting a new test.
		records, err := s.platform.SecurityKeys(ctx, *site.Session)
		if err != nil {
			return jobs.Outcome{}, fmt.Errorf("暂无法核对临时 Key，请重试清理")
		}
		for _, record := range records {
			if safeString(record, "name") == name && SecurityKeyGroupMatches(site.Platform, record, sel.GroupID) {
				if task.KeyID != "" {
					return jobs.Outcome{}, fmt.Errorf("临时 Key 名称冲突，请核对上游")
				}
				task.KeyID = SecurityKeyID(record)
			}
		}
		if task.Result.Message == "" {
			task.Result.APIKeyProbeResult = APIKeyProbeResult{Model: sel.Model, Inconclusive: true, Message: "测试在临时 Key 创建时中断"}
		}
		if err = save("cleanup"); err != nil {
			return jobs.Outcome{}, err
		}
	}
	if task.Phase == "new" && temporary {
		ips, err := policies.AllowedIPs(ctx, uid, aid)
		if err == nil && task.PreviewID == "" {
			ips, err = policies.AllowedIPsForGroup(ctx, uid, aid, site.ID, sel.GroupID)
		}
		if err != nil {
			return jobs.Outcome{}, fmt.Errorf("读取测试 Key 白名单失败")
		}
		config := map[string]any{"name": name, "allow_ips": strings.Join(ips, "\n"), "group": sel.GroupID, "unlimited_quota": true, "expired_time": time.Now().Add(time.Hour).Unix()}
		if site.Platform == PlatformSub2API {
			group, err := strconv.Atoi(sel.GroupID)
			if err != nil {
				return jobs.Outcome{}, fmt.Errorf("该上游分组 ID 无效")
			}
			config = map[string]any{"name": name, "group_id": group, "ip_whitelist": ips, "expires_in_days": 1}
		}
		if err = save("creating_key"); err != nil {
			return jobs.Outcome{}, err
		}
		task.KeyID, _, err = s.platform.CreateManagedKey(ctx, *site.Session, config, "")
		if err != nil {
			task.Result.APIKeyProbeResult = APIKeyProbeResult{Model: sel.Model, Message: "临时 Key 创建失败，正在核对上游", Inconclusive: true}
			if err = save("creating_key"); err != nil {
				return jobs.Outcome{}, err
			}
			return jobs.Outcome{Pending: true, Delay: 5 * time.Second}, nil
		}
		if err = save("ready"); err != nil {
			return jobs.Outcome{}, err
		}
	}
	if task.Phase == "submitting" {
		task.Result.APIKeyProbeResult = APIKeyProbeResult{Model: sel.Model, Inconclusive: true, Message: "上游受理结果待确认，请在上游查看调用记录"}
		task.Probe = nil
		if err = save("cleanup"); err != nil {
			return jobs.Outcome{}, err
		}
	}
	if task.Phase == "new" || task.Phase == "ready" || task.Phase == "polling" {
		keyID, accountURL := task.KeyID, ""
		var opts APIKeyProbeOptions
		if task.Phase == "polling" && task.Probe != nil {
			opts = *task.Probe
			opts.TaskID = task.TaskID
		} else {
			if !temporary {
				keyID, accountURL, err = s.manualConnection(ctx, uid, aid, site, sel)
				if task.KeyID != "" && keyID != task.KeyID {
					err = fmt.Errorf("测试期间绑定 Key 已变化")
				}
			}
			if err == nil {
				opts, err = s.manualTestProbeOptions(ctx, site, sel, keyID, accountURL, task.TaskID, id)
			}
		}
		if err != nil {
			task.Result.APIKeyProbeResult = APIKeyProbeResult{Model: sel.Model, Message: err.Error(), Inconclusive: true}
		} else if sel.CatalogOnly {
			task.Result.Models, err = s.platform.FetchAPIKeyModelIDs(ctx, opts)
			if err != nil {
				task.Result.Message = "临时 Key 无法读取模型目录，可手动输入模型 ID"
			} else {
				task.Result.Success = true
				task.Result.Message = fmt.Sprintf("读取到 %d 个可选模型", len(task.Result.Models))
			}
		} else {
			if task.Phase != "polling" {
				task.KeyID = keyID
				if sel.Mode != "text" {
					task.Probe = &opts
				}
				if err = save("submitting"); err != nil {
					return jobs.Outcome{}, err
				}
			}
			testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			result, probeErr := s.platform.ProbeAPIKey(testCtx, opts)
			cancel()
			result.Message = strings.ReplaceAll(result.Message, opts.APIKey, "[已隐藏]")
			task.Result.APIKeyProbeResult = result
			if task.TaskID == "" && result.TaskID != "" {
				task.TaskID = result.TaskID
			}
			task.Result.TaskID = task.TaskID
			if task.TaskID != "" && (result.Pending || result.Uncertain || probeErr != nil) && time.Since(task.StartedAt) < 45*time.Minute {
				if err = save("polling"); err != nil {
					return jobs.Outcome{}, err
				}
				return jobs.Outcome{Pending: true, Delay: 8 * time.Second, Value: task.Result}, nil
			}
			if probeErr != nil || result.Pending || result.Uncertain {
				task.Result.Inconclusive = true
				if task.Result.Message == "" || result.Pending || result.Uncertain {
					task.Result.Message = "调用超时或协议不可用，请核对上游记录"
				}
			}
		}
		task.Probe = nil
		if err = save("cleanup"); err != nil {
			return jobs.Outcome{}, err
		}
	}
	if task.Phase == "cleanup" && task.Result.Success && !sel.CatalogOnly && sel.ConnectionID != "" && s.AfterBoundModelTest != nil {
		if task.FinishedAt.IsZero() {
			task.FinishedAt = time.Now().UTC()
			if err = save("cleanup"); err != nil {
				return jobs.Outcome{}, err
			}
		}
		err = s.AfterBoundModelTest(ctx, BoundModelTest{
			UserID: uid, WorkspaceID: aid, ConnectionID: sel.ConnectionID, SiteID: site.ID, KeyID: task.KeyID, ID: id,
			Model: sel.Model, Mode: sel.Mode, Adapter: sel.Adapter, LatencyMS: task.Result.LatencyMS, CheckedAt: task.FinishedAt,
		})
		if err != nil {
			return jobs.Outcome{}, fmt.Errorf("模型调用成功，监控状态写入失败，可重试同步结果")
		}
	}
	if task.Phase == "cleanup" && temporary && task.KeyID != "" {
		if site.Platform == PlatformSub2API {
			err = s.platform.DeleteSub2APIKey(*site.Session, task.KeyID)
		} else {
			err = s.platform.DeleteNewAPIToken(*site.Session, task.KeyID)
		}
		var requestError *RequestError
		if err != nil && !(errors.As(err, &requestError) && requestError.HTTPStatus == http.StatusNotFound) {
			return jobs.Outcome{Pending: true, Delay: time.Minute, Value: map[string]string{"message": "正在清理临时测试 Key"}}, nil
		}
	}
	task.Result.Model = sel.Model
	task.Result.Pending = false
	task.Probe, task.Draft = nil, nil
	if err = save("done"); err != nil {
		return jobs.Outcome{}, err
	}
	return jobs.Outcome{Value: task.Result}, nil
}

func (s *HealthService) manualTestProbeOptions(ctx context.Context, site *Site, sel manualModelSelection, keyID, accountURL, taskID, testID string) (APIKeyProbeOptions, error) {
	record, _, err := s.platform.ReadSecurityKey(ctx, *site.Session, keyID)
	if err != nil || sel.GroupID != "" && !SecurityKeyGroupMatches(site.Platform, record, sel.GroupID) {
		return APIKeyProbeOptions{}, fmt.Errorf("测试 Key 不可读取或已更换上游分组")
	}
	key, err := s.platform.KeySecret(ctx, *site.Session, keyID, record)
	if err != nil {
		return APIKeyProbeOptions{}, fmt.Errorf("无法读取测试 Key 凭据")
	}
	return APIKeyProbeOptions{BaseURL: EffectiveAPIURL(*site, SecurityKeyGroup(site.Platform, record), accountURL), APIKey: key,
		Platform: sel.Platform, ModelID: sel.Model, Mode: sel.Mode, Adapter: sel.Adapter, Seconds: sel.Seconds,
		TaskID: taskID, IdempotencyKey: testID, InsecureSkipTLS: site.SkipTLSVerify || site.Session.InsecureSkipTLS, VerifyOutput: true}, nil
}
