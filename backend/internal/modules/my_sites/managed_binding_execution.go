package my_sites

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
)

func (m *KeyManagement) runBinding(ctx context.Context, uid, aid, id string, raw json.RawMessage) (jobs.Outcome, error) {
	var task managedBindingJob
	if json.Unmarshal(raw, &task) != nil || id != task.Plan.ID {
		return jobs.Outcome{}, fmt.Errorf("无效绑定任务")
	}
	unlock, err := m.lockOperation(ctx, "managed-binding|"+uid+"|"+aid+"|"+id)
	if err != nil {
		return jobs.Outcome{Pending: true, Delay: 5 * time.Second}, nil
	}
	defer unlock()
	conn, err := m.sites.connRepository.GetRealConnection(ctx, id, uid, aid)
	if err != nil || conn == nil {
		return jobs.Outcome{}, fmt.Errorf("找不到对接绑定")
	}
	if conn.UpstreamKeyID != task.KeyID || conn.AdminAccountID != task.AccountID {
		return jobs.Outcome{}, fmt.Errorf("账号或 Key 已变化，请重新预览")
	}
	unlockKey, err := m.lockOperation(ctx, "key-security|"+conn.UpstreamSiteID+"|"+conn.UpstreamKeyID)
	if err != nil {
		return jobs.Outcome{Pending: true, Delay: 5 * time.Second}, nil
	}
	defer unlockKey()
	site, err := m.owned(ctx, uid, aid, conn.UpstreamSiteID)
	if err != nil {
		return jobs.Outcome{}, err
	}
	session, err := m.sites.RequireSession(ctx, uid, aid)
	if err != nil || session.Platform != upstream.PlatformSub2API {
		return jobs.Outcome{}, fmt.Errorf("自有 Sub2API 会话不可用")
	}
	defer m.invalidate(site)
	result, err := m.executeBinding(ctx, *conn, site, session, task)
	return jobs.Outcome{Value: result}, err
}

func (m *KeyManagement) executeBinding(ctx context.Context, conn RealConnection, site *upstream.Site, session upstream.Session, task managedBindingJob) (map[string]any, error) {
	out := map[string]any{"connectionId": conn.ID}
	uid, aid := conn.UserID, conn.WorkspaceAdminAccountID
	oldRecord, _, err := m.platform.ReadSecurityKey(ctx, *site.Session, conn.UpstreamKeyID)
	if err != nil {
		return out, err
	}
	oldSecret, err := m.platform.KeySecret(ctx, *site.Session, conn.UpstreamKeyID, oldRecord)
	if err != nil || !upstream.SameMonitorKey(site.Platform, oldSecret, conn.UpstreamKey) || !upstream.SecurityKeyGroupMatches(site.Platform, oldRecord, conn.UpstreamGroupID) {
		return out, fmt.Errorf("上游 Key 或分组与绑定不一致")
	}
	account, err := m.platform.FetchForwardingAccount(ctx, session, conn.AdminAccountID)
	if err != nil || !upstream.SameMonitorKey(site.Platform, account.APIKey, oldSecret) {
		return out, fmt.Errorf("自有账号凭据与绑定不一致")
	}
	var retained bindingTarget
	groupIDs := []string{}
	for _, target := range task.Plan.Targets {
		groupIDs = append(groupIDs, target.GroupID)
		if target.GroupID == task.Selection.Retain {
			retained = target
		}
	}
	if retained.Name == "" {
		return out, fmt.Errorf("保留分组名称缺失")
	}

	if !slices.Equal(bindingGroupIDs(conn.OwnGroupIDs), bindingGroupIDs(groupIDs)) {
		if len(conn.OwnGroupIDs) == 1 && conn.OwnGroupIDs[0] == retained.GroupID && conn.AdminAccountName == retained.Name {
			return out, nil
		}
		return out, fmt.Errorf("本地分组已变化，请重新预览")
	}
	if !slices.Equal(bindingGroupIDs(account.GroupIDs), bindingGroupIDs(groupIDs)) && !slices.Equal(account.GroupIDs, []string{retained.GroupID}) {
		return out, fmt.Errorf("自有账号分组已变化，请重新预览")
	}
	if err = m.alias(ctx, uid, aid, site.ID, conn.UpstreamKeyID, keyString(oldRecord["name"])); err != nil {
		return out, err
	}
	oldKeys, err := m.platform.SecurityKeys(ctx, *site.Session)
	if err != nil {
		return out, err
	}
	oldAccounts, err := m.platform.ListSub2APIAdminAccounts(session)
	if err != nil {
		return out, err
	}
	if nameCollision(oldKeys, oldAccounts, retained.Name, conn.UpstreamKeyID, conn.AdminAccountID) {
		return out, fmt.Errorf("两端存在同名对象，请刷新预览后重试")
	}
	if len(groupIDs) == 1 {
		err = m.renameBinding(ctx, conn, site, session, retained)
		if err == nil {
			out["name"] = retained.Name
		}
		return out, err
	}
	return m.splitBinding(ctx, conn, site, session, task, retained, oldRecord, oldKeys, oldAccounts)
}

func nameCollision(keys []map[string]any, accounts []upstream.Sub2APIAdminAccountStatus, name, keyID, accountID string) bool {
	for _, key := range keys {
		if upstream.SecurityKeyID(key) != keyID && strings.EqualFold(keyString(key["name"]), name) {
			return true
		}
	}
	for _, account := range accounts {
		if account.ID != accountID && strings.EqualFold(account.Name, name) {
			return true
		}
	}
	return false
}

func (m *KeyManagement) renameBinding(ctx context.Context, conn RealConnection, site *upstream.Site, session upstream.Session, target bindingTarget) error {
	if err := m.platform.RenameManagedKey(ctx, *site.Session, conn.UpstreamKeyID, target.Name); err != nil {
		return err
	}
	if err := m.platform.UpdateForwardingAccountIdentity(ctx, session, conn.AdminAccountID, target.Name, nil); err != nil {
		return fmt.Errorf("上游已改名，自有账号名称待同步: %w", err)
	}
	detail, err := m.platform.FetchForwardingAccountDetail(ctx, session, conn.AdminAccountID)
	if err != nil || detail.Name != target.Name {
		return fmt.Errorf("自有账号名称回读不一致")
	}
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE real_connections SET admin_account_name=$4 WHERE id=$1 AND user_id=$2 AND workspace_admin_account_id=$3`, conn.ID, conn.UserID, conn.WorkspaceAdminAccountID, target.Name); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE managed_binding_names SET applied_name=$5 WHERE connection_id=$1 AND user_id=$2 AND workspace_id=$3 AND own_group_id=$4`, conn.ID, conn.UserID, conn.WorkspaceAdminAccountID, target.GroupID, target.Name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (m *KeyManagement) splitBinding(ctx context.Context, conn RealConnection, site *upstream.Site, session upstream.Session, task managedBindingJob, retained bindingTarget, oldRecord map[string]any, oldKeys []map[string]any, oldAccounts []upstream.Sub2APIAdminAccountStatus) (map[string]any, error) {
	out := map[string]any{"connectionId": conn.ID, "phase": "prepare"}
	uid, aid := conn.UserID, conn.WorkspaceAdminAccountID
	oldLimits := remainingLimits(site.Platform, oldRecord)
	_, _, retainedStep, err := m.progress(ctx, uid, aid, conn.ID, site.ID, retained.GroupID)
	if err != nil {
		return out, err
	}
	if retainedStep == "" {
		if err = validateSplitBudgets(task.Plan.Targets, oldLimits, task.Selection.Budgets); err != nil {
			return out, err
		}
	}
	if len(task.Plan.Targets) > 1 {
		fingerprint := string(encodeBindingPayload(task.Selection))
		var same bool
		err = m.db.QueryRow(ctx, "INSERT INTO managed_binding_progress(user_id,workspace_id,operation_id,own_group_id,site_id,key_id,step,recovery) VALUES($1,$2,$3,'',$4,$5,'split',$6) ON CONFLICT (user_id,workspace_id,operation_id,own_group_id) DO UPDATE SET recovery=managed_binding_progress.recovery RETURNING recovery=$6::jsonb", uid, aid, conn.ID, site.ID, conn.UpstreamKeyID, fingerprint).Scan(&same)
		if err != nil {
			return out, err
		}
		if !same {
			return out, fmt.Errorf("该绑定已有拆分任务，请重试原任务")
		}
	}
	original := upstream.ManagedKeyConfig(site.Platform, oldRecord)
	original["name"] = keyString(oldRecord["name"])
	children := []RealConnection{}
	if m.sites.bindingMonitor != nil {
		items := []RealConnection{conn}
		if err = m.sites.bindingMonitor.PopulatePrimaryModels(ctx, uid, aid, items); err != nil {
			return out, err
		}
		conn = items[0]
	}
	for _, target := range task.Plan.Targets {
		if target.GroupID == retained.GroupID {
			continue
		}
		childKey, childAccount, step, err := m.progress(ctx, uid, aid, conn.ID, site.ID, target.GroupID)
		if err != nil {
			return out, err
		}
		if step == "" {
			if nameCollision(oldKeys, oldAccounts, target.Name, "", "") {
				return out, fmt.Errorf("目标名称 %s 已被占用", target.Name)
			}
			if err = m.saveProgress(ctx, uid, aid, conn.ID, target.GroupID, "", "", "creating"); err != nil {
				return out, err
			}
		}
		cfg := maps.Clone(original)
		cfg["name"] = target.Name
		for field, value := range task.Selection.Budgets[target.GroupID] {
			if _, limited := task.Plan.Limits[field]; limited {
				cfg[field] = value
			}
		}
		var secret string
		childKey, secret, err = m.ensureManagedKey(ctx, site, childKey, target.Name, cfg, step != "ready")
		if err != nil {
			return out, fmt.Errorf("分组 %s 创建 Key %w", target.GroupName, err)
		}
		if step != "ready" {
			if err = m.saveProgress(ctx, uid, aid, conn.ID, target.GroupID, childKey, childAccount, "key_ready"); err != nil {
				return out, err
			}
		}
		childAccount, err = m.ensureManagedAccount(ctx, session, childAccount, target.Name, conn, secret, nil)
		if err != nil {
			return out, fmt.Errorf("分组 %s 准备转发账号 %w", target.GroupName, err)
		}
		if err = m.saveProgress(ctx, uid, aid, conn.ID, target.GroupID, childKey, childAccount, "ready"); err != nil {
			return out, err
		}
		child := conn
		child.ID = conn.ID + "-s" + fmt.Sprint(target.Number)
		child.UpstreamKeyID, child.UpstreamKey = childKey, secret
		child.AdminAccountID, child.AdminAccountName = childAccount, target.Name
		child.OwnGroupIDs = []string{target.GroupID}
		child.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		children = append(children, child)
	}
	out["phase"] = "switch"
	sourceDetails, err := m.platform.FetchForwardingAccountDetail(ctx, session, conn.AdminAccountID)
	if err != nil || sourceDetails.Schedulable == nil {
		return out, fmt.Errorf("无法确认来源账号调度状态")
	}
	if retainedStep != "limits_applied" && len(task.Plan.Limits) > 0 {
		fresh, _, readErr := m.platform.ReadSecurityKey(ctx, *site.Session, conn.UpstreamKeyID)
		if readErr != nil {
			return out, readErr
		}
		cfg := upstream.ManagedKeyConfig(site.Platform, fresh)
		cfg["name"] = keyString(fresh["name"])
		if retainedStep != "limits_pending" {
			if err = validateSplitBudgets(task.Plan.Targets, remainingLimits(site.Platform, fresh), task.Selection.Budgets); err != nil {
				return out, err
			}
		}
		for field, value := range task.Selection.Budgets[retained.GroupID] {
			if _, limited := task.Plan.Limits[field]; !limited {
				continue
			}
			if site.Platform == upstream.PlatformSub2API {
				usedField := strings.Replace(field, "rate_limit_", "usage_", 1)
				if field == "quota" {
					usedField = "quota_used"
				}
				used, _ := fresh[usedField].(float64)
				value += used
			}
			cfg[field] = value
		}
		if retainedStep == "limits_pending" {
			var recovery []byte
			if err = m.db.QueryRow(ctx, "SELECT recovery FROM managed_binding_progress WHERE user_id=$1 AND workspace_id=$2 AND operation_id=$3 AND own_group_id=$4", uid, aid, conn.ID, retained.GroupID).Scan(&recovery); err != nil {
				return out, err
			}
			cfg = map[string]any{}
			if err = json.Unmarshal(recovery, &cfg); err != nil {
				return out, err
			}
		} else {
			if _, err = m.db.Exec(ctx, "UPDATE managed_binding_progress SET key_id=$5,account_id=$6,step='limits_pending',recovery=$7 WHERE user_id=$1 AND workspace_id=$2 AND operation_id=$3 AND own_group_id=$4", uid, aid, conn.ID, retained.GroupID, conn.UpstreamKeyID, conn.AdminAccountID, encodeBindingPayload(cfg)); err != nil {
				return out, err
			}
		}
		if check := m.platform.VerifyManagedKeyConfig(ctx, *site.Session, conn.UpstreamKeyID, cfg); check != nil {
			if err = m.platform.ConfigureSecurityKey(ctx, *site.Session, conn.UpstreamKeyID, cfg); err != nil {
				return out, err
			}
			if err = m.platform.VerifyManagedKeyConfig(ctx, *site.Session, conn.UpstreamKeyID, cfg); err != nil {
				return out, err
			}
		}
		if err = m.saveProgress(ctx, uid, aid, conn.ID, retained.GroupID, conn.UpstreamKeyID, conn.AdminAccountID, "limits_applied"); err != nil {
			return out, err
		}
	}
	// Keep the source routing each group until its verified successor is attached.
	for _, child := range children {
		if err = m.setManagedAccountGroups(ctx, session, child.AdminAccountID, child.OwnGroupIDs); err != nil {
			return out, err
		}
		if err = m.ensureAccountSchedule(ctx, session, child.AdminAccountID, *sourceDetails.Schedulable); err != nil {
			return out, err
		}
	}
	if err = m.setManagedAccountGroups(ctx, session, conn.AdminAccountID, []string{retained.GroupID}); err != nil {
		return out, err
	}
	if err = m.platform.RenameManagedKey(ctx, *site.Session, conn.UpstreamKeyID, retained.Name); err != nil {
		return out, err
	}
	if err = m.platform.UpdateForwardingAccountIdentity(ctx, session, conn.AdminAccountID, retained.Name, nil); err != nil {
		return out, err
	}
	readback, err := m.platform.FetchForwardingAccountDetail(ctx, session, conn.AdminAccountID)
	if err != nil || readback.Name != retained.Name {
		return out, fmt.Errorf("保留账号名称回读不一致")
	}
	out["phase"] = "persist"
	for _, child := range children {
		if m.sites.bindingMonitor != nil {
			if err = m.sites.bindingMonitor.ConfigureBinding(ctx, child, &child.TestModelID, &child.AllowWhenUpstreamRateGteOwn); err != nil {
				return out, err
			}
		}
	}
	if err = m.commitSplit(ctx, conn, retained, children); err != nil {
		return out, err
	}
	m.sites.membershipChanged(ctx, conn, groupIDsForSplit(retained, children))
	out["phase"] = "done"
	out["newBindings"] = len(children)
	return out, nil
}

func groupIDsForSplit(retained bindingTarget, children []RealConnection) []string {
	out := []string{retained.GroupID}
	for _, child := range children {
		out = append(out, child.OwnGroupIDs...)
	}
	return out
}

func (m *KeyManagement) ensureAccountSchedule(ctx context.Context, session upstream.Session, id string, want bool) error {
	detail, err := m.platform.FetchForwardingAccountDetail(ctx, session, id)
	if err != nil {
		return err
	}
	if detail.Schedulable != nil && *detail.Schedulable == want {
		return nil
	}
	if err = m.platform.SetSub2APIAdminAccountSchedulable(session, id, want); err != nil {
		return err
	}
	detail, err = m.platform.FetchForwardingAccountDetail(ctx, session, id)
	if err != nil || detail.Schedulable == nil || *detail.Schedulable != want {
		return fmt.Errorf("自有账号 %s 调度状态回读不一致", id)
	}
	return nil
}

func (m *KeyManagement) setManagedAccountGroups(ctx context.Context, session upstream.Session, id string, want []string) error {
	detail, err := m.platform.FetchForwardingAccountDetail(ctx, session, id)
	if err != nil {
		return err
	}
	if slices.Equal(bindingGroupIDs(detail.GroupIDs), bindingGroupIDs(want)) {
		return nil
	}
	ids, err := stringsToInts(want)
	if err != nil {
		return err
	}
	if err = m.platform.UpdateForwardingAccountIdentity(ctx, session, id, "", ids); err != nil {
		return err
	}
	detail, err = m.platform.FetchForwardingAccountDetail(ctx, session, id)
	if err != nil || !slices.Equal(bindingGroupIDs(detail.GroupIDs), bindingGroupIDs(want)) {
		return fmt.Errorf("自有账号 %s 分组未通过回读", id)
	}
	return nil
}

func (m *KeyManagement) ensureManagedKey(ctx context.Context, site *upstream.Site, id, name string, cfg map[string]any, configure bool) (string, string, error) {
	if id == "" {
		keys, err := m.platform.SecurityKeys(ctx, *site.Session)
		if err != nil {
			return "", "", err
		}
		for _, record := range keys {
			if keyString(record["name"]) == name {
				if id != "" {
					return "", "", fmt.Errorf("存在多个同名上游 Key")
				}
				id = upstream.SecurityKeyID(record)
			}
		}
		if id == "" {
			id, _, err = m.platform.CreateManagedKey(ctx, *site.Session, cfg, "")
			if err != nil {
				return "", "", err
			}
		}
	}
	record, _, err := m.platform.ReadSecurityKey(ctx, *site.Session, id)
	if err != nil || keyString(record["name"]) != name {
		return "", "", fmt.Errorf("新 Key 读回失败")
	}
	secret, err := m.platform.KeySecret(ctx, *site.Session, id, record)
	if err != nil {
		return "", "", err
	}
	if configure {
		if err = m.platform.ConfigureSecurityKey(ctx, *site.Session, id, cfg); err != nil {
			return "", "", err
		}
		if err = m.platform.VerifyManagedKeyConfig(ctx, *site.Session, id, cfg); err != nil {
			return "", "", err
		}
	}
	return id, secret, nil
}

func (m *KeyManagement) ensureManagedAccount(ctx context.Context, session upstream.Session, id, name string, source RealConnection, key string, create map[string]any) (string, error) {
	if id == "" {
		accounts, err := m.platform.ListSub2APIAdminAccounts(session)
		if err != nil {
			return "", err
		}
		for _, account := range accounts {
			if account.Name == name {
				if id != "" {
					return "", fmt.Errorf("存在多个同名转发账号")
				}
				id = account.ID
			}
		}
		if id == "" {
			if create == nil {
				id, err = m.platform.CloneForwardingAccount(ctx, session, source.AdminAccountID, name, source.UpstreamKey, key)
			} else {
				id, err = m.platform.CreateSub2APIAdminAccount(session, create)
			}
			if err != nil {
				return "", err
			}
		}
	}
	account, err := m.platform.FetchForwardingAccount(ctx, session, id)
	if err != nil || account.Name != name || !upstream.SameMonitorKey(upstream.PlatformNewAPI, account.APIKey, key) {
		return "", fmt.Errorf("新转发账号凭据或名称未通过回读")
	}
	return id, nil
}

func (m *KeyManagement) commitSplit(ctx context.Context, source RealConnection, retained bindingTarget, children []RealConnection) error {
	uid, aid := source.UserID, source.WorkspaceAdminAccountID
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE real_connections SET admin_account_name=$4,own_group_ids=$5 WHERE id=$1 AND user_id=$2 AND workspace_admin_account_id=$3`,
		source.ID, uid, aid, retained.Name, []string{retained.GroupID}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO managed_binding_splits(user_id,workspace_id,connection_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, uid, aid, source.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE managed_binding_names SET applied_name=$5 WHERE connection_id=$1 AND user_id=$2 AND workspace_id=$3 AND own_group_id=$4`, source.ID, uid, aid, retained.GroupID, retained.Name); err != nil {
		return err
	}
	for _, child := range children {
		_, err = tx.Exec(ctx, `INSERT INTO real_connections(id,user_id,workspace_admin_account_id,upstream_site_id,upstream_group_id,upstream_group_name,upstream_key_id,upstream_key,admin_account_id,admin_account_name,own_group_ids,group_type,created_at,upstream_api_url)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(id) DO NOTHING`, child.ID, uid, aid, child.UpstreamSiteID, child.UpstreamGroupID, child.UpstreamGroupName, child.UpstreamKeyID, child.UpstreamKey, child.AdminAccountID, child.AdminAccountName, child.OwnGroupIDs, child.GroupType, child.CreatedAt, child.UpstreamAPIURL)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE managed_binding_names SET connection_id=$5,applied_name=$6 WHERE connection_id=$1 AND user_id=$2 AND workspace_id=$3 AND own_group_id=$4`, source.ID, uid, aid, child.OwnGroupIDs[0], child.ID, child.AdminAccountName); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE managed_binding_progress SET step='done' WHERE user_id=$1 AND workspace_id=$2 AND operation_id=$3`, uid, aid, source.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (m *KeyManagement) lockBindingMutation(ctx context.Context, conn RealConnection) (func(), error) {
	unlock, err := m.lockOperation(ctx, "managed-binding|"+conn.UserID+"|"+conn.WorkspaceAdminAccountID+"|"+conn.ID)
	if err != nil {
		return nil, err
	}
	unlockKey, err := m.lockOperation(ctx, "key-security|"+conn.UpstreamSiteID+"|"+conn.UpstreamKeyID)
	if err != nil {
		unlock()
		return nil, err
	}
	var pending bool
	err = m.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM managed_binding_progress WHERE user_id=$1 AND workspace_id=$2 AND operation_id=$3 AND step NOT IN('','done'))", conn.UserID, conn.WorkspaceAdminAccountID, conn.ID).Scan(&pending)
	if err != nil || pending {
		unlockKey()
		unlock()
		return nil, fmt.Errorf("该绑定有未完成的拆分，请在任务进度处理")
	}
	return func() { unlockKey(); unlock() }, nil
}

func (m *KeyManagement) pendingKeySplit(ctx context.Context, uid, aid, site, id string) (bool, error) {
	var pending bool
	err := m.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM managed_binding_progress p LEFT JOIN real_connections c ON c.id=p.operation_id AND c.user_id=p.user_id AND c.workspace_admin_account_id=p.workspace_id WHERE p.user_id=$1 AND p.workspace_id=$2 AND p.site_id=$3 AND (p.key_id=$4 OR c.upstream_key_id=$4) AND p.step NOT IN('','done'))", uid, aid, site, id).Scan(&pending)
	return pending, err
}

func (m *KeyManagement) finishManagedRotation(ctx context.Context, uid, aid string, site *upstream.Site, r rotationState) error {
	var name string
	err := m.db.QueryRow(ctx, "SELECT coalesce(min(n.applied_name),'') FROM managed_binding_names n JOIN real_connections c ON c.id=n.connection_id AND c.user_id=n.user_id AND c.workspace_admin_account_id=n.workspace_id WHERE n.user_id=$1 AND n.workspace_id=$2 AND c.upstream_site_id=$3 AND c.upstream_key_id=$4 AND n.applied_name<>''", uid, aid, site.ID, r.NewID).Scan(&name)
	if err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	if err = m.alias(ctx, uid, aid, site.ID, r.NewID, keyString(r.Config["name"])); err != nil {
		return err
	}
	if _, err = m.db.Exec(ctx, "INSERT INTO managed_key_lineage(user_id,workspace_id,site_id,previous_id,successor_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", uid, aid, site.ID, r.OldID, r.NewID); err != nil {
		return err
	}
	if err = m.platform.RenameManagedKey(ctx, *site.Session, r.NewID, name); err != nil {
		return err
	}
	m.invalidate(site)
	return nil
}
