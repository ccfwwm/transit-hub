package my_sites

import (
	"context"
	"encoding/json"
	"net/http"
	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

func (k *KeySecurity) whitelistSwitch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid, _ := authctx.UserID(ctx)
	aid, e := k.sites.currentAdminAccountID(ctx, uid)
	if e != nil {
		httpjson.WriteError(w, 403, "请选择工作区")
		return
	}
	c, e := k.sites.connRepository.GetRealConnection(ctx, r.PathValue("id"), uid, aid)
	if e != nil || c == nil {
		httpjson.WriteError(w, 404, "绑定不存在")
		return
	}
	site, e := k.ownedSite(ctx, uid, aid, c.UpstreamSiteID)
	if e != nil {
		httpjson.WriteError(w, 404, e.Error())
		return
	}
	var job *jobs.Job
	if r.Method == http.MethodPut {
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if httpjson.Decode(r, &req) != nil || req.Enabled == nil {
			httpjson.WriteError(w, 400, "请选择白名单开关")
			return
		}
		lock, e := k.db.Acquire(ctx)
		if e != nil {
			httpjson.WriteError(w, 503, "安全任务服务忙")
			return
		}
		defer lock.Release()
		var locked bool
		key := "key-security|" + site.ID + "|" + c.UpstreamKeyID
		e = lock.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&locked)
		if e != nil || !locked {
			httpjson.WriteError(w, 409, "该 Key 正在更新，请稍后重试")
			return
		}
		defer lock.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key)
		if e = k.policies.SetWhitelistEnabled(ctx, uid, aid, site.ID, c.UpstreamKeyID, *req.Enabled); e != nil {
			httpjson.WriteError(w, 500, "白名单开关保存失败")
			return
		}
		t := securityTarget{SiteID: site.ID, KeyID: c.UpstreamKeyID, GroupID: c.UpstreamGroupID, Action: "whitelist"}
		raw, _ := json.Marshal(t)
		j, e := k.jobs.Submit(ctx, uid, aid, "key_security", []jobs.Item{{ID: site.ID + "/" + c.UpstreamKeyID, Payload: raw}})
		if e != nil {
			httpjson.WriteError(w, 500, "开关已保存，同步任务提交失败；请重试")
			return
		}
		job = &j
	}
	enabled, e := k.policies.WhitelistEnabled(ctx, uid, aid, site.ID, c.UpstreamKeyID)
	if e != nil {
		httpjson.WriteError(w, 500, "读取开关失败")
		return
	}
	var shared int
	_ = k.db.QueryRow(ctx, `SELECT count(*) FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_key_id=$4`, uid, aid, site.ID, c.UpstreamKeyID).Scan(&shared)
	httpjson.Write(w, 200, map[string]any{"enabled": enabled, "sharedAccounts": shared, "job": job})
}

func (k *KeySecurity) syncSiteWhitelist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid, _ := authctx.UserID(ctx)
	aid, e := k.sites.currentAdminAccountID(ctx, uid)
	if e != nil {
		httpjson.WriteError(w, 403, "请选择工作区")
		return
	}
	var req struct {
		SiteID string `json:"siteId"`
	}
	if httpjson.Decode(r, &req) != nil {
		httpjson.WriteError(w, 400, "请选择上游")
		return
	}
	site, e := k.ownedSite(ctx, uid, aid, req.SiteID)
	if e != nil {
		httpjson.WriteError(w, 404, e.Error())
		return
	}
	all, e := k.targets(ctx, uid, aid, "whitelist")
	if e != nil {
		httpjson.WriteError(w, 500, "读取绑定失败")
		return
	}
	items := []jobs.Item{}
	for _, item := range all {
		var t securityTarget
		if json.Unmarshal(item.Payload, &t) == nil && t.SiteID == site.ID {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		httpjson.Write(w, 200, map[string]any{"status": "done", "total": 0, "failed": 0, "items": []jobs.Item{}})
		return
	}
	j, e := k.jobs.Submit(ctx, uid, aid, "key_security", items)
	if e != nil {
		httpjson.WriteError(w, 500, "提交同步任务失败")
		return
	}
	httpjson.Write(w, 202, j)
}
