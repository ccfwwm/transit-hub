package my_sites

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
)

// Only an authenticated 404 from the exact account can authorize removal.
// An absent list entry, a masked credential, 403, 429 or a timeout cannot.
func (k *KeySecurity) pruneDeletedBindings(ctx context.Context, uid, aid string, siteIDs ...string) (int, error) {
	session, e := k.sites.RequireSession(ctx, uid, aid)
	if e != nil {
		return 0, fmt.Errorf("无法验证自有站管理员会话，未清理任何绑定")
	}
	if session.Platform != upstream.PlatformSub2API {
		return 0, nil
	}
	conns, e := k.sites.connRepository.ListRealConnections(ctx, uid, aid)
	if e != nil {
		return 0, e
	}
	removed := 0
	for _, c := range conns {
		if len(siteIDs) > 0 && c.UpstreamSiteID != siteIDs[0] {
			continue
		}
		var sitesTable bool
		if e = k.db.QueryRow(ctx, `SELECT to_regclass('upstream_sites') IS NOT NULL`).Scan(&sitesTable); e != nil {
			return removed, e
		}
		if sitesTable {
			var exists bool
			e = k.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM upstream_sites WHERE id=$1 AND user_id=$2 AND admin_account_id=$3)`, c.UpstreamSiteID, uid, aid).Scan(&exists)
			if e != nil {
				return removed, e
			}
			if !exists {
				if e = k.removeDeletedBinding(ctx, c, "上游站点已删除，已清理失效监控绑定"); e != nil {
					return removed, e
				}
				removed++
				continue
			}
		}
		if c.AdminAccountID == "" {
			continue
		}
		_, e = k.sites.platformService.FetchForwardingAccountDetail(ctx, session, c.AdminAccountID)
		var req *upstream.RequestError
		if !errors.As(e, &req) || req.HTTPStatus != 404 {
			continue
		}
		if e = k.removeDeletedBinding(ctx, c); e != nil {
			return removed, e
		}
		removed++
	}
	return removed, nil
}

func (k *KeySecurity) removeDeletedBinding(ctx context.Context, c RealConnection, reasons ...string) error {
	tx, e := k.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Refuse to race a binding reattachment or an accepted media operation.
	var account string
	e = tx.QueryRow(ctx, `SELECT admin_account_id FROM real_connections WHERE id=$1 AND user_id=$2 AND workspace_admin_account_id=$3 FOR UPDATE`, c.ID, c.UserID, c.WorkspaceAdminAccountID).Scan(&account)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if account != c.AdminAccountID {
		return fmt.Errorf("绑定已修改，未清理")
	}
	var mediaExists bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('channel_monitor_media_tasks') IS NOT NULL`).Scan(&mediaExists); e != nil {
		return e
	}
	if mediaExists {
		var active bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_monitor_media_tasks m JOIN channel_monitor_rules r ON r.id=m.rule_id WHERE r.connection_id=$1 AND r.user_id=$2 AND r.admin_account_id=$3 AND m.status IN('submitting','pending','unknown') AND m.created_at>now()-interval '60 minutes')`, c.ID, c.UserID, c.WorkspaceAdminAccountID).Scan(&active)
		if e != nil {
			return e
		}
		if active {
			return fmt.Errorf("已删除账号仍有受理媒体任务，等待任务结束后清理")
		}
	}
	ids := []string{c.ID}
	var rulesExist bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('channel_monitor_rules') IS NOT NULL`).Scan(&rulesExist); e != nil {
		return e
	}
	if rulesExist {
		rows, e := tx.Query(ctx, `SELECT id FROM channel_monitor_rules WHERE connection_id=$1 AND user_id=$2 AND admin_account_id=$3`, c.ID, c.UserID, c.WorkspaceAdminAccountID)
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	for _, table := range []string{"channel_monitor_group_states", "channel_monitor_media_tasks", "channel_monitor_model_catalog", "channel_monitor_manual_checks", "channel_monitor_views", "channel_monitor_results"} {
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); e != nil {
			return e
		}
		if exists {
			if _, e = tx.Exec(ctx, `DELETE FROM `+table+` WHERE rule_id=ANY($1::text[])`, ids); e != nil {
				return e
			}
		}
	}
	if rulesExist {
		if _, e = tx.Exec(ctx, `DELETE FROM channel_monitor_rules WHERE user_id=$1 AND admin_account_id=$2 AND connection_id=$3`, c.UserID, c.WorkspaceAdminAccountID, c.ID); e != nil {
			return e
		}
	}
	var snapshots bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('channel_monitor_snapshots') IS NOT NULL`).Scan(&snapshots); e != nil {
		return e
	}
	if snapshots {
		if _, e = tx.Exec(ctx, `DELETE FROM channel_monitor_snapshots WHERE user_id=$1 AND admin_account_id=$2`, c.UserID, c.WorkspaceAdminAccountID); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `DELETE FROM real_connections WHERE id=$1 AND user_id=$2 AND workspace_admin_account_id=$3 AND admin_account_id=$4`, c.ID, c.UserID, c.WorkspaceAdminAccountID, c.AdminAccountID); e != nil {
		return e
	}
	// No successor exists for a prepared rotation. Clear its temporary secrets.
	if _, e = tx.Exec(ctx, `UPDATE key_security_rotations SET phase='cancelled',old_secret='',new_secret='',updated_at=now() WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND old_key_id=$4 AND phase='prepared' AND new_key_id='' AND NOT EXISTS(SELECT 1 FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_key_id=$4)`, c.UserID, c.WorkspaceAdminAccountID, c.UpstreamSiteID, c.UpstreamKeyID); e != nil {
		return e
	}
	// Remove a mapping only after its last valid connection disappears.
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT mappings FROM my_site_states WHERE user_id=$1 AND admin_account_id=$2 FOR UPDATE`, c.UserID, c.WorkspaceAdminAccountID).Scan(&raw)
	if e == nil {
		var mappings []GroupMapping
		if e = json.Unmarshal(raw, &mappings); e != nil {
			return e
		}
		var keep bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_group_name=$4)`, c.UserID, c.WorkspaceAdminAccountID, c.UpstreamSiteID, c.UpstreamGroupName).Scan(&keep)
		if e != nil {
			return e
		}
		if !keep {
			for i := range mappings {
				refs := []UpstreamGroupRef{}
				for _, v := range mappings[i].UpstreamTargets {
					if v.SiteID != c.UpstreamSiteID || v.GroupName != c.UpstreamGroupName {
						refs = append(refs, v)
					}
				}
				mappings[i].UpstreamTargets = refs
			}
			raw, _ = json.Marshal(mappings)
			if _, e = tx.Exec(ctx, `UPDATE my_site_states SET mappings=$3,updated_at=now() WHERE user_id=$1 AND admin_account_id=$2`, c.UserID, c.WorkspaceAdminAccountID, raw); e != nil {
				return e
			}
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	k.sites.membershipChanged(ctx, c, c.OwnGroupIDs)
	reason := fmt.Sprintf("自有账号 %s 已删除，已清理对应监控绑定", c.AdminAccountID)
	if len(reasons) > 0 {
		reason = reasons[0]
	}
	k.event(ctx, c.UserID, c.WorkspaceAdminAccountID, securityTarget{SiteID: c.UpstreamSiteID, KeyID: c.UpstreamKeyID, Action: "cleanup"}, "done", reason)
	return nil
}

func (k *KeySecurity) pruneMissingUpstreamBindings(ctx context.Context, uid, aid string, site upstream.Site, records []map[string]any) error {
	available := map[string]bool{}
	for _, record := range records {
		available[upstream.SecurityKeyID(record)] = true
	}
	conns, e := k.sites.connRepository.ListRealConnections(ctx, uid, aid)
	if e != nil {
		return e
	}
	for _, c := range conns {
		if c.UpstreamSiteID != site.ID || available[c.UpstreamKeyID] {
			continue
		}
		// A complete list plus an authenticated 404 is required. New API forks
		// returning success:false/null instead of a 404 remain unresolved.
		_, _, e = k.sites.platformService.ReadSecurityKey(ctx, *site.Session, c.UpstreamKeyID)
		var req *upstream.RequestError
		if !(errors.As(e, &req) && req.HTTPStatus == 404) && !errors.Is(e, upstream.ErrSecurityKeyMissing) {
			continue
		}
		var recovering bool
		e = k.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM key_security_rotations WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND old_key_id=$4 AND new_key_id<>'' AND phase NOT IN('done','cancelled'))`, uid, aid, site.ID, c.UpstreamKeyID).Scan(&recovering)
		if e != nil {
			return e
		}
		if recovering {
			continue
		}
		if e = k.removeDeletedBinding(ctx, c, fmt.Sprintf("上游 Key %s 已删除，已清理账号 %s 的失效监控绑定", c.UpstreamKeyID, c.AdminAccountID)); e != nil {
			return e
		}
	}
	return nil
}

// Share the workspace audit interval with the scheduled scan. Only sites that
// have a managed binding can queue work from a data refresh.
func (k *KeySecurity) QueueSiteAudit(ctx context.Context, uid, aid, site string) {
	if k.cipher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, unlock := k.lockAuditSchedule(ctx, uid, aid)
	if conn == nil {
		return
	}
	defer unlock()
	var due bool
	err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM key_security_policies p WHERE p.user_id=$1 AND p.admin_account_id=$2 AND p.audit_enabled
		AND EXISTS(SELECT 1 FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_key_id<>'' AND admin_account_id<>'' AND upstream_key<>'')
		AND NOT EXISTS(SELECT 1 FROM key_security_site_refresh f WHERE f.user_id=$1 AND f.admin_account_id=$2 AND f.site_id=$3 AND f.queued_at>now()-make_interval(mins => p.audit_interval_minutes))
		AND NOT EXISTS(SELECT 1 FROM operation_jobs j JOIN operation_items i ON i.job_id=j.id
			WHERE j.user_id=$1 AND j.admin_account_id=$2 AND ((j.kind='key_security_scan' AND i.id=$3) OR (j.kind='key_security' AND split_part(i.id,'/',1)=$3)) AND i.status IN('queued','running','waiting')))`, uid, aid, site).Scan(&due)
	if err != nil || !due {
		return
	}
	_, _ = k.jobs.Submit(ctx, uid, aid, "key_security_scan", []jobs.Item{{ID: site}})
}
