package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/shared/httpjson"
)

type GroupKeyPolicy struct {
	KeyPolicy
	Inherited   bool       `json:"inherited"`
	ObservedIPs []string   `json:"observedIPs"`
	ObservedAt  *time.Time `json:"observedAt,omitempty"`
	BoundKeys   int        `json:"boundKeys"`
}

func (p *KeyPolicies) Group(ctx context.Context, uid, aid, site, group string) (GroupKeyPolicy, error) {
	base, e := p.Get(ctx, uid, aid)
	v := GroupKeyPolicy{KeyPolicy: base, Inherited: true, ObservedIPs: []string{}}
	if e != nil {
		return v, e
	}
	// The version here belongs to the override, independent of the workspace policy.
	v.Version = 0
	var raw []byte
	e = p.db.QueryRow(ctx, `SELECT allowed_ips,version FROM upstream_group_key_policies WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND group_id=$4`, uid, aid, site, group).Scan(&raw, &v.Version)
	if e == nil {
		v.Inherited = false
		e = json.Unmarshal(raw, &v.AllowedIPs)
	} else if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	return v, e
}
func (p *KeyPolicies) AllowedIPsForGroup(ctx context.Context, uid, aid, site, group string) ([]string, error) {
	var disabled bool
	if err := p.db.QueryRow(ctx, `SELECT coalesce((settings->>'keyWhitelistDisabled')::boolean,false) FROM upstream_sites WHERE id=$1 AND user_id=$2 AND admin_account_id=$3`, site, uid, aid).Scan(&disabled); err != nil {
		return nil, err
	}
	if disabled {
		return []string{}, nil
	}

	v, e := p.Group(ctx, uid, aid, site, group)
	return v.AllowedIPs, e
}
func (p *KeyPolicies) SaveGroup(ctx context.Context, uid, aid, site, group string, v GroupKeyPolicy) (GroupKeyPolicy, error) {
	var raw []byte
	if !v.Inherited {
		ips, e := NormalizeAllowedIPs(v.AllowedIPs)
		if e != nil {
			return v, e
		}
		raw, _ = json.Marshal(ips)
	}
	tx, e := p.db.Begin(ctx)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "key-policy|"+uid+"|"+aid+"|"+site+"|"+group); e != nil {
		return v, e
	}
	var current int64
	e = tx.QueryRow(ctx, `SELECT version FROM upstream_group_key_policies WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND group_id=$4`, uid, aid, site, group).Scan(&current)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return v, e
	}
	if current != v.Version {
		return v, fmt.Errorf("分组策略已变化，请重新读取")
	}
	if v.Inherited {
		_, e = tx.Exec(ctx, `DELETE FROM upstream_group_key_policies WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND group_id=$4`, uid, aid, site, group)
	} else {
		_, e = tx.Exec(ctx, `INSERT INTO upstream_group_key_policies(user_id,admin_account_id,site_id,group_id,allowed_ips) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,admin_account_id,site_id,group_id) DO UPDATE SET allowed_ips=EXCLUDED.allowed_ips,version=upstream_group_key_policies.version+1`, uid, aid, site, group, raw)
	}
	if e != nil {
		return v, e
	}
	if e = tx.Commit(ctx); e != nil {
		return v, e
	}
	return p.Group(ctx, uid, aid, site, group)
}

func (p *KeyPolicies) observe(ctx context.Context, uid, aid, site, key string, ips []string) error {
	raw, _ := json.Marshal(ips)
	_, e := p.db.Exec(ctx, `INSERT INTO upstream_key_policy_bindings(user_id,admin_account_id,site_id,key_id,managed_ips,observed_ips,observed_at) VALUES($1,$2,$3,$4,'[]',$5,now()) ON CONFLICT(user_id,admin_account_id,site_id,key_id) DO UPDATE SET observed_ips=EXCLUDED.observed_ips,observed_at=now()`, uid, aid, site, key, raw)
	return e
}
func (p *KeyPolicies) groupView(ctx context.Context, uid, aid, site, group string) (GroupKeyPolicy, error) {
	v, e := p.Group(ctx, uid, aid, site, group)
	if e != nil {
		return v, e
	}
	rows, e := p.db.Query(ctx, `SELECT DISTINCT c.upstream_key_id,b.observed_ips,b.observed_at FROM real_connections c LEFT JOIN upstream_key_policy_bindings b ON b.user_id=c.user_id AND b.admin_account_id=c.workspace_admin_account_id AND b.site_id=c.upstream_site_id AND b.key_id=c.upstream_key_id WHERE c.user_id=$1 AND c.workspace_admin_account_id=$2 AND c.upstream_site_id=$3 AND c.upstream_group_id=$4 AND c.upstream_key_id<>''`, uid, aid, site, group)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var id string
		var raw []byte
		var at *time.Time
		if e = rows.Scan(&id, &raw, &at); e != nil {
			return v, e
		}
		v.BoundKeys++
		var ips []string
		_ = json.Unmarshal(raw, &ips)
		for _, ip := range ips {
			if !seen[ip] {
				seen[ip] = true
				v.ObservedIPs = append(v.ObservedIPs, ip)
			}
		}
		if at != nil && (v.ObservedAt == nil || at.Before(*v.ObservedAt)) {
			v.ObservedAt = at
		}
	}
	return v, rows.Err()
}

type groupKeyJob struct {
	SiteID, GroupID string
	ReadOnly        bool
	Replace         bool
}

func (s *HealthService) registerGroupKeyPolicies(mux *http.ServeMux, j *jobs.Service, p *KeyPolicies) {
	j.Register("group_key_policy", func(ctx context.Context, uid, aid, key string, raw json.RawMessage) (jobs.Outcome, error) {
		var req groupKeyJob
		if e := json.Unmarshal(raw, &req); e != nil {
			return jobs.Outcome{}, e
		}
		site, e := s.ownedSite(ctx, uid, aid, req.SiteID)
		if e != nil {
			return jobs.Outcome{}, e
		}
		var exists bool
		e = p.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_group_id=$4 AND upstream_key_id=$5)`, uid, aid, site.ID, req.GroupID, key).Scan(&exists)
		if e != nil {
			return jobs.Outcome{}, e
		}
		if !exists || site.Session == nil {
			return jobs.Outcome{}, fmt.Errorf("绑定 Key 不存在")
		}
		var ips []string
		if req.ReadOnly {
			_, ips, e = p.platform.readKeyPolicy(ctx, *site.Session, key)
			if e == nil {
				e = p.observe(ctx, uid, aid, site.ID, key, ips)
			}
		} else {
			ips, e = p.ApplyForGroup(ctx, uid, aid, *site, key, req.GroupID, req.Replace)
		}
		return jobs.Outcome{Value: ips}, e
	})
	handler := func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := s.scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		site, e := s.ownedSite(r.Context(), uid, aid, r.PathValue("id"))
		if e != nil {
			httpjson.WriteError(w, 404, "上游不存在")
			return
		}
		group := r.URL.Query().Get("groupId")
		found := false
		for _, g := range site.Metrics.Groups {
			if g.ID == group {
				found = true
				break
			}
		}
		if !found {
			httpjson.WriteError(w, 404, "分组不存在")
			return
		}
		if r.Method == http.MethodGet {
			v, e := p.groupView(r.Context(), uid, aid, site.ID, group)
			if e != nil {
				httpjson.WriteError(w, 500, "无法读取白名单")
				return
			}
			httpjson.Write(w, 200, v)
			return
		}
		replace := false
		if r.Method == http.MethodPut {
			var req struct {
				GroupKeyPolicy
				Replace bool `json:"replace"`
			}
			if e = httpjson.Decode(r, &req); e == nil {
				_, e = p.SaveGroup(r.Context(), uid, aid, site.ID, group, req.GroupKeyPolicy)
				replace = req.Replace
			}
			if e != nil {
				httpjson.WriteError(w, 400, e.Error())
				return
			}
		}
		rows, e := p.db.Query(r.Context(), `SELECT DISTINCT upstream_key_id FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_group_id=$4 AND upstream_key_id<>''`, uid, aid, site.ID, group)
		if e != nil {
			httpjson.WriteError(w, 500, "无法读取绑定 Key")
			return
		}
		raw, _ := json.Marshal(groupKeyJob{SiteID: site.ID, GroupID: group, ReadOnly: r.Method == http.MethodPost, Replace: replace})
		items := []jobs.Item{}
		for rows.Next() {
			var key string
			if e = rows.Scan(&key); e != nil {
				break
			}
			items = append(items, jobs.Item{ID: key, Payload: raw})
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			httpjson.WriteError(w, 500, "无法读取绑定 Key")
			return
		}
		v, e := p.groupView(r.Context(), uid, aid, site.ID, group)
		if e != nil {
			httpjson.WriteError(w, 500, "无法读取策略")
			return
		}
		if len(items) == 0 {
			httpjson.Write(w, 200, map[string]any{"policy": v})
			return
		}
		job, e := j.Submit(r.Context(), uid, aid, "group_key_policy", items)
		if e != nil {
			httpjson.WriteError(w, 500, e.Error())
			return
		}
		httpjson.Write(w, 202, map[string]any{"policy": v, "job": job})
	}
	mux.HandleFunc("GET /api/upstream-sites/{id}/group-key-policy", handler)
	mux.HandleFunc("PUT /api/upstream-sites/{id}/group-key-policy", handler)
	mux.HandleFunc("POST /api/upstream-sites/{id}/group-key-policy/refresh", handler)
}
