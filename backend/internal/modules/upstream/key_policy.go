package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultMonitorIP = "206.82.2.46"

type KeyPolicy struct {
	AllowedIPs []string `json:"allowedIPs"`
	Version    int64    `json:"version"`
}
type KeyPolicies struct {
	db              *pgxpool.Pool
	platform        *PlatformService
	validateBinding func(context.Context, string, string, Site, string) error
}

func NewKeyPolicies(db *pgxpool.Pool, p *PlatformService) *KeyPolicies {
	return &KeyPolicies{db: db, platform: p}
}

func (p *KeyPolicies) SetBindingValidator(f func(context.Context, string, string, Site, string) error) {
	p.validateBinding = f
}
func (p *KeyPolicies) Observe(ctx context.Context, uid, aid, site, key string, ips []string) error {
	return p.observe(ctx, uid, aid, site, key, ips)
}
func (p *KeyPolicies) EnsureSchema(ctx context.Context) error {
	_, e := p.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS upstream_key_whitelist_switches(user_id text NOT NULL,admin_account_id text NOT NULL,site_id text NOT NULL,key_id text NOT NULL,enabled boolean NOT NULL,updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(user_id,admin_account_id,site_id,key_id));
 CREATE TABLE IF NOT EXISTS upstream_key_policies(user_id text NOT NULL,admin_account_id text NOT NULL,allowed_ips jsonb NOT NULL,version bigint NOT NULL DEFAULT 1,PRIMARY KEY(user_id,admin_account_id));
 CREATE TABLE IF NOT EXISTS upstream_key_policy_bindings(user_id text NOT NULL,admin_account_id text NOT NULL,site_id text NOT NULL,key_id text NOT NULL,managed_ips jsonb NOT NULL,updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(user_id,admin_account_id,site_id,key_id));
 CREATE TABLE IF NOT EXISTS upstream_group_key_policies(user_id text NOT NULL,admin_account_id text NOT NULL,site_id text NOT NULL,group_id text NOT NULL,allowed_ips jsonb NOT NULL,version bigint NOT NULL DEFAULT 1,PRIMARY KEY(user_id,admin_account_id,site_id,group_id));
 ALTER TABLE upstream_key_policy_bindings ADD COLUMN IF NOT EXISTS observed_ips jsonb;
 ALTER TABLE upstream_key_policy_bindings ADD COLUMN IF NOT EXISTS observed_at timestamptz; CREATE TABLE IF NOT EXISTS upstream_key_ip_overrides (
 user_id text NOT NULL,admin_account_id text NOT NULL,site_id text NOT NULL,key_id text NOT NULL,addresses jsonb NOT NULL,
 PRIMARY KEY(user_id,admin_account_id,site_id,key_id));`)
	return e
}
func NormalizeAllowedIPs(values []string) ([]string, error) {
	if len(values) > 100 {
		return nil, fmt.Errorf("IP 白名单最多 100 项")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if ip, e := netip.ParseAddr(v); e == nil {
			v = ip.String()
		} else if prefix, e := netip.ParsePrefix(v); e == nil {
			v = prefix.Masked().String()
		} else {
			return nil, fmt.Errorf("无效 IP 或 CIDR: %s", v)
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("白名单至少保留一个地址")
	}
	return out, nil
}
func (p *KeyPolicies) Get(ctx context.Context, uid, aid string) (KeyPolicy, error) {
	v := KeyPolicy{AllowedIPs: []string{DefaultMonitorIP}, Version: 0}
	var raw []byte
	e := p.db.QueryRow(ctx, `SELECT allowed_ips,version FROM upstream_key_policies WHERE user_id=$1 AND admin_account_id=$2`, uid, aid).Scan(&raw, &v.Version)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return v, nil
		}
		return v, e
	}
	e = json.Unmarshal(raw, &v.AllowedIPs)
	return v, e
}
func (p *KeyPolicies) AllowedIPs(ctx context.Context, uid, aid string) ([]string, error) {
	v, e := p.Get(ctx, uid, aid)
	return v.AllowedIPs, e
}
func (p *KeyPolicies) Save(ctx context.Context, uid, aid string, v KeyPolicy) (KeyPolicy, error) {
	ips, e := NormalizeAllowedIPs(v.AllowedIPs)
	if e != nil {
		return v, e
	}
	b, _ := json.Marshal(ips)
	err := p.db.QueryRow(ctx, `INSERT INTO upstream_key_policies(user_id,admin_account_id,allowed_ips) VALUES($1,$2,$3) ON CONFLICT(user_id,admin_account_id) DO UPDATE SET allowed_ips=EXCLUDED.allowed_ips,version=upstream_key_policies.version+1 WHERE upstream_key_policies.version=$4 RETURNING version`, uid, aid, b, v.Version).Scan(&v.Version)
	v.AllowedIPs = ips
	if err != nil {
		return v, fmt.Errorf("白名单版本已变化，请重新读取后保存")
	}
	return v, nil
}
func (p *KeyPolicies) Record(ctx context.Context, uid, aid, site, key string, ips []string) error {
	raw, _ := json.Marshal(ips)
	_, e := p.db.Exec(ctx, `INSERT INTO upstream_key_policy_bindings(user_id,admin_account_id,site_id,key_id,managed_ips) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,admin_account_id,site_id,key_id) DO UPDATE SET managed_ips=EXCLUDED.managed_ips,updated_at=now()`, uid, aid, site, key, raw)
	return e
}
func mergeManagedIPs(current, previous, desired []string, replace bool) []string {
	set := map[string]bool{}
	if !replace {
		for _, ip := range current {
			set[ip] = true
		}
		for _, ip := range previous {
			delete(set, ip)
		}
	}
	for _, ip := range desired {
		set[ip] = true
	}
	out := []string{}
	for ip := range set {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}
func (p *KeyPolicies) Apply(ctx context.Context, uid, aid string, site Site, keyID string, replace bool) ([]string, error) {
	return p.ApplyForGroup(ctx, uid, aid, site, keyID, "", replace)
}
func (p *KeyPolicies) ApplyForGroup(ctx context.Context, uid, aid string, site Site, keyID, groupID string, replace bool) ([]string, error) {
	if p.validateBinding != nil {
		if err := p.validateBinding(ctx, uid, aid, site, keyID); err != nil {
			return nil, err
		}
	}
	return p.applyForGroup(ctx, uid, aid, site, keyID, groupID, replace)
}

// Called only for the Key returned by an explicit new-binding creation request.
// It does not authorize later inventory scans or cached security tasks.
func (p *KeyPolicies) ApplyCreatedKeyForGroup(ctx context.Context, uid, aid string, site Site, keyID, groupID string) ([]string, error) {
	return p.applyForGroup(ctx, uid, aid, site, keyID, groupID, false)
}

func (p *KeyPolicies) applyForGroup(ctx context.Context, uid, aid string, site Site, keyID, groupID string, replace bool) ([]string, error) {
	if site.UserID != uid || site.AdminAccountID != aid || site.Session == nil {
		return nil, fmt.Errorf("上游凭据不可用")
	}
	desired, e := p.DesiredIPs(ctx, uid, aid, site, keyID, groupID)
	if e != nil {
		return nil, e
	}
	old := []string{}
	var raw []byte
	if p.db.QueryRow(ctx, `SELECT managed_ips FROM upstream_key_policy_bindings WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND key_id=$4`, uid, aid, site.ID, keyID).Scan(&raw) == nil {
		_ = json.Unmarshal(raw, &old)
	}
	record, current, e := p.platform.readKeyPolicy(ctx, *site.Session, keyID)
	if e != nil {
		return nil, e
	}
	want := mergeManagedIPs(current, old, desired, replace)
	if len(desired) == 0 {
		want = []string{}
	}
	if e = p.platform.writeKeyPolicy(ctx, *site.Session, keyID, record, want); e != nil {
		return nil, e
	}
	_, got, e := p.platform.readKeyPolicy(ctx, *site.Session, keyID)
	if e != nil {
		return nil, e
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return nil, fmt.Errorf("上游未保存预期白名单，可能不支持该功能")
	}
	if e = p.Record(ctx, uid, aid, site.ID, keyID, desired); e != nil {
		return nil, e
	}
	return got, p.observe(ctx, uid, aid, site.ID, keyID, got)
}
func (s *PlatformService) readKeyPolicy(ctx context.Context, session Session, id string) (map[string]any, []string, error) {
	path := "/api/v1/keys/" + url.PathEscape(id)
	opts := requestOptions{AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS, Context: ctx}
	if session.Platform == PlatformNewAPI {
		path = "/api/token/" + url.PathEscape(id)
		opts = newAPIRequestOptions(session)
		opts.Context = ctx
	}
	r, e := s.httpClient.requestJSON(session.BaseURL+path, opts)
	if e != nil {
		return nil, nil, e
	}
	record := dataRecord(r.Payload)
	out := []string{}
	if session.Platform == PlatformNewAPI {
		for _, v := range strings.Split(safeString(record, "allow_ips"), "\n") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
	} else {
		if list, ok := record["ip_whitelist"].([]any); ok {
			for _, v := range list {
				if str, ok := v.(string); ok {
					out = append(out, str)
				}
			}
		}
	}
	return record, out, nil
}
func (s *PlatformService) writeKeyPolicy(ctx context.Context, session Session, id string, record map[string]any, ips []string) error {
	path := "/api/v1/keys/" + url.PathEscape(id)
	opts := requestOptions{AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: session.InsecureSkipTLS, Context: ctx, Method: http.MethodPut, Body: map[string]any{"ip_whitelist": ips}}
	if session.Platform == PlatformNewAPI {
		path = "/api/token/"
		body := map[string]any{}
		for _, k := range []string{"id", "name", "status", "expired_time", "remain_quota", "unlimited_quota", "model_limits_enabled", "model_limits", "group", "group_config", "preset_id", "sticky_session", "remark", "cross_group_retry", "auto_groups", "allow_user_quota_query"} {
			if v, ok := record[k]; ok {
				body[k] = v
			}
		}
		if body["id"] == nil {
			return fmt.Errorf("上游 Token 配置不完整，已跳过更新")
		}
		body["allow_ips"] = strings.Join(ips, "\n")
		opts = newAPIRequestOptionsWith(session, http.MethodPut, body)
		opts.Context = ctx
	}
	_, e := s.httpClient.requestJSON(session.BaseURL+path, opts)
	return e
}
