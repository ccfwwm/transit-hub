package my_sites

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

type KeySecurityPolicy struct {
	Enabled              bool       `json:"enabled"`
	AuditEnabled         bool       `json:"auditEnabled"`
	AuditIntervalMinutes int        `json:"auditIntervalMinutes"`
	Time                 string     `json:"time"`
	Timezone             string     `json:"timezone"`
	IntervalHours        int        `json:"intervalHours"`
	Version              int64      `json:"version"`
	NextRotation         time.Time  `json:"nextRotation"`
	LastAudit            *time.Time `json:"lastAudit,omitempty"`
}
type KeySecurity struct {
	db       *pgxpool.Pool
	sites    *Service
	policies *upstream.KeyPolicies
	jobs     *jobs.Service
	cipher   *upstream.SiteCredentialCipher
}
type securityTarget struct {
	SiteID  string `json:"siteId"`
	KeyID   string `json:"keyId"`
	GroupID string `json:"groupId"`
	Action  string `json:"action"`
}
type rotationState struct {
	ID, Site, OldID, NewID, NewSecret, OldSecret, Phase string
	Config                                              map[string]any
	Naming                                              rotationNaming
}

func NewKeySecurity(db *pgxpool.Pool, s *Service, p *upstream.KeyPolicies, j *jobs.Service, secret string) (*KeySecurity, error) {
	var cipher *upstream.SiteCredentialCipher
	var e error
	if strings.TrimSpace(secret) != "" {
		cipher, e = upstream.NewSiteCredentialCipher(secret)
		if e != nil {
			return nil, e
		}
	}
	k := &KeySecurity{db: db, sites: s, policies: p, jobs: j, cipher: cipher}
	if p != nil {
		p.SetBindingValidator(func(ctx context.Context, uid, aid string, site upstream.Site, key string) error {
			bound, err := k.validateBoundConsumers(ctx, uid, aid, site, []string{key}, nil)
			if err != nil {
				return err
			}
			if !bound {
				return fmt.Errorf("Key 未被当前自有 Sub2API 绑定使用，未修改白名单")
			}
			return nil
		})
	}
	_, e = db.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS key_security_policies(user_id text NOT NULL,admin_account_id text NOT NULL,enabled boolean NOT NULL DEFAULT true,audit_enabled boolean NOT NULL DEFAULT true,local_time text NOT NULL DEFAULT '04:00',timezone text NOT NULL DEFAULT 'Asia/Shanghai',interval_hours int NOT NULL DEFAULT 24,version bigint NOT NULL DEFAULT 1,next_rotation timestamptz NOT NULL,last_audit timestamptz,PRIMARY KEY(user_id,admin_account_id));
	 CREATE TABLE IF NOT EXISTS key_security_site_refresh(user_id text NOT NULL,admin_account_id text NOT NULL,site_id text NOT NULL,queued_at timestamptz NOT NULL,PRIMARY KEY(user_id,admin_account_id,site_id));
	 CREATE TABLE IF NOT EXISTS key_security_observation_claims(user_id text NOT NULL,admin_account_id text NOT NULL,site_id text NOT NULL,key_id text NOT NULL,queued_at timestamptz NOT NULL,PRIMARY KEY(user_id,admin_account_id,site_id,key_id));
 CREATE TABLE IF NOT EXISTS key_security_rotations(id text PRIMARY KEY,user_id text NOT NULL,admin_account_id text NOT NULL,site_id text NOT NULL,old_key_id text NOT NULL,new_key_id text NOT NULL DEFAULT '',old_secret text NOT NULL,new_secret text NOT NULL,config jsonb NOT NULL,phase text NOT NULL DEFAULT 'prepared',error text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,admin_account_id,site_id,old_key_id));
 CREATE TABLE IF NOT EXISTS key_security_events(id bigserial PRIMARY KEY,user_id text NOT NULL,admin_account_id text NOT NULL,site_id text NOT NULL,key_id text NOT NULL,action text NOT NULL,status text NOT NULL,message text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
 CREATE INDEX IF NOT EXISTS idx_key_security_events_workspace ON key_security_events(user_id,admin_account_id,created_at DESC);`)
	if e != nil {
		return nil, e
	}
	if _, e = db.Exec(context.Background(), `ALTER TABLE key_security_policies ADD COLUMN IF NOT EXISTS last_discovery timestamptz;
	 ALTER TABLE key_security_policies ADD COLUMN IF NOT EXISTS audit_interval_minutes int NOT NULL DEFAULT 5;
	 ALTER TABLE key_security_rotations ADD COLUMN IF NOT EXISTS group_id text NOT NULL DEFAULT '';
	 ALTER TABLE key_security_rotations ADD COLUMN IF NOT EXISTS naming jsonb NOT NULL DEFAULT '{}';
	 UPDATE key_security_rotations r SET group_id=coalesce((SELECT c.upstream_group_id FROM real_connections c WHERE c.user_id=r.user_id AND c.workspace_admin_account_id=r.admin_account_id AND c.upstream_site_id=r.site_id AND c.upstream_key_id IN(r.old_key_id,r.new_key_id) LIMIT 1),'') WHERE r.group_id='';`); e != nil {
		return nil, e
	}
	j.Register("key_security", k.runTarget)
	j.Register("key_security_scan", k.scanSite)
	j.Register("key_security_bindings", func(ctx context.Context, uid, aid, id string, _ json.RawMessage) (jobs.Outcome, error) {
		removed, cleanupErr := k.pruneDeletedBindings(ctx, uid, aid)
		if cleanupErr != nil {
			k.event(ctx, uid, aid, securityTarget{Action: "cleanup"}, "failed", cleanupErr.Error())
		}
		v, e := s.syncMonitorBindings(ctx, uid, aid, MonitorBindingRequest{}, false)
		// A failed discovery is visible and must not prevent rotating bindings
		// whose identity was already verified on an earlier run.
		_, _ = db.Exec(ctx, `UPDATE key_security_policies SET last_discovery=now() WHERE user_id=$1 AND admin_account_id=$2`, uid, aid)
		return jobs.Outcome{Value: map[string]any{"message": fmt.Sprintf("已匹配 %d 个渠道，清理 %d 个已删除条目，%d 个未匹配", v.Matched, removed, v.Skipped), "matched": v.Matched, "skipped": v.Skipped, "removed": removed, "items": v.Items}}, e
	})
	return k, nil
}

func defaultSecurityPolicy() KeySecurityPolicy {
	return KeySecurityPolicy{Enabled: true, AuditEnabled: true, AuditIntervalMinutes: 5, Time: "04:00", Timezone: "Asia/Shanghai", IntervalHours: 24}
}
func validateSecurityPolicy(p KeySecurityPolicy) error {
	if len(p.Time) != 5 {
		return fmt.Errorf("更新时间格式应为 HH:MM")
	}
	if _, e := time.Parse("15:04", p.Time); e != nil {
		return fmt.Errorf("更新时间无效")
	}
	if _, e := time.LoadLocation(p.Timezone); e != nil {
		return fmt.Errorf("时区无效")
	}
	if p.IntervalHours < 1 || p.IntervalHours > 24*90 {
		return fmt.Errorf("更新间隔应为 1 至 2160 小时")
	}
	if p.AuditIntervalMinutes < 1 || p.AuditIntervalMinutes > 1440 {
		return fmt.Errorf("巡检间隔应为 1 至 1440 分钟")
	}
	return nil
}
func nextSecurityRotation(now time.Time, p KeySecurityPolicy) time.Time {
	loc, _ := time.LoadLocation(p.Timezone)
	v := now.In(loc)
	t, _ := time.Parse("15:04", p.Time)
	next := time.Date(v.Year(), v.Month(), v.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	for !next.After(now) {
		next = next.Add(time.Duration(p.IntervalHours) * time.Hour)
	}
	return next
}
func (k *KeySecurity) policy(ctx context.Context, uid, aid string) (KeySecurityPolicy, error) {
	p := defaultSecurityPolicy()
	p.NextRotation = nextSecurityRotation(time.Now(), p)
	e := k.db.QueryRow(ctx, `SELECT enabled,audit_enabled,local_time,timezone,interval_hours,version,next_rotation,last_audit,audit_interval_minutes FROM key_security_policies WHERE user_id=$1 AND admin_account_id=$2`, uid, aid).Scan(&p.Enabled, &p.AuditEnabled, &p.Time, &p.Timezone, &p.IntervalHours, &p.Version, &p.NextRotation, &p.LastAudit, &p.AuditIntervalMinutes)
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	return p, e
}
func (k *KeySecurity) savePolicy(ctx context.Context, uid, aid string, p KeySecurityPolicy) (KeySecurityPolicy, error) {
	if e := validateSecurityPolicy(p); e != nil {
		return p, e
	}
	p.NextRotation = nextSecurityRotation(time.Now(), p)
	e := k.db.QueryRow(ctx, `INSERT INTO key_security_policies(user_id,admin_account_id,enabled,audit_enabled,local_time,timezone,interval_hours,next_rotation,audit_interval_minutes)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(user_id,admin_account_id) DO UPDATE SET
		enabled=EXCLUDED.enabled,audit_enabled=EXCLUDED.audit_enabled,local_time=EXCLUDED.local_time,timezone=EXCLUDED.timezone,
		next_rotation=CASE WHEN (key_security_policies.enabled,key_security_policies.local_time,key_security_policies.timezone,key_security_policies.interval_hours)
			IS DISTINCT FROM (EXCLUDED.enabled,EXCLUDED.local_time,EXCLUDED.timezone,EXCLUDED.interval_hours)
			THEN EXCLUDED.next_rotation ELSE key_security_policies.next_rotation END,
		interval_hours=EXCLUDED.interval_hours,audit_interval_minutes=EXCLUDED.audit_interval_minutes,
		version=key_security_policies.version+1 WHERE key_security_policies.version=$10 RETURNING version,next_rotation,last_audit`,
		uid, aid, p.Enabled, p.AuditEnabled, p.Time, p.Timezone, p.IntervalHours, p.NextRotation, p.AuditIntervalMinutes, p.Version).Scan(&p.Version, &p.NextRotation, &p.LastAudit)
	if e != nil {
		return p, fmt.Errorf("安全策略已变化，请刷新后重试")
	}
	return p, nil
}
func (k *KeySecurity) event(ctx context.Context, uid, aid string, t securityTarget, status, message string) {
	_, _ = k.db.Exec(ctx, `INSERT INTO key_security_events(user_id,admin_account_id,site_id,key_id,action,status,message) VALUES($1,$2,$3,$4,$5,$6,$7)`, uid, aid, t.SiteID, t.KeyID, t.Action, status, message)
}
func (k *KeySecurity) ownedSite(ctx context.Context, uid, aid, id string) (*upstream.Site, error) {
	s, e := k.sites.upstreamLookup.GetSite(ctx, id)
	if e != nil || s == nil || s.UserID != uid || s.AdminAccountID != aid || s.Session == nil {
		return nil, fmt.Errorf("上游不存在或未登录")
	}
	return s, nil
}
func (k *KeySecurity) targets(ctx context.Context, uid, aid, action string, selected ...[]string) ([]jobs.Item, error) {
	// Binding discovery is a separate explicit operation. Both managed and imported
	// connections are included here; no monitor/takeover/paused filter is applied.
	var ids []string
	if len(selected) > 0 {
		ids = selected[0]
	}
	rows, e := k.db.Query(ctx, `SELECT DISTINCT c.upstream_site_id,coalesce(r.old_key_id,c.upstream_key_id),coalesce(r.group_id,c.upstream_group_id)
		FROM real_connections c LEFT JOIN key_security_rotations r
		ON $4='rotate' AND r.user_id=c.user_id AND r.admin_account_id=c.workspace_admin_account_id
		AND r.site_id=c.upstream_site_id AND r.new_key_id=c.upstream_key_id AND r.phase NOT IN('done','cancelled')
		WHERE c.user_id=$1 AND c.workspace_admin_account_id=$2 AND c.upstream_key_id<>'' AND c.admin_account_id<>'' AND c.upstream_key<>''
		AND ($3::text[] IS NULL OR c.id=ANY($3::text[])) ORDER BY 1,2,3`, uid, aid, ids, action)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []jobs.Item{}
	seen := map[string]bool{}
	for rows.Next() {
		var t securityTarget
		t.Action = action
		if e = rows.Scan(&t.SiteID, &t.KeyID, &t.GroupID); e != nil {
			return nil, e
		}
		id := t.SiteID + "/" + t.KeyID
		if seen[id] {
			continue
		}
		seen[id] = true
		raw, _ := json.Marshal(t)
		out = append(out, jobs.Item{ID: id, Payload: raw})
	}
	return out, rows.Err()
}
func rotationPlanVersion(items []jobs.Item) string {
	hash := sha256.New()
	for _, item := range items {
		hash.Write(item.Payload)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (k *KeySecurity) rotationPreview(ctx context.Context, uid, aid string, ids []string, total int) (map[string]any, error) {
	items, err := k.targets(ctx, uid, aid, "rotate", ids)
	if err != nil {
		return nil, err
	}
	var eligible int
	err = k.db.QueryRow(ctx, `SELECT count(*) FROM real_connections
		WHERE user_id=$1 AND workspace_admin_account_id=$2 AND id=ANY($3::text[])
		AND upstream_key_id<>'' AND admin_account_id<>'' AND upstream_key<>''`, uid, aid, ids).Scan(&eligible)
	return map[string]any{"keys": len(items), "skipped": total - eligible, "version": rotationPlanVersion(items)}, err
}

func (k *KeySecurity) submit(ctx context.Context, uid, aid, action string) (jobs.Job, error) {
	items, e := k.targets(ctx, uid, aid, action)
	if e != nil {
		return jobs.Job{}, e
	}
	if len(items) == 0 {
		return jobs.Job{}, fmt.Errorf("当前工作区没有绑定 Key，请先同步监控绑定")
	}
	return k.jobs.Submit(ctx, uid, aid, "key_security", items)
}

func (k *KeySecurity) runTarget(ctx context.Context, uid, aid, id string, raw json.RawMessage) (out jobs.Outcome, err error) {
	if k.cipher == nil {
		return out, fmt.Errorf("请先配置凭据加密密钥")
	}
	var t securityTarget
	if json.Unmarshal(raw, &t) != nil || id != t.SiteID+"/"+t.KeyID {
		return out, fmt.Errorf("无效安全任务")
	}
	defer func() {
		if err != nil {
			k.event(ctx, uid, aid, t, "failed", err.Error())
		}
	}()
	// Advisory lock covers audit, manual and scheduled rotations, including two
	// different workspaces attempting to manage the same upstream account key.
	lock, e := k.db.Acquire(ctx)
	if e != nil {
		return out, e
	}
	defer lock.Release()
	var locked bool
	e = lock.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "key-security|"+t.SiteID+"|"+t.KeyID).Scan(&locked)
	if e != nil {
		return out, e
	}
	if !locked {
		return jobs.Outcome{Pending: true, Delay: 5 * time.Second}, nil
	}
	defer lock.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "key-security|"+t.SiteID+"|"+t.KeyID)
	site, e := k.ownedSite(ctx, uid, aid, t.SiteID)
	if e != nil {
		return out, e
	}
	if k.sites.keyManagement != nil {
		pending, err := k.sites.keyManagement.pendingKeySplit(ctx, uid, aid, t.SiteID, t.KeyID)
		if err != nil {
			return out, err
		}
		if pending {
			return out, fmt.Errorf("此 Key 有未完成的拆分，请先恢复拆分任务")
		}
	}
	// A retry after cutover must finish the persisted rotation, even though local
	// bindings now reference its replacement key.
	r, e := k.loadRotation(ctx, uid, aid, t)
	existing := e == nil
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if t.Action == "whitelist" && existing {
		if r.Phase != "done" && r.Phase != "cancelled" {
			return out, fmt.Errorf("此 Key 有未完成的轮换，请先完成轮换")
		}
		existing = false
	}
	if existing && (r.Phase == "done" || r.Phase == "cancelled") {
		return k.rotate(ctx, uid, aid, *site, t)
	}
	keyIDs := []string{t.KeyID}
	var secrets []string
	if existing {
		keyIDs = append(keyIDs, r.NewID)
		secrets = []string{r.OldSecret, r.NewSecret}
	} else if t.Action == "audit" {
		consumers, err := k.boundConsumers(ctx, uid, aid, site.ID, keyIDs)
		if err != nil {
			return out, err
		}
		if len(consumers) == 0 {
			return skippedUnboundKey(), nil
		}
		record, ips, err := k.sites.platformService.ReadSecurityKey(ctx, *site.Session, t.KeyID)
		if err != nil {
			return out, err
		}
		if !upstream.SecurityKeyGroupMatches(site.Platform, record, t.GroupID) {
			return out, fmt.Errorf("上游 Key 分组已变化，请重新同步绑定")
		}
		exact, err := k.policies.VerifyObservedIPs(ctx, uid, aid, *site, t.KeyID, t.GroupID, ips)
		if err != nil {
			return out, err
		}
		if exact {
			return k.whitelistMatch(ctx, uid, aid, t), nil
		}
	}
	bound, e := k.validateBoundConsumers(ctx, uid, aid, *site, keyIDs, secrets)
	if e != nil {
		return out, e
	}
	if !bound {
		return skippedUnboundKey(), nil
	}
	if existing {
		_, _ = k.db.Exec(ctx, `UPDATE key_security_rotations SET updated_at=now() WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND old_key_id=$4 AND phase<>'done'`, uid, aid, t.SiteID, t.KeyID)
		return k.rotate(ctx, uid, aid, *site, t)
	}
	record, _, e := k.sites.platformService.ReadSecurityKey(ctx, *site.Session, t.KeyID)
	if e != nil {
		return out, e
	}
	if !upstream.SecurityKeyGroupMatches(site.Platform, record, t.GroupID) {
		return out, fmt.Errorf("上游 Key 分组已变化，请重新同步绑定")
	}
	enabled, e := k.policies.WhitelistEnabled(ctx, uid, aid, site.ID, t.KeyID)
	if e != nil {
		return out, e
	}
	if t.Action == "whitelist" || !enabled && t.Action != "rotate" {
		ips, e := k.policies.ApplyForGroup(ctx, uid, aid, *site, t.KeyID, t.GroupID, true)
		if e != nil {
			return out, e
		}
		message := "白名单已启用并回读验证"
		if !enabled {
			message = "白名单已关闭并回读验证"
		}
		k.event(ctx, uid, aid, t, "done", message)
		return jobs.Outcome{Value: map[string]any{"enabled": enabled, "allowedIPs": ips, "message": message}}, nil
	}
	exact := false
	if t.Action != "audit" {
		exact, e = k.policies.VerifyExact(ctx, uid, aid, *site, t.KeyID, t.GroupID)
		if e != nil {
			return out, e
		}
	}
	// Exact replacement is intentional: an injected IP must never be merged back.
	if !exact {
		// Persist the rotation before changing the remote whitelist: a restart
		// between those two writes must still finish the mandatory key rotation.
		out, e := k.rotate(ctx, uid, aid, *site, t)
		if e != nil {
			if _, repairErr := k.policies.ApplyForGroup(ctx, uid, aid, *site, t.KeyID, t.GroupID, true); repairErr != nil {
				return out, fmt.Errorf("轮换未完成，白名单修复也失败，请查看上游权限")
			}
		}
		return out, e
	}
	if t.Action == "rotate" {
		return k.rotate(ctx, uid, aid, *site, t)
	}
	return k.whitelistMatch(ctx, uid, aid, t), nil
}

func skippedUnboundKey() jobs.Outcome {
	return jobs.Outcome{Value: map[string]any{"skipped": true, "message": "Key 未被当前自有 Sub2API 绑定使用，已跳过"}}
}

func (k *KeySecurity) whitelistMatch(ctx context.Context, uid, aid string, t securityTarget) jobs.Outcome {
	const message = "白名单一致"
	k.event(ctx, uid, aid, t, "done", message)
	return jobs.Outcome{Value: map[string]any{"message": message, "keyId": t.KeyID, "siteId": t.SiteID}}
}

func randomSecuritySecret() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return "sk-" + hex.EncodeToString(b), nil
}
func (k *KeySecurity) loadRotation(ctx context.Context, uid, aid string, t securityTarget) (rotationState, error) {
	var r rotationState
	var raw, naming []byte
	e := k.db.QueryRow(ctx, `SELECT id,site_id,old_key_id,new_key_id,old_secret,new_secret,config,phase,naming FROM key_security_rotations WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND old_key_id=$4`, uid, aid, t.SiteID, t.KeyID).Scan(&r.ID, &r.Site, &r.OldID, &r.NewID, &r.OldSecret, &r.NewSecret, &raw, &r.Phase, &naming)
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(raw, &r.Config); e != nil {
		return r, e
	}
	if e = json.Unmarshal(naming, &r.Naming); e != nil {
		return r, e
	}
	if r.Phase == "done" || r.Phase == "cancelled" {
		return r, nil
	}
	r.OldSecret, e = k.cipher.Decrypt(r.OldSecret)
	if e != nil {
		return r, e
	}
	r.NewSecret, e = k.cipher.Decrypt(r.NewSecret)
	return r, e
}
func (k *KeySecurity) phase(ctx context.Context, r rotationState, phase string) error {
	encrypted, e := k.cipher.Encrypt(r.NewSecret)
	if e != nil {
		return e
	}
	_, e = k.db.Exec(ctx, `UPDATE key_security_rotations SET phase=$2,new_key_id=$3,new_secret=$4,error='',updated_at=now() WHERE id=$1`, r.ID, phase, r.NewID, encrypted)
	return e
}
func (k *KeySecurity) rotate(ctx context.Context, uid, aid string, site upstream.Site, t securityTarget) (jobs.Outcome, error) {
	out := jobs.Outcome{}
	r, e := k.loadRotation(ctx, uid, aid, t)
	if errors.Is(e, pgx.ErrNoRows) {
		conns, e := k.sites.connRepository.ListRealConnections(ctx, uid, aid)
		if e != nil {
			return out, e
		}
		old := ""
		count := 0
		for _, c := range conns {
			if c.UpstreamSiteID == site.ID && c.UpstreamKeyID == t.KeyID {
				if old != "" && old != c.UpstreamKey {
					return out, fmt.Errorf("同一 Key 存在不同本地凭据，已停止")
				}
				old = c.UpstreamKey
				count++
			}
		}
		if count == 0 || old == "" {
			return out, fmt.Errorf("没有可轮换的绑定凭据")
		}
		var shared bool
		e = k.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM real_connections WHERE coalesce(nullif(upstream_key_fingerprint,''),upstream_key)=$3 AND (user_id<>$1 OR workspace_admin_account_id<>$2 OR upstream_site_id<>$4))`, uid, aid, connectionKeyMatch(k.sites.storageCipher, old), site.ID).Scan(&shared)
		if e != nil {
			return out, e
		}
		if shared {
			return out, fmt.Errorf("Key 被其他工作区共用，需先拆分独立 Key")
		}
		record, _, e := k.sites.platformService.ReadSecurityKey(ctx, *site.Session, t.KeyID)
		if e != nil {
			return out, e
		}
		if !upstream.SecurityKeyGroupMatches(site.Platform, record, t.GroupID) {
			return out, fmt.Errorf("分组已变化，已停止轮换")
		}
		keyFromUpstream, _ := record["key"].(string)
		if site.Platform == upstream.PlatformNewAPI {
			keyFromUpstream, e = k.sites.platformService.SecurityTokenKey(ctx, *site.Session, t.KeyID)
			if e != nil {
				return out, fmt.Errorf("无法核对完整上游 Key")
			}
		}
		if !upstream.SameMonitorKey(site.Platform, old, keyFromUpstream) {
			return out, fmt.Errorf("本地与上游 Key 不一致，已停止轮换")
		}
		secret, e := randomSecuritySecret()
		if e != nil {
			return out, e
		}
		rid, e := randomConnID()
		if e != nil {
			return out, e
		}
		ips, e := k.policies.DesiredIPs(ctx, uid, aid, site, t.KeyID, t.GroupID)
		if e != nil {
			return out, e
		}
		if k.sites.keyManagement != nil {
			if e = k.sites.keyManagement.alias(ctx, uid, aid, site.ID, t.KeyID, keyString(record["name"])); e != nil {
				return out, e
			}
		}
		config, e := upstream.SecurityCloneConfig(site.Platform, record, "THrotate-"+rid, ips)
		if e != nil {
			return out, e
		}
		oldEnc, e := k.cipher.Encrypt(old)
		if e != nil {
			return out, e
		}
		newEnc, e := k.cipher.Encrypt(secret)
		if e != nil {
			return out, e
		}
		raw, _ := json.Marshal(config)
		naming := rotationNaming{PreviousName: keyString(record["name"])}
		_, e = k.db.Exec(ctx, `INSERT INTO key_security_rotations(id,user_id,admin_account_id,site_id,old_key_id,old_secret,new_secret,config,group_id,naming) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, rid, uid, aid, site.ID, t.KeyID, oldEnc, newEnc, raw, t.GroupID, naming)
		if e != nil {
			return out, e
		}
		r = rotationState{ID: rid, Site: site.ID, OldID: t.KeyID, OldSecret: old, NewSecret: secret, Config: config, Phase: "prepared", Naming: naming}
	} else if e != nil {
		return out, e
	}
	if r.Phase == "done" {
		return rotationResult(r), nil
	}
	if r.Phase == "cancelled" {
		return out, fmt.Errorf("原账号或凭据已移除，已停止此旧绑定轮换；请重新同步绑定")
	}
	session, e := k.sites.RequireSession(ctx, uid, aid)
	if e != nil {
		return out, fmt.Errorf("自有站管理员会话不可用")
	}
	if session.Platform != upstream.PlatformSub2API {
		return out, fmt.Errorf("仅支持轮换已绑定自有 Sub2API 的 Key")
	}
	if r.Phase == "prepared" {
		// Re-read mutable restrictions immediately before creating the successor.
		// This also avoids resetting spending incurred while media was finishing.
		record, _, e := k.sites.platformService.ReadSecurityKey(ctx, *site.Session, r.OldID)
		if e != nil {
			return out, e
		}
		if !upstream.SecurityKeyGroupMatches(site.Platform, record, t.GroupID) {
			return out, fmt.Errorf("轮换期间上游分组已变化，请重新核对")
		}
		exact, e := k.policies.VerifyExact(ctx, uid, aid, site, r.OldID, t.GroupID)
		if e != nil {
			return out, e
		}
		if !exact {
			if _, e = k.policies.ApplyForGroup(ctx, uid, aid, site, r.OldID, t.GroupID, true); e != nil {
				return out, fmt.Errorf("白名单修复失败，旧 Key 未继续轮换")
			}
			k.event(ctx, uid, aid, t, "repaired", "白名单不一致，已精确修复并回读")
		}
		// Accepted media tasks must finish under their original key. Repairing a
		// drifted whitelist has already contained external use while this waits.
		var mediaTable bool
		if e = k.db.QueryRow(ctx, `SELECT to_regclass('channel_monitor_media_tasks') IS NOT NULL`).Scan(&mediaTable); e != nil {
			return out, e
		}
		if mediaTable {
			var active bool
			e = k.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_monitor_media_tasks m JOIN channel_monitor_rules r ON r.id=m.rule_id JOIN real_connections c ON c.id=r.connection_id WHERE c.user_id=$1 AND c.workspace_admin_account_id=$2 AND c.upstream_site_id=$3 AND c.upstream_key_id=$4 AND m.status IN('submitting','pending','unknown') AND m.created_at>now()-interval '60 minutes')`, uid, aid, site.ID, r.OldID).Scan(&active)
			if e != nil {
				return out, e
			}
			if active {
				return jobs.Outcome{Pending: true, Delay: 30 * time.Second, Value: map[string]any{"message": "等待已受理的媒体检测结束后轮换"}}, nil
			}
		}
		consumers, e := k.sites.connRepository.ListRealConnections(ctx, uid, aid)
		if e != nil {
			return out, e
		}
		consumerCount := 0
		for _, c := range consumers {
			if c.UpstreamSiteID != site.ID || c.UpstreamKeyID != r.OldID {
				continue
			}
			consumerCount++
			a, e := k.sites.platformService.FetchForwardingAccount(ctx, session, c.AdminAccountID)
			if e != nil || !upstream.SameMonitorKey(site.Platform, a.APIKey, r.OldSecret) {
				// Only cancel before any remote key was created. Transient read
				// failures remain retryable; an explicit changed key is conclusive.
				var reqErr *upstream.RequestError
				missing := errors.As(e, &reqErr) && reqErr.HTTPStatus == 404
				if (e == nil || missing) && r.NewID == "" {
					_, _ = k.db.Exec(ctx, `UPDATE key_security_rotations SET phase='cancelled',old_secret='',new_secret='',updated_at=now() WHERE id=$1`, r.ID)
				}
				return out, fmt.Errorf("原转发账号已删除或凭据变化，未创建新 Key")
			}
		}
		if consumerCount == 0 {
			return out, fmt.Errorf("原绑定已移除，未创建新 Key")
		}
		ips, e := k.policies.DesiredIPs(ctx, uid, aid, site, t.KeyID, t.GroupID)
		if e != nil {
			return out, e
		}
		name, _ := r.Config["name"].(string)
		if r.Naming.PreviousName == "" {
			r.Naming.PreviousName = keyString(record["name"])
		}
		r.Config, e = upstream.SecurityCloneConfig(site.Platform, record, name, ips)
		if e != nil {
			return out, e
		}
		cfg, _ := json.Marshal(r.Config)
		if _, e = k.db.Exec(ctx, `UPDATE key_security_rotations SET config=$2,naming=$3,updated_at=now() WHERE id=$1`, r.ID, cfg, r.Naming); e != nil {
			return out, e
		}
		r.NewID, r.NewSecret, e = k.sites.platformService.CreateSecurityKey(ctx, *site.Session, r.Config, r.NewSecret)
		if e != nil {
			return out, e
		}
		if site.Platform == upstream.PlatformNewAPI && strings.HasPrefix(r.OldSecret, "sk-") && !strings.HasPrefix(r.NewSecret, "sk-") {
			r.NewSecret = "sk-" + r.NewSecret
		}
		if e = k.phase(ctx, r, "created"); e != nil {
			return out, e
		}
		r.Phase = "created"
	}
	if r.Phase == "created" {
		if e = k.policies.CopyWhitelistSwitch(ctx, uid, aid, site.ID, r.OldID, r.NewID); e != nil {
			return out, e
		}
		if e = k.sites.platformService.ConfigureSecurityKey(ctx, *site.Session, r.NewID, r.Config); e != nil {
			return out, e
		}
		if e = k.sites.platformService.VerifySecurityConfig(ctx, *site.Session, r.NewID, r.Config); e != nil {
			return out, e
		}
		exact, e := k.policies.VerifyExact(ctx, uid, aid, site, r.NewID, t.GroupID)
		if e != nil || !exact {
			return out, fmt.Errorf("新 Key 白名单回读不一致，旧 Key 尚未撤销")
		}
		if e = k.phase(ctx, r, "verified"); e != nil {
			return out, e
		}
		r.Phase = "verified"
	}
	if r.Phase == "verified" {
		conns, e := k.sites.connRepository.ListRealConnections(ctx, uid, aid)
		if e != nil {
			return out, e
		}
		count := 0
		for _, c := range conns {
			if c.UpstreamSiteID != site.ID || c.UpstreamKeyID != r.OldID {
				continue
			}
			count++
			if c.AdminAccountID == "" {
				return out, fmt.Errorf("绑定缺少自有转发账号")
			}
			if e = k.sites.platformService.ReplaceForwardingKey(ctx, session, c.AdminAccountID, r.OldSecret, r.NewSecret); e != nil {
				return out, e
			}
		}
		if count == 0 {
			return out, fmt.Errorf("原绑定已变化，未撤销旧 Key")
		}
		tx, e := k.db.Begin(ctx)
		if e != nil {
			return out, e
		}
		defer tx.Rollback(ctx)
		key, fingerprint, e := protectConnectionKey(k.sites.storageCipher, r.NewSecret)
		if e != nil {
			return out, e
		}
		tag, e := tx.Exec(ctx, `UPDATE real_connections SET upstream_key_id=$5,upstream_key=$6,upstream_key_fingerprint=$8
			WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_key_id=$4
			AND coalesce(nullif(upstream_key_fingerprint,''),upstream_key)=$7`, uid, aid, site.ID, r.OldID, r.NewID, key, connectionKeyMatch(k.sites.storageCipher, r.OldSecret), fingerprint)
		if e != nil {
			return out, e
		}
		if tag.RowsAffected() != int64(count) {
			return out, fmt.Errorf("转发绑定已变化，保留旧 Key 待核对")
		}

		_, e = tx.Exec(ctx, `UPDATE key_security_rotations SET phase='switched',updated_at=now() WHERE id=$1`, r.ID)
		if e != nil {
			return out, e
		}
		if e = tx.Commit(ctx); e != nil {
			return out, e
		}
		r.Phase = "switched"
	}
	if r.Phase == "switched" {
		// Recheck every current consumer before revocation, including after restart.
		manager := k.sites.keyManagement
		if manager != nil {
			unlock, err := manager.lockOperation(ctx, "key-security|"+site.ID+"|"+r.NewID)
			if err != nil {
				return jobs.Outcome{Pending: true, Delay: 5 * time.Second}, nil
			}
			defer unlock()
		}
		conns, e := k.boundConsumers(ctx, uid, aid, site.ID, []string{r.NewID})
		if e != nil {
			return out, e
		}
		var account upstream.Sub2APIAdminAccountStatus
		for _, c := range conns {
			account, e = k.sites.platformService.FetchForwardingAccount(ctx, session, c.AdminAccountID)
			if e != nil || !upstream.SameMonitorKey(site.Platform, account.APIKey, r.NewSecret) {
				return out, fmt.Errorf("新 Key 渠道回读不一致，旧 Key 尚未撤销")
			}
		}
		if len(conns) == 0 {
			return out, fmt.Errorf("新 Key 绑定不存在，旧 Key 尚未撤销")
		}
		if manager != nil {
			defer manager.invalidate(&site)
			if e = manager.prepareRotationNaming(ctx, uid, aid, &site, session, conns, account, &r); e != nil {
				return out, e
			}
			if e = manager.renameRotatedAccount(ctx, session, conns, account, r.Naming); e != nil {
				return out, e
			}
		}
		if e = k.sites.platformService.DeleteSecurityKey(ctx, *site.Session, r.OldID); e != nil {
			return out, e
		}
		if manager != nil {
			if e = manager.finishManagedRotation(ctx, uid, aid, &site, conns, r); e != nil {
				return out, e
			}
		}
		if e = k.phase(ctx, r, "done"); e != nil {
			return out, e
		}
		// Old secrets are no longer needed for recovery after the cutover is final.
		_, e = k.db.Exec(ctx, `UPDATE key_security_rotations SET old_secret='',new_secret='' WHERE id=$1`, r.ID)
		if e != nil {
			return out, e
		}
	}
	k.event(ctx, uid, aid, t, "done", rotationMessage(r))
	return rotationResult(r), nil
}

// Inventory is read only to reconcile bindings. Only current bound consumers
// become security targets; unrelated upstream Keys must never be modified.
func (k *KeySecurity) scanSite(ctx context.Context, uid, aid, id string, raw json.RawMessage) (jobs.Outcome, error) {
	if _, e := k.db.Exec(ctx, `INSERT INTO key_security_site_refresh(user_id,admin_account_id,site_id,queued_at) VALUES($1,$2,$3,now()) ON CONFLICT(user_id,admin_account_id,site_id) DO UPDATE SET queued_at=now()`, uid, aid, id); e != nil {
		return jobs.Outcome{}, e
	}
	site, e := k.ownedSite(ctx, uid, aid, id)
	if e != nil {
		return jobs.Outcome{}, e
	}
	records, e := k.sites.platformService.SecurityKeys(ctx, *site.Session)
	if e != nil {
		if refresher, ok := k.sites.upstreamLookup.(interface {
			RefreshSite(context.Context, string) (*upstream.Site, error)
		}); ok {
			if fresh, refreshErr := refresher.RefreshSite(ctx, site.ID); refreshErr == nil && fresh != nil && fresh.UserID == uid && fresh.AdminAccountID == aid && fresh.Session != nil {
				site = fresh
				records, e = k.sites.platformService.SecurityKeys(ctx, *site.Session)
			}
		}
	}
	if e != nil {
		return jobs.Outcome{}, e
	}
	if _, e = k.pruneDeletedBindings(ctx, uid, aid, site.ID); e != nil {
		return jobs.Outcome{}, e
	}
	if e = k.pruneMissingUpstreamBindings(ctx, uid, aid, *site, records); e != nil {
		return jobs.Outcome{}, e
	}
	boundItems, e := k.targets(ctx, uid, aid, "audit")
	if e != nil {
		return jobs.Outcome{}, e
	}
	items := []jobs.Item{}
	for _, item := range boundItems {
		var t securityTarget
		if json.Unmarshal(item.Payload, &t) == nil && t.SiteID == id {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return jobs.Outcome{Value: map[string]any{"message": "此上游没有绑定自有 Sub2API 的 Key"}}, nil
	}
	j, e := k.jobs.Submit(ctx, uid, aid, "key_security", items)
	if e != nil {
		return jobs.Outcome{}, e
	}
	return jobs.Outcome{Value: map[string]any{"message": fmt.Sprintf("已选择 %d 个自有站绑定 Key", len(items)), "childJobId": j.ID}}, nil
}
func (k *KeySecurity) submitScan(ctx context.Context, uid, aid string, intervalMinutes int) (jobs.Job, error) {
	rows, e := k.db.Query(ctx, `SELECT s.id FROM upstream_sites s WHERE s.user_id=$1 AND s.admin_account_id=$2 AND s.platform IN('sub2api','newapi')
		AND EXISTS(SELECT 1 FROM real_connections c WHERE c.user_id=s.user_id AND c.workspace_admin_account_id=s.admin_account_id AND c.upstream_site_id=s.id AND c.upstream_key_id<>'' AND c.admin_account_id<>'' AND c.upstream_key<>'')
		AND ($3::int=0 OR NOT EXISTS(SELECT 1 FROM key_security_site_refresh f WHERE f.user_id=s.user_id AND f.admin_account_id=s.admin_account_id AND f.site_id=s.id AND f.queued_at>now()-make_interval(mins => $3)))`, uid, aid, intervalMinutes)
	if e != nil {
		return jobs.Job{}, e
	}
	defer rows.Close()
	items := []jobs.Item{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return jobs.Job{}, e
		}
		items = append(items, jobs.Item{ID: id})
	}
	if e = rows.Err(); e != nil {
		return jobs.Job{}, e
	}
	if len(items) == 0 {
		return jobs.Job{}, fmt.Errorf("当前工作区没有可同步的上游")
	}
	return k.jobs.Submit(ctx, uid, aid, "key_security_scan", items)
}

func (k *KeySecurity) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/my-sites/key-security/site-whitelist", k.syncSiteWhitelist)
	mux.HandleFunc("GET /api/my-sites/real-connections/{id}/key-whitelist", k.whitelistSwitch)
	mux.HandleFunc("PUT /api/my-sites/real-connections/{id}/key-whitelist", k.whitelistSwitch)

	scope := func(r *http.Request) (string, string, error) {
		uid, _ := authctx.UserID(r.Context())
		if uid == "" {
			return "", "", fmt.Errorf("未登录")
		}
		aid, e := k.sites.currentAdminAccountID(r.Context(), uid)
		return uid, aid, e
	}
	mux.HandleFunc("GET /api/my-sites/key-security", func(w http.ResponseWriter, r *http.Request) {
		uid, aid, e := scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		p, e := k.policy(r.Context(), uid, aid)
		if e != nil {
			httpjson.WriteError(w, 500, "无法读取安全策略")
			return
		}
		rows, e := k.db.Query(r.Context(), `SELECT site_id,key_id,action,status,message,created_at FROM key_security_events WHERE user_id=$1 AND admin_account_id=$2 ORDER BY created_at DESC,id DESC LIMIT 50`, uid, aid)
		if e != nil {
			httpjson.WriteError(w, 500, "无法读取安全记录")
			return
		}
		defer rows.Close()
		events := []map[string]any{}
		for rows.Next() {
			var site, key, action, status, message string
			var at time.Time
			if e = rows.Scan(&site, &key, &action, &status, &message, &at); e != nil {
				httpjson.WriteError(w, 500, "无法读取安全记录")
				return
			}
			events = append(events, map[string]any{"siteId": site, "keyId": key, "action": action, "status": status, "message": message, "createdAt": at})
		}
		if rows.Err() != nil {
			httpjson.WriteError(w, 500, "无法读取安全记录")
			return
		}
		httpjson.Write(w, 200, map[string]any{"policy": p, "events": events})
	})
	mux.HandleFunc("PUT /api/my-sites/key-security", func(w http.ResponseWriter, r *http.Request) {
		if k.cipher == nil {
			httpjson.WriteError(w, 503, "请先配置凭据加密密钥")
			return
		}
		uid, aid, e := scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		session, e := k.sites.RequireSession(r.Context(), uid, aid)
		if e != nil || session.Platform != upstream.PlatformSub2API {
			httpjson.WriteError(w, 403, "请先登录自有 Sub2API 管理员")
			return
		}
		var p KeySecurityPolicy
		if httpjson.Decode(r, &p) != nil {
			httpjson.WriteError(w, 400, "无效策略")
			return
		}
		if p.AuditIntervalMinutes == 0 {
			current, err := k.policy(r.Context(), uid, aid)
			if err != nil {
				httpjson.WriteError(w, 500, "无法读取安全策略")
				return
			}
			p.AuditIntervalMinutes = current.AuditIntervalMinutes
		}
		p, e = k.savePolicy(r.Context(), uid, aid, p)
		if e != nil {
			httpjson.WriteError(w, 400, e.Error())
			return
		}
		httpjson.Write(w, 200, p)
	})
	mux.HandleFunc("POST /api/my-sites/key-security/{action}", func(w http.ResponseWriter, r *http.Request) {
		if k.cipher == nil {
			httpjson.WriteError(w, 503, "请先配置凭据加密密钥")
			return
		}
		uid, aid, e := scope(r)
		if e != nil {
			httpjson.WriteError(w, 401, "未登录")
			return
		}
		// Require an actual self-hosted administrator session before security writes.
		session, e := k.sites.RequireSession(r.Context(), uid, aid)
		if e != nil || session.Platform != upstream.PlatformSub2API {
			httpjson.WriteError(w, 403, "请先登录自有 Sub2API 管理员")
			return
		}
		var req struct {
			ConnectionIDs []string `json:"connectionIds"`
			RuleIDs       []string `json:"ruleIds"`
			Preview       bool     `json:"preview"`
			Version       string   `json:"version"`
		}
		if r.ContentLength != 0 {
			if httpjson.Decode(r, &req) != nil {
				httpjson.WriteError(w, 400, "无效目标")
				return
			}
		}
		action := r.PathValue("action")
		if len(req.ConnectionIDs) > 10000 || len(req.RuleIDs) > 10000 {
			httpjson.WriteError(w, 400, "目标过多")
			return
		}
		if req.RuleIDs != nil {
			if (action != "rotate" && action != "whitelist") || req.ConnectionIDs != nil {
				httpjson.WriteError(w, 400, "无效操作目标")
				return
			}
			e = k.db.QueryRow(r.Context(), `SELECT coalesce(array_agg(DISTINCT connection_id),'{}') FROM channel_monitor_rules
				WHERE user_id=$1 AND admin_account_id=$2 AND id=ANY($3::text[])`, uid, aid, req.RuleIDs).Scan(&req.ConnectionIDs)
			if e != nil {
				httpjson.WriteError(w, 500, "无法读取监控绑定")
				return
			}
		}
		if req.ConnectionIDs != nil && len(req.ConnectionIDs) == 0 && !req.Preview {
			httpjson.WriteError(w, 400, "请选择操作目标")
			return
		}
		if req.RuleIDs == nil && len(req.ConnectionIDs) > 0 {
			var count int
			e = k.db.QueryRow(r.Context(), `SELECT count(*) FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND id=ANY($3::text[])`, uid, aid, req.ConnectionIDs).Scan(&count)
			if e != nil || count != len(req.ConnectionIDs) {
				httpjson.WriteError(w, 404, "目标不属于当前工作区或有重复")
				return
			}
		}
		if req.Preview {
			if action != "rotate" || req.ConnectionIDs == nil {
				httpjson.WriteError(w, 400, "请选择轮换目标")
				return
			}
			total := len(req.ConnectionIDs)
			if req.RuleIDs != nil {
				total = len(req.RuleIDs)
			}
			plan, err := k.rotationPreview(r.Context(), uid, aid, req.ConnectionIDs, total)
			if err != nil {
				httpjson.WriteError(w, 500, "无法预览轮换")
				return
			}
			httpjson.Write(w, 200, plan)
			return
		}
		lock, e := k.db.Acquire(r.Context())
		if e != nil {
			httpjson.WriteError(w, 503, "安全任务服务忙")
			return
		}
		defer lock.Release()
		var locked bool
		e = lock.QueryRow(r.Context(), `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "security-schedule|"+uid+"|"+aid).Scan(&locked)
		if e != nil || !locked {
			httpjson.WriteError(w, 409, "安全任务正在提交，请稍后重试")
			return
		}
		defer lock.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "security-schedule|"+uid+"|"+aid)
		var active bool
		e = k.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM operation_jobs j JOIN operation_items i ON i.job_id=j.id WHERE j.user_id=$1 AND j.admin_account_id=$2 AND j.kind IN('key_security','key_security_scan','key_security_bindings') AND i.status IN('queued','running','waiting'))`, uid, aid).Scan(&active)
		if e != nil || active {
			httpjson.WriteError(w, 409, "已有 Key 安全任务正在执行，请等待完成")
			return
		}
		var job jobs.Job
		switch action {
		case "discover":
			job, e = k.jobs.Submit(r.Context(), uid, aid, "key_security_bindings", []jobs.Item{{ID: "workspace"}})
		case "sync-all":
			job, e = k.submitScan(r.Context(), uid, aid, 0)
		case "sync":
			if len(req.ConnectionIDs) > 0 {
				var items []jobs.Item
				items, e = k.targets(r.Context(), uid, aid, "audit", req.ConnectionIDs)
				if e == nil {
					job, e = k.jobs.Submit(r.Context(), uid, aid, "key_security", items)
				}
			} else {
				job, e = k.submit(r.Context(), uid, aid, "audit")
			}
		case "whitelist":
			if len(req.ConnectionIDs) == 0 {
				httpjson.WriteError(w, 400, "请选择白名单同步目标")
				return
			}
			var items []jobs.Item
			items, e = k.targets(r.Context(), uid, aid, "whitelist", req.ConnectionIDs)
			if e == nil && len(items) == 0 {
				e = fmt.Errorf("所选记录没有可同步的绑定 Key")
			}
			if e == nil {
				job, e = k.jobs.Submit(r.Context(), uid, aid, "key_security", items)
			}
		case "rotate":
			if req.ConnectionIDs != nil {
				var items []jobs.Item
				items, e = k.targets(r.Context(), uid, aid, "rotate", req.ConnectionIDs)
				if e == nil && req.Version != "" && req.Version != rotationPlanVersion(items) {
					e = fmt.Errorf("轮换目标已变化，请重新预览")
				}
				if e == nil && len(items) == 0 {
					e = fmt.Errorf("所选记录没有可轮换的绑定 Key")
				}
				if e == nil {
					job, e = k.jobs.Submit(r.Context(), uid, aid, "key_security", items)
				}
			} else {
				job, e = k.submit(r.Context(), uid, aid, "rotate")
			}
		default:
			httpjson.WriteError(w, 400, "无效操作")
			return
		}
		if e != nil {
			httpjson.WriteError(w, 400, e.Error())
			return
		}
		httpjson.Write(w, 202, job)
	})
}

func (k *KeySecurity) Start(ctx context.Context) {
	if k.cipher == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			k.tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (k *KeySecurity) tick(ctx context.Context) {
	// New workspaces inherit daily 04:00 and periodic exact whitelist enforcement.
	rows, e := k.db.Query(ctx, `SELECT DISTINCT user_id,workspace_admin_account_id FROM real_connections`)
	if e != nil {
		return
	}
	type scope struct{ uid, aid string }
	scopes := []scope{}
	for rows.Next() {
		var s scope
		if rows.Scan(&s.uid, &s.aid) == nil {
			scopes = append(scopes, s)
		}
	}
	rows.Close()
	for _, s := range scopes {
		run, cancel := context.WithTimeout(ctx, 30*time.Second)
		k.tickScope(run, s.uid, s.aid)
		cancel()
	}
}
func (k *KeySecurity) tickScope(ctx context.Context, uid, aid string) {
	p, e := k.policy(ctx, uid, aid)
	if e != nil {
		return
	}
	if p.Version == 0 {
		p, e = k.savePolicy(ctx, uid, aid, p)
		if e != nil {
			return
		}
	}
	// Lock submission together with its schedule update. This also handles two
	// application replicas and avoids duplicate schedules after restarts.
	conn, unlock := k.lockAuditSchedule(ctx, uid, aid)
	if conn == nil {
		return
	}
	defer unlock()
	p, e = k.policy(ctx, uid, aid)
	if e != nil {
		return
	}
	now := time.Now()
	// Do not add work while any security batch for this workspace is unfinished.
	var active bool
	e = k.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operation_jobs j JOIN operation_items i ON i.job_id=j.id WHERE j.user_id=$1 AND j.admin_account_id=$2 AND j.kind IN('key_security','key_security_scan','key_security_bindings') AND i.status IN('queued','running','waiting'))`, uid, aid).Scan(&active)
	if e != nil || active {
		return
	}
	// Recover incomplete cutovers first, including revocation failures. The same
	// old-key operation is resumed, never a second rotation of its replacement.
	rs, e := k.db.Query(ctx, `SELECT site_id,old_key_id,config,group_id FROM key_security_rotations WHERE user_id=$1 AND admin_account_id=$2 AND phase NOT IN('done','cancelled') AND updated_at<now()-interval '5 minutes'`, uid, aid)
	if e != nil {
		return
	}
	items := []jobs.Item{}
	for rs.Next() {
		var t securityTarget
		var raw []byte
		t.Action = "rotate"
		if rs.Scan(&t.SiteID, &t.KeyID, &raw, &t.GroupID) != nil {
			continue
		}
		var cfg map[string]any
		_ = json.Unmarshal(raw, &cfg)
		site, e := k.ownedSite(ctx, uid, aid, t.SiteID)
		if e != nil {
			continue
		}
		if t.GroupID == "" {
			t.GroupID = upstream.SecurityKeyGroup(site.Platform, cfg)
		}
		b, _ := json.Marshal(t)
		items = append(items, jobs.Item{ID: t.SiteID + "/" + t.KeyID, Payload: b})
	}
	rs.Close()
	if len(items) > 0 {
		_, _ = k.jobs.Submit(ctx, uid, aid, "key_security", items)
	}
	if p.Enabled && !p.NextRotation.After(now) {
		var discovered bool
		if e = k.db.QueryRow(ctx, `SELECT coalesce(last_discovery >= next_rotation,false) FROM key_security_policies WHERE user_id=$1 AND admin_account_id=$2`, uid, aid).Scan(&discovered); e != nil {
			return
		}
		if !discovered {
			_, _ = k.jobs.Submit(ctx, uid, aid, "key_security_bindings", []jobs.Item{{ID: "workspace"}})
			return
		}
		if _, e = k.submit(ctx, uid, aid, "rotate"); e == nil {
			next := p.NextRotation
			for !next.After(now) {
				next = next.Add(time.Duration(p.IntervalHours) * time.Hour)
			}
			_, _ = k.db.Exec(ctx, `UPDATE key_security_policies SET next_rotation=$3 WHERE user_id=$1 AND admin_account_id=$2`, uid, aid, next)
		}
		return
	}
	if p.AuditEnabled && (p.LastAudit == nil || now.Sub(*p.LastAudit) >= time.Duration(p.AuditIntervalMinutes)*time.Minute) {
		if _, e = k.submitScan(ctx, uid, aid, p.AuditIntervalMinutes); e == nil {
			_, _ = k.db.Exec(ctx, `UPDATE key_security_policies SET last_audit=now() WHERE user_id=$1 AND admin_account_id=$2`, uid, aid)
		}
	}
}

func (k *KeySecurity) lockAuditSchedule(ctx context.Context, uid, aid string) (*pgxpool.Conn, func()) {
	conn, err := k.db.Acquire(ctx)
	if err != nil {
		return nil, nil
	}
	key := "security-schedule|" + uid + "|" + aid
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&locked); err != nil || !locked {
		conn.Release()
		return nil, nil
	}
	return conn, func() {
		defer conn.Release()
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key)
	}
}
