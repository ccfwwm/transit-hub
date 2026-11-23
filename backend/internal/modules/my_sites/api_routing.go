package my_sites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

type APIRouting struct {
	db    *pgxpool.Pool
	sites *Service
	jobs  *jobs.Service
}
type routeTarget struct {
	SiteID  string `json:"siteId"`
	URL     string `json:"url"`
	Attempt int    `json:"attempt,omitempty"`
}
type domainReference struct {
	UserID  string `json:"userId"`
	AdminID string `json:"adminId"`
	Host    string `json:"host"`
	Label   string `json:"label"`
}
type domainEntry struct {
	Host       string   `json:"host"`
	Manual     bool     `json:"manual"`
	References []string `json:"references"`
	Active     bool     `json:"active"`
}

func NewAPIRouting(db *pgxpool.Pool, sites *Service, js *jobs.Service) *APIRouting {
	return &APIRouting{db, sites, js}
}
func (a *APIRouting) EnsureSchema(ctx context.Context) error {
	_, e := a.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS upstream_api_route_versions(site_id text PRIMARY KEY,revision text NOT NULL,updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS upstream_domain_manual(user_id text NOT NULL,admin_account_id text NOT NULL,host text NOT NULL,PRIMARY KEY(user_id,admin_account_id,host));
CREATE TABLE IF NOT EXISTS upstream_domain_bridge(id boolean PRIMARY KEY DEFAULT true CHECK(id),target_url text NOT NULL DEFAULT '',active_hosts jsonb NOT NULL DEFAULT '[]',external_refs jsonb NOT NULL DEFAULT '[]',status text NOT NULL DEFAULT 'unconfigured',error text NOT NULL DEFAULT '',updated_at timestamptz NOT NULL DEFAULT now());
INSERT INTO upstream_domain_bridge(id) VALUES(true) ON CONFLICT DO NOTHING;`)
	return e
}
func (a *APIRouting) Register(m *http.ServeMux) {
	a.jobs.Register("upstream_api_sync", a.run)
	m.HandleFunc("GET /api/my-sites/domain-allowlist", a.domains)
	m.HandleFunc("POST /api/my-sites/domain-allowlist", a.domains)
	m.HandleFunc("DELETE /api/my-sites/domain-allowlist", a.domains)
	m.HandleFunc("POST /api/my-sites/api-routing-sync", a.sync)
}
func (a *APIRouting) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if e := a.scan(ctx); e != nil {
					log.Printf("[api-routing] queue scan failed: %v", e)
				}
			}
		}
	}()
}

// This scanner only reads local state. A stable target signature prevents page
// refreshes and periodic upstream status collection from re-writing accounts.
func (a *APIRouting) scan(ctx context.Context) error {
	rows, e := a.db.Query(ctx, `SELECT id,user_id,admin_account_id,base_url,settings FROM upstream_sites ORDER BY id`)
	if e != nil {
		return e
	}
	list := []upstream.Site{}
	for rows.Next() {
		var s upstream.Site
		var raw []byte
		if e = rows.Scan(&s.ID, &s.UserID, &s.AdminAccountID, &s.BaseURL, &raw); e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal(raw, &s.Settings); e != nil {
			rows.Close()
			return e
		}
		list = append(list, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, s := range list {
		if e = a.queue(ctx, s, false); e != nil {
			return e
		}
	}
	return nil
}
func (a *APIRouting) queue(ctx context.Context, s upstream.Site, force bool) error {
	conns, e := a.sites.connRepository.ListRealConnections(ctx, s.UserID, s.AdminAccountID)
	if e != nil {
		return e
	}
	items := []jobs.Item{}
	signature := []string{}
	for _, c := range conns {
		if c.UpstreamSiteID != s.ID {
			continue
		}
		_, groupManaged := s.Settings.GroupAPIURLs[c.UpstreamGroupID]
		if !force && !s.Settings.APIURLManaged && !groupManaged {
			continue
		}
		u := upstream.EffectiveAPIURL(s, c.UpstreamGroupID, "")
		t := routeTarget{SiteID: s.ID, URL: u}
		raw, _ := json.Marshal(t)
		items = append(items, jobs.Item{ID: c.ID, Payload: raw})
		signature = append(signature, c.ID+"|"+u)
	}
	sort.Strings(signature)
	b, _ := json.Marshal(signature)
	hash := sha256.Sum256(b)
	revision := hex.EncodeToString(hash[:])
	var previous string
	_ = a.db.QueryRow(ctx, `SELECT revision FROM upstream_api_route_versions WHERE site_id=$1`, s.ID).Scan(&previous)
	if !force && previous == revision {
		return nil
	}
	if len(items) > 0 {
		if _, e = a.jobs.Submit(ctx, s.UserID, s.AdminAccountID, "upstream_api_sync", items); e != nil {
			return e
		}
	}
	_, e = a.db.Exec(ctx, `INSERT INTO upstream_api_route_versions(site_id,revision) VALUES($1,$2) ON CONFLICT(site_id) DO UPDATE SET revision=excluded.revision,updated_at=now()`, s.ID, revision)
	return e
}
func (a *APIRouting) run(ctx context.Context, uid, aid, id string, raw json.RawMessage) (jobs.Outcome, error) {
	var t routeTarget
	if json.Unmarshal(raw, &t) != nil {
		return jobs.Outcome{}, fmt.Errorf("无效 API 同步任务")
	}
	c, e := a.sites.connRepository.GetRealConnection(ctx, id, uid, aid)
	if e != nil {
		return jobs.Outcome{}, e
	}
	if c == nil || c.UpstreamSiteID != t.SiteID {
		return jobs.Outcome{Value: map[string]string{"status": "removed"}}, nil
	}
	s, e := a.sites.upstreamLookup.GetSite(ctx, t.SiteID)
	if e != nil || s == nil || s.UserID != uid || s.AdminAccountID != aid {
		return jobs.Outcome{Value: map[string]string{"status": "removed"}}, nil
	}
	currentURL, _ := upstream.NormalizeRoutingURL(upstream.EffectiveAPIURL(*s, c.UpstreamGroupID, ""))
	targetURL, _ := upstream.NormalizeRoutingURL(t.URL)
	if currentURL != targetURL {
		return jobs.Outcome{Value: map[string]string{"status": "superseded"}}, nil
	}
	// Share the rotation lock: both operations preserve the same credentials map.
	lock, e := a.db.Acquire(ctx)
	if e != nil {
		return jobs.Outcome{}, e
	}
	defer lock.Release()
	var locked bool
	e = lock.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "key-security|"+c.UpstreamSiteID+"|"+c.UpstreamKeyID).Scan(&locked)
	if e != nil {
		return jobs.Outcome{}, e
	}
	if !locked {
		return jobs.Outcome{Pending: true, Delay: 5 * time.Second}, nil
	}
	defer lock.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "key-security|"+c.UpstreamSiteID+"|"+c.UpstreamKeyID)
	// A rotation may have completed while this task was queued.
	c, e = a.sites.connRepository.GetRealConnection(ctx, id, uid, aid)
	if e != nil || c == nil {
		return jobs.Outcome{}, fmt.Errorf("绑定已变化，请重新同步")
	}
	state, e := a.sites.authenticatedState(ctx, uid, aid)
	if e == nil {
		e = a.sites.platformService.UpdateForwardingAPIURL(ctx, state.Session, c.AdminAccountID, c.UpstreamKey, t.URL)
	}
	if e == nil {
		_, e = a.db.Exec(ctx, `UPDATE real_connections SET upstream_api_url=$4 WHERE id=$1 AND user_id=$2 AND workspace_admin_account_id=$3 AND coalesce(nullif(upstream_key_fingerprint,''),upstream_key)=$5`, id, uid, aid, t.URL, connectionKeyMatch(a.sites.storageCipher, c.UpstreamKey))
	}
	if e != nil {
		if t.Attempt < 3 {
			t.Attempt++
			next, _ := json.Marshal(t)
			return jobs.Outcome{Pending: true, Delay: time.Duration(t.Attempt*15) * time.Second, NextPayload: next, Value: map[string]string{"message": e.Error()}}, nil
		}
		return jobs.Outcome{}, e
	}
	return jobs.Outcome{Value: map[string]string{"status": "synced", "apiUrl": t.URL, "accountId": c.AdminAccountID}}, nil
}
func (a *APIRouting) sync(w http.ResponseWriter, r *http.Request) {
	uid, _ := authctx.UserID(r.Context())
	aid, e := a.sites.currentAdminAccountID(r.Context(), uid)
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
	s, e := a.sites.upstreamLookup.GetSite(r.Context(), req.SiteID)
	if e != nil || s == nil || s.UserID != uid || s.AdminAccountID != aid {
		httpjson.WriteError(w, 404, "上游不存在")
		return
	}
	if e = a.queue(r.Context(), *s, true); e != nil {
		httpjson.WriteError(w, 500, "同步任务提交失败")
		return
	}
	httpjson.Write(w, 202, map[string]bool{"queued": true})
}

func addDomain(out map[string]*domainEntry, raw, label string) {
	u, e := url.Parse(raw)
	if e != nil {
		return
	}
	host, e := upstream.NormalizeUpstreamHost(u.Hostname())
	if e != nil {
		return
	}
	d := out[host]
	if d == nil {
		d = &domainEntry{Host: host, References: []string{}}
		out[host] = d
	}
	for _, v := range d.References {
		if v == label {
			return
		}
	}
	if label != "" {
		d.References = append(d.References, label)
	}
}
func (a *APIRouting) domainReferences(ctx context.Context, uid, aid string) (map[string]*domainEntry, error) {
	out := map[string]*domainEntry{}
	rows, e := a.db.Query(ctx, `SELECT id,name,base_url,settings FROM upstream_sites WHERE user_id=$1 AND admin_account_id=$2`, uid, aid)
	if e != nil {
		return out, e
	}
	sites := map[string]upstream.Site{}
	for rows.Next() {
		var s upstream.Site
		var raw []byte
		if e = rows.Scan(&s.ID, &s.Name, &s.BaseURL, &raw); e != nil {
			rows.Close()
			return out, e
		}
		if e = json.Unmarshal(raw, &s.Settings); e != nil {
			rows.Close()
			return out, e
		}
		sites[s.ID] = s
		addDomain(out, upstream.EffectiveAPIURL(s, "", ""), s.Name)
		for gid := range s.Settings.GroupAPIURLs {
			addDomain(out, upstream.EffectiveAPIURL(s, gid, ""), s.Name+" · 分组 "+gid)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	conns, e := a.sites.connRepository.ListRealConnections(ctx, uid, aid)
	if e != nil {
		return out, e
	}
	for _, c := range conns {
		u := c.UpstreamAPIURL
		if s, ok := sites[c.UpstreamSiteID]; ok {
			u = upstream.EffectiveAPIURL(s, c.UpstreamGroupID, u)
		}
		addDomain(out, u, "绑定账号 "+c.AdminAccountID)
	}
	return out, nil
}
func (a *APIRouting) domains(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid, _ := authctx.UserID(ctx)
	aid, e := a.sites.currentAdminAccountID(ctx, uid)
	if e != nil {
		httpjson.WriteError(w, 403, "请选择工作区")
		return
	}
	var target, status, message string
	var hosts, external []byte
	var updated time.Time
	e = a.db.QueryRow(ctx, `SELECT target_url,active_hosts,external_refs,status,error,updated_at FROM upstream_domain_bridge WHERE id=true`).Scan(&target, &hosts, &external, &status, &message, &updated)
	if e != nil {
		httpjson.WriteError(w, 500, "读取域名配置失败")
		return
	}
	var base string
	e = a.db.QueryRow(ctx, `SELECT base_url FROM my_site_states WHERE user_id=$1 AND admin_account_id=$2`, uid, aid).Scan(&base)
	if e != nil || target == "" || upstream.MonitorAPIBase(base) != upstream.MonitorAPIBase(target) {
		httpjson.Write(w, 200, map[string]any{"enabled": false, "items": []domainEntry{}, "status": "unconfigured", "jobs": []jobs.Job{}})
		return
	}
	refs, e := a.domainReferences(ctx, uid, aid)
	if e != nil {
		httpjson.WriteError(w, 500, "读取域名引用失败")
		return
	}
	var ex []domainReference
	_ = json.Unmarshal(external, &ex)
	for _, v := range ex {
		if v.UserID == uid && v.AdminID == aid {
			addDomain(refs, "https://"+v.Host, v.Label)
		}
	}
	if r.Method != http.MethodGet {
		var req struct {
			Host string `json:"host"`
		}
		if httpjson.Decode(r, &req) != nil {
			httpjson.WriteError(w, 400, "请输入域名")
			return
		}
		host, err := upstream.NormalizeUpstreamHost(req.Host)
		if err != nil {
			httpjson.WriteError(w, 400, err.Error())
			return
		}
		if r.Method == http.MethodPost {
			_, e = a.db.Exec(ctx, `INSERT INTO upstream_domain_manual(user_id,admin_account_id,host) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, uid, aid, host)
		} else {
			_, e = a.db.Exec(ctx, `DELETE FROM upstream_domain_manual WHERE user_id=$1 AND admin_account_id=$2 AND host=$3`, uid, aid, host)
		}
		if e != nil {
			httpjson.WriteError(w, 500, "域名配置保存失败")
			return
		}
		_, _ = a.db.Exec(ctx, `UPDATE upstream_domain_bridge SET status='pending' WHERE id=true`)
		status = "pending"
	}
	rows, e := a.db.Query(ctx, `SELECT host FROM upstream_domain_manual WHERE user_id=$1 AND admin_account_id=$2 ORDER BY host`, uid, aid)
	if e != nil {
		httpjson.WriteError(w, 500, "读取域名列表失败")
		return
	}
	for rows.Next() {
		var host string
		if e = rows.Scan(&host); e != nil {
			rows.Close()
			httpjson.WriteError(w, 500, "读取域名列表失败")
			return
		}
		addDomain(refs, "https://"+host, "")
		refs[host].Manual = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		httpjson.WriteError(w, 500, "读取域名列表失败")
		return
	}
	var active []string
	_ = json.Unmarshal(hosts, &active)
	isActive := map[string]bool{}
	for _, v := range active {
		isActive[v] = true
	}
	list := []domainEntry{}
	for h, v := range refs {
		v.Active = isActive[h]
		sort.Strings(v.References)
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Host < list[j].Host })
	routingJobs := []jobs.Job{}
	jobRows, err := a.db.Query(ctx, `SELECT id FROM operation_jobs WHERE user_id=$1 AND admin_account_id=$2 AND kind='upstream_api_sync' ORDER BY created_at DESC LIMIT 5`, uid, aid)
	if err == nil {
		ids := []string{}
		for jobRows.Next() {
			var id string
			if jobRows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		jobRows.Close()
		for _, id := range ids {
			if j, err := a.jobs.Get(ctx, uid, aid, id); err == nil {
				routingJobs = append(routingJobs, j)
			}
		}
	}
	if status == "synced" && time.Since(updated) > 90*time.Second {
		status = "offline"
		message = "服务器域名同步程序未响应"
	}
	httpjson.Write(w, 200, map[string]any{"enabled": true, "items": list, "status": status, "error": message, "updatedAt": updated, "jobs": routingJobs, "targetUrl": target, "reloadRequired": false})
}
