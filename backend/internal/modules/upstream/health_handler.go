package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

type previewData struct {
	Request CreateRequest
	Login   *LoginResult
}
type previewView struct {
	CommittedSiteID string      `json:"committedSiteId,omitempty"`
	ID              string      `json:"id"`
	Status          string      `json:"status"`
	Groups          []GroupInfo `json:"groups"`
	Health          HealthPage  `json:"health"`
	JobID           string      `json:"jobId,omitempty"`
}
type probeSelection struct {
	GroupID     string `json:"groupId"`
	Model       string `json:"model"`
	Mode        string `json:"mode"`
	Adapter     string `json:"adapter"`
	PresetModel string `json:"presetModel"`
	Size        string `json:"size"`
	Seconds     int    `json:"seconds"`
	Prompt      string `json:"prompt"`
}
type previewProbe struct {
	KeyName   string
	Selection probeSelection
	KeyID     string
	Key       string
	TaskID    string
	Phase     string
	Result    APIKeyProbeResult
	StartedAt time.Time
}

func previewKeyName(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "THcheck-" + hex.EncodeToString(sum[:6])
}

type previewLoginKey struct{}

func (s *HealthService) scope(r *http.Request) (string, string, error) {
	uid, _ := authctx.UserID(r.Context())
	if uid == "" {
		return "", "", fmt.Errorf("未登录")
	}
	aid, e := s.sites.requireCurrentAdminAccountID(r.Context(), uid)
	return uid, aid, e
}
func (s *HealthService) ownedSite(ctx context.Context, uid, aid, id string) (*Site, error) {
	site, e := s.sites.cache.Get(ctx, id)
	if e != nil {
		return nil, e
	}
	if site == nil || site.UserID != uid || site.AdminAccountID != aid {
		return nil, fmt.Errorf("上游不存在")
	}
	return site, nil
}
func (s *HealthService) seal(v any) (string, error) {
	if s.cipher == nil {
		return "", fmt.Errorf("预览需配置凭据加密密钥")
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	return s.cipher.Encrypt(string(raw))
}
func (s *HealthService) open(raw string, v any) error {
	if s.cipher == nil {
		return fmt.Errorf("凭据加密不可用")
	}
	text, e := s.cipher.Decrypt(raw)
	if e != nil {
		return e
	}
	return json.Unmarshal([]byte(text), v)
}
func (s *HealthService) preview(ctx context.Context, uid, aid, id string) (previewData, previewView, error) {
	var data previewData
	var v previewView
	var encrypted string
	var raw []byte
	e := s.db.QueryRow(ctx, `SELECT encrypted,payload FROM upstream_health_previews WHERE id=$1 AND user_id=$2 AND admin_account_id=$3 AND expires_at>now()`, id, uid, aid).Scan(&encrypted, &raw)
	if e != nil {
		return data, v, fmt.Errorf("预览已过期或不存在")
	}
	if e = s.open(encrypted, &data); e != nil {
		return data, v, e
	}
	e = json.Unmarshal(raw, &v)
	return data, v, e
}
func (s *HealthService) savePreview(ctx context.Context, id string, data previewData, v previewView) error {
	encrypted, e := s.seal(data)
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(v)
	_, e = s.db.Exec(ctx, `UPDATE upstream_health_previews SET encrypted=$2,payload=$3,expires_at=now()+interval '24 hours' WHERE id=$1`, id, encrypted, raw)
	return e
}
func (s *HealthService) Register(mux *http.ServeMux, j *jobs.Service, policies *KeyPolicies) {
	s.registerMonitorTemplates(mux)
	s.registerGroupKeyPolicies(mux, j, policies)
	mux.HandleFunc("POST /api/upstream-sites/{id}/precheck", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := s.scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		site, e := s.ownedSite(r.Context(), uid, aid, r.PathValue("id"))
		if e != nil || site.Session == nil {
			httpjson.WriteError(w, 400, "上游未连接")
			return
		}
		id, e := randomID()
		if e != nil {
			httpjson.WriteError(w, 500, "无法创建预检")
			return
		}
		id = "preview_" + id
		data := previewData{Login: &LoginResult{Session: *site.Session, Platform: site.Platform, Metrics: site.Metrics}}
		v := previewView{ID: id, Status: "ready", Groups: site.Metrics.Groups, CommittedSiteID: site.ID}
		v.Health, e = s.Page(r.Context(), uid, aid, site.ID, nil)
		if e != nil {
			httpjson.WriteError(w, 500, "无法读取状态")
			return
		}
		encrypted, e := s.seal(data)
		if e != nil {
			httpjson.WriteError(w, 400, e.Error())
			return
		}
		raw, _ := json.Marshal(v)
		if _, e = s.db.Exec(r.Context(), `INSERT INTO upstream_health_previews(id,user_id,admin_account_id,encrypted,payload,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '24 hours')`, id, uid, aid, encrypted, raw); e != nil {
			httpjson.WriteError(w, 500, "无法保存预检")
			return
		}
		_, e = s.db.Exec(r.Context(), `INSERT INTO upstream_model_health SELECT $1,user_id,admin_account_id,source_id,model,group_id,source,payload FROM upstream_model_health WHERE site_id=$2 AND user_id=$3 AND admin_account_id=$4`, id, site.ID, uid, aid)
		if e != nil {
			httpjson.WriteError(w, 500, "无法加载预检状态")
			return
		}
		httpjson.Write(w, 201, v)
	})
	j.Register("health_refresh", func(ctx context.Context, uid, aid, id string, _ json.RawMessage) (jobs.Outcome, error) {
		site, e := s.ownedSite(ctx, uid, aid, id)
		if e != nil {
			return jobs.Outcome{}, e
		}
		return jobs.Outcome{}, s.Refresh(ctx, *site, true)
	})
	j.Register("health_preview", func(ctx context.Context, uid, aid, id string, _ json.RawMessage) (jobs.Outcome, error) {
		data, v, e := s.preview(ctx, uid, aid, id)
		if e != nil {
			return jobs.Outcome{}, e
		}
		result, e := s.sites.createLogin(data.Request)
		if e != nil {
			return jobs.Outcome{}, e
		}
		data.Login = &result
		v.Status = "ready"
		v.Groups = result.Metrics.Groups
		site := Site{ID: id, UserID: uid, AdminAccountID: aid, BaseURL: result.Session.BaseURL, Platform: result.Platform, Session: &result.Session, Metrics: result.Metrics, SkipTLSVerify: data.Request.SkipTLSVerify, Settings: SiteSettings{MonitorProfile: data.Request.MonitorProfile, DefaultAPIURL: data.Request.APIURL, APIURLManaged: true}}
		if e = s.Refresh(ctx, site, true); e != nil {
			return jobs.Outcome{}, e
		}
		v.Health, e = s.Page(ctx, uid, aid, id, nil)
		if e != nil {
			return jobs.Outcome{}, e
		}
		return jobs.Outcome{Value: v}, s.savePreview(ctx, id, data, v)
	})
	_, _ = s.db.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS upstream_preview_probes(id text PRIMARY KEY,preview_id text NOT NULL,user_id text NOT NULL,admin_account_id text NOT NULL,encrypted text NOT NULL,updated_at timestamptz NOT NULL DEFAULT now())`)
	j.Register("health_probe", func(ctx context.Context, uid, aid, id string, _ json.RawMessage) (jobs.Outcome, error) {
		return s.runPreviewProbe(ctx, uid, aid, id, policies)
	})
	s.registerManualModelTest(mux, j, policies)
	mux.HandleFunc("GET /api/upstream-sites/{id}/model-health", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := s.scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		if _, e = s.ownedSite(r.Context(), uid, aid, r.PathValue("id")); e != nil {
			httpjson.WriteError(w, 404, e.Error())
			return
		}
		v, e := s.Page(r.Context(), uid, aid, r.PathValue("id"), r.URL.Query())
		if e != nil {
			httpjson.WriteError(w, 500, "无法读取模型状态")
			return
		}
		httpjson.Write(w, 200, v)
	})
	mux.HandleFunc("POST /api/upstream-sites/{id}/model-health/refresh", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := s.scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		if _, e = s.ownedSite(r.Context(), uid, aid, r.PathValue("id")); e != nil {
			httpjson.WriteError(w, 404, e.Error())
			return
		}
		v, e := j.Submit(r.Context(), uid, aid, "health_refresh", []jobs.Item{{ID: r.PathValue("id")}})
		if e != nil {
			httpjson.WriteError(w, 400, e.Error())
			return
		}
		httpjson.Write(w, 202, v)
	})
	mux.HandleFunc("PUT /api/upstream-sites/{id}/model-health/mapping", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := s.scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		site, e := s.ownedSite(r.Context(), uid, aid, r.PathValue("id"))
		if e != nil {
			httpjson.WriteError(w, 404, e.Error())
			return
		}
		var v HealthMapping
		if httpjson.Decode(r, &v) != nil || v.Model == "" || v.SourceID == "" || v.GroupID == "" {
			httpjson.WriteError(w, 400, "请选择来源、分组和准确模型 ID")
			return
		}
		valid := false
		for _, g := range site.Metrics.Groups {
			if g.ID == v.GroupID {
				valid = true
			}
		}
		if !valid {
			httpjson.WriteError(w, 400, "分组不存在")
			return
		}
		_, e = s.db.Exec(r.Context(), `INSERT INTO upstream_health_mappings SELECT site_id,source_id,$3,$4 FROM upstream_model_health WHERE site_id=$1 AND source_id=$2 AND user_id=$5 AND admin_account_id=$6 ON CONFLICT(site_id,source_id) DO UPDATE SET group_id=EXCLUDED.group_id,model=EXCLUDED.model`, site.ID, v.SourceID, v.GroupID, v.Model, uid, aid)
		if e != nil {
			httpjson.WriteError(w, 500, "保存失败")
			return
		}
		httpjson.Write(w, 200, v)
	})
	mux.HandleFunc("POST /api/upstream-previews", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := s.scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		var req CreateRequest
		if httpjson.Decode(r, &req) != nil {
			httpjson.WriteError(w, 400, "无效上游配置")
			return
		}
		if e = validateCreate(req); e != nil {
			httpjson.WriteError(w, 400, errorKey(e))
			return
		}
		id, e := randomID()
		if e != nil {
			httpjson.WriteError(w, 500, "无法创建预览")
			return
		}
		id = "preview_" + id
		encrypted, e := s.seal(previewData{Request: req})
		if e != nil {
			httpjson.WriteError(w, 400, e.Error())
			return
		}
		v := previewView{ID: id, Status: "connecting", Groups: []GroupInfo{}, Health: HealthPage{Items: []ModelHealth{}, Capabilities: []HealthCapability{}}}
		b, _ := json.Marshal(v)
		if _, e = s.db.Exec(r.Context(), `INSERT INTO upstream_health_previews(id,user_id,admin_account_id,encrypted,payload) VALUES($1,$2,$3,$4,$5)`, id, uid, aid, encrypted, b); e != nil {
			httpjson.WriteError(w, 500, "无法保存预览")
			return
		}
		job, e := j.Submit(r.Context(), uid, aid, "health_preview", []jobs.Item{{ID: id}})
		if e != nil {
			httpjson.WriteError(w, 500, "无法提交预览")
			return
		}
		v.JobID = job.ID
		httpjson.Write(w, 202, v)
	})
	mux.HandleFunc("GET /api/upstream-previews/{id}", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := s.scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		_, v, e := s.preview(r.Context(), uid, aid, r.PathValue("id"))
		if e != nil {
			httpjson.WriteError(w, 404, e.Error())
			return
		}
		v.Health, e = s.Page(r.Context(), uid, aid, v.ID, r.URL.Query())
		if e != nil {
			httpjson.WriteError(w, 500, "读取失败")
			return
		}
		httpjson.Write(w, 200, v)
	})
	mux.HandleFunc("POST /api/upstream-previews/{id}/commit", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := s.scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		data, view, e := s.preview(r.Context(), uid, aid, r.PathValue("id"))
		if e != nil || data.Login == nil {
			httpjson.WriteError(w, 400, "请先完成连接预览")
			return
		}
		if view.CommittedSiteID != "" {
			site, e := s.ownedSite(r.Context(), uid, aid, view.CommittedSiteID)
			if e != nil {
				httpjson.WriteError(w, 404, "已添加的上游不存在")
				return
			}
			httpjson.Write(w, 200, toResponse(site))
			return
		}
		ctx := context.WithValue(r.Context(), previewLoginKey{}, data.Login)
		v, e := s.sites.Create(ctx, uid, data.Request)
		if e != nil {
			httpjson.WriteError(w, 400, errorKey(e))
			return
		}
		view.CommittedSiteID = v.ID
		_ = s.savePreview(r.Context(), view.ID, data, view)
		httpjson.Write(w, 201, v)
	})
	mux.HandleFunc("POST /api/upstream-previews/{id}/probes", func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.scope(r); err != nil {
			httpjson.WriteError(w, http.StatusUnauthorized, "未登录")
			return
		}
		httpjson.WriteError(w, http.StatusGone, "主动检测已关闭，请在上游 Key 详情中手动测试")
	})
}
func (s *HealthService) runPreviewProbe(ctx context.Context, uid, aid, id string, p *KeyPolicies) (jobs.Outcome, error) {
	var previewID, encrypted string
	e := s.db.QueryRow(ctx, `SELECT preview_id,encrypted FROM upstream_preview_probes WHERE id=$1 AND user_id=$2 AND admin_account_id=$3`, id, uid, aid).Scan(&previewID, &encrypted)
	if e != nil {
		return jobs.Outcome{}, e
	}
	var task previewProbe
	if e = s.open(encrypted, &task); e != nil {
		return jobs.Outcome{}, e
	}
	data, _, e := s.preview(ctx, uid, aid, previewID)
	if e != nil {
		return jobs.Outcome{}, e
	}
	if data.Login == nil {
		return jobs.Outcome{}, fmt.Errorf("上游未连接")
	}
	session := data.Login.Session
	save := func() error {
		encrypted, e := s.seal(task)
		if e != nil {
			return e
		}
		_, e = s.db.Exec(ctx, `UPDATE upstream_preview_probes SET encrypted=$2,updated_at=now() WHERE id=$1`, id, encrypted)
		return e
	}
	if task.Phase == "new" {
		return jobs.Outcome{}, fmt.Errorf("主动检测已关闭")
	}
	if task.Phase == "creating_key" {
		var keys []Sub2APIKeyItem
		if session.Platform == PlatformSub2API {
			keys, e = s.platform.ListSub2APIKeys(session)
		} else {
			keys, e = s.platform.ListNewAPITokens(session)
		}
		if e != nil {
			return jobs.Outcome{}, e
		}
		for _, key := range keys {
			name := task.KeyName
			if name == "" {
				name = "TransitHub-" + id
			}
			if key.Name == name {
				task.KeyID = key.ID
				task.Key = key.Key
				task.Phase = "cleanup"
				break
			}
		}
		if task.Phase == "creating_key" {
			return jobs.Outcome{}, fmt.Errorf("未查到预检 Key，请核对上游创建结果")
		}
		if e = save(); e != nil {
			return jobs.Outcome{}, e
		}
	}
	if task.Phase == "submitting" || task.Phase == "unknown" {
		result, err := s.platform.ProbeAPIKey(ctx, APIKeyProbeOptions{BaseURL: session.BaseURL, APIKey: task.Key, Platform: "openai", ModelID: task.Selection.Model, Mode: task.Selection.Mode, Adapter: task.Selection.Adapter, PresetModel: task.Selection.PresetModel, Seconds: task.Selection.Seconds, IdempotencyKey: id, Recover: true, InsecureSkipTLS: session.InsecureSkipTLS})
		if err != nil || result.TaskID == "" {
			return jobs.Outcome{}, fmt.Errorf("受理结果待确认，已阻止重复提交；可核对上游后再次查询")
		}
		task.TaskID = result.TaskID
		task.Phase = "polling"
		if e = save(); e != nil {
			return jobs.Outcome{}, e
		}
	}
	if task.Phase == "key_error" {
		return jobs.Outcome{}, fmt.Errorf("预检 Key 创建失败，请重新发起预检")
	}
	if task.Phase == "ready" {
		task.Phase = "cleanup"
		task.Result.Inconclusive = true
		task.Result.Message = "主动检测已关闭，未提交生成"
		if e = save(); e != nil {
			return jobs.Outcome{}, e
		}
	}
	if task.Phase == "polling" && task.TaskID != "" {
		sel := task.Selection
		o := APIKeyProbeOptions{BaseURL: session.BaseURL, APIKey: task.Key, Platform: "openai", ModelID: sel.Model, Mode: sel.Mode, Adapter: sel.Adapter, PresetModel: sel.PresetModel, Size: sel.Size, Seconds: sel.Seconds, Prompt: sel.Prompt, TaskID: task.TaskID, IdempotencyKey: id, InsecureSkipTLS: session.InsecureSkipTLS}
		if o.Mode == "" {
			o.Mode = "text"
		}
		for _, g := range data.Login.Metrics.Groups {
			if g.ID == sel.GroupID && g.Platform != nil {
				o.Platform = *g.Platform
			}
		}
		if task.TaskID == "" {
			task.Phase = "submitting"
			if e = save(); e != nil {
				return jobs.Outcome{}, e
			}
		}
		result, err := s.platform.ProbeAPIKey(ctx, o)
		task.Result = result
		if result.TaskID != "" {
			task.TaskID = result.TaskID
		}
		if err != nil && task.TaskID != "" {
			task.Phase = "polling"
			_ = save()
			return jobs.Outcome{Pending: true, Delay: 20 * time.Second, Value: map[string]any{"status": "generating", "message": "继续查询原任务"}}, nil
		}
		if result.Uncertain || (err != nil && task.Phase == "submitting") {
			task.Phase = "unknown"
			_ = save()
			return jobs.Outcome{}, fmt.Errorf("生成受理待确认；不自动重复提交")
		}
		if result.Pending {
			task.Phase = "polling"
			if e = save(); e != nil {
				return jobs.Outcome{}, e
			}
			if time.Since(task.StartedAt) > time.Hour {
				return jobs.Outcome{}, fmt.Errorf("生成超过观察上限，请核对上游任务")
			}
			return jobs.Outcome{Pending: true, Delay: 20 * time.Second, Value: map[string]any{"status": "generating", "taskId": task.TaskID}}, nil
		}
		if err != nil {
			task.Result.Message = "检测协议或请求不可用"
			task.Result.Inconclusive = true
		}
		task.Phase = "cleanup"
		if e = save(); e != nil {
			return jobs.Outcome{}, e
		}
	}
	if task.Phase == "cleanup" {
		if session.Platform == PlatformSub2API {
			e = s.platform.DeleteSub2APIKey(session, task.KeyID)
		} else {
			e = s.platform.DeleteNewAPIToken(session, task.KeyID)
		}
		if e != nil {
			if re, ok := e.(*RequestError); !ok || re.HTTPStatus != 404 {
				return jobs.Outcome{Pending: true, Delay: time.Minute, Value: map[string]any{"status": "cleanup", "message": "检测结束，正在回收预检 Key"}}, nil
			}
		}
		task.Phase = "done"
		task.Key = ""
		if e = save(); e != nil {
			return jobs.Outcome{}, e
		}
	}
	return jobs.Outcome{Value: map[string]any{"success": task.Result.Success, "message": task.Result.Message, "inconclusive": task.Result.Inconclusive, "model": task.Selection.Model, "groupId": task.Selection.GroupID, "latencyMs": task.Result.LatencyMS}}, nil
}
