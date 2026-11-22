package my_sites

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/httpjson"
)

type bindingTarget struct {
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName"`
	Name      string `json:"name"`
	Number    int    `json:"number"`
	Retain    bool   `json:"retain"`
	Error     string `json:"error,omitempty"`
}

type bindingPlan struct {
	ID          string             `json:"id"`
	SiteID      string             `json:"siteId"`
	SiteName    string             `json:"siteName"`
	KeyName     string             `json:"keyName"`
	AccountName string             `json:"accountName"`
	Targets     []bindingTarget    `json:"targets"`
	Limits      map[string]float64 `json:"limits,omitempty"`
	Error       string             `json:"error,omitempty"`
}

type bindingSelection struct {
	ID      string                        `json:"id"`
	Retain  string                        `json:"retain"`
	Budgets map[string]map[string]float64 `json:"budgets"`
}

type managedBindingJob struct {
	Selection bindingSelection
	Plan      bindingPlan
	KeyID     string
	AccountID string
}

func (m *KeyManagement) previewBindings(w http.ResponseWriter, r *http.Request) {
	uid, aid, err := m.scope(r)
	if err != nil {
		httpjson.WriteError(w, 401, "工作区会话不可用")
		return
	}
	plans, err := m.buildBindingPlans(r.Context(), uid, aid, r.URL.Query().Get("site"))
	apiResult(w, plans, err)
}

func (m *KeyManagement) buildBindingPlans(ctx context.Context, uid, aid, onlySite string) ([]bindingPlan, error) {
	conns, err := m.sites.connRepository.ListRealConnections(ctx, uid, aid)
	if err != nil {
		return nil, err
	}
	state, err := m.sites.repository.Get(ctx, uid, aid)
	if err != nil || state == nil || state.Session.Platform != upstream.PlatformSub2API {
		return nil, fmt.Errorf("请先登录自有 Sub2API")
	}
	sites, err := m.workspaceSites(ctx, uid, aid)
	if err != nil {
		return nil, err
	}
	slugs, err := m.siteSlugs(ctx, uid, aid)
	if err != nil {
		return nil, err
	}
	orderedBindings(conns)
	reservations := []bindingNameRow{}
	managed := []RealConnection{}
	for _, conn := range conns {
		if conn.AdminAccountID == "" || conn.UpstreamKeyID == "" || len(conn.OwnGroupIDs) == 0 {
			continue
		}
		managed = append(managed, conn)
		slug := slugs[conn.UpstreamSiteID]
		if site := sites[conn.UpstreamSiteID]; slug == "" && site != nil {
			slug = bindingSlug(site.BaseURL)
		}
		for _, group := range conn.OwnGroupIDs {
			reservations = append(reservations, bindingNameRow{ConnectionID: conn.ID, GroupID: group, Slug: slug})
		}
	}
	reservations, err = m.reserveBindingNames(ctx, uid, aid, reservations)
	if err != nil {
		return nil, err
	}
	byID := map[string][]bindingNameRow{}
	for _, v := range reservations {
		byID[v.ConnectionID] = append(byID[v.ConnectionID], v)
	}
	labels := map[string]string{}
	for _, own := range state.OwnGroups {
		labels[own.ID] = own.Name
	}
	plans := []bindingPlan{}
	var accounts []upstream.Sub2APIAdminAccountStatus
	admin, adminErr := m.sites.RequireSession(ctx, uid, aid)
	if adminErr == nil {
		accounts, adminErr = m.platform.ListSub2APIAdminAccounts(admin)
	}
	inventory := map[string]map[string]keyEntry{}
	for _, conn := range managed {
		if onlySite != "" && conn.UpstreamSiteID != onlySite {
			continue
		}
		site := sites[conn.UpstreamSiteID]
		plan := bindingPlan{ID: conn.ID, SiteID: conn.UpstreamSiteID, AccountName: conn.AdminAccountName, Targets: []bindingTarget{}}
		if site == nil || site.Session == nil {
			plan.Error = "上游站点或会话不可用"
			plans = append(plans, plan)
			continue
		}
		plan.SiteName = site.Name
		rows, ok := inventory[site.ID]
		if !ok {
			rows = map[string]keyEntry{}
			entries, _ := m.inventory(ctx, site)
			for _, row := range entries {
				rows[row.ID] = row
			}
			inventory[site.ID] = rows
		}
		key, ok := rows[conn.UpstreamKeyID]
		plan.KeyName = key.Name
		if !ok {
			plan.Error = "无法读取当前上游 Key"
		}
		if adminErr != nil {
			plan.Error = "无法核对自有 Sub2API 账号列表"
		}
		keyRecords := make([]map[string]any, 0, len(rows))
		for _, entry := range rows {
			keyRecords = append(keyRecords, map[string]any{"id": entry.ID, "name": entry.Name})
		}
		rate := managedRate(site, conn.UpstreamGroupID)
		if rate == nil && ok && site.Platform == upstream.PlatformSub2API {
			if record, _, err := m.platform.ReadSecurityKey(ctx, *site.Session, conn.UpstreamKeyID); err == nil {
				rate = verifiedKeyGroupRate(record, conn.UpstreamGroupID)
			}
		}
		plan.Limits = key.Remaining
		if len(conn.OwnGroupIDs) == 1 {
			plan.Limits = nil
		}
		for i, name := range byID[conn.ID] {
			target := bindingTarget{GroupID: name.GroupID, GroupName: labels[name.GroupID], Number: name.Number, Retain: i == 0}
			target.Name, err = managedBindingName(name.Slug, target.GroupName, rate, name.Number, site.Platform)
			for retries := 0; err == nil && adminErr == nil && nameCollision(keyRecords, accounts, target.Name, conn.UpstreamKeyID, conn.AdminAccountID); retries++ {
				if retries == 10 {
					err = fmt.Errorf("存在多个名称冲突")
					break
				}
				target.Number, err = m.skipOccupiedOrdinal(ctx, uid, aid, name)
				if err != nil {
					break
				}
				target.Name, err = managedBindingName(name.Slug, target.GroupName, rate, target.Number, site.Platform)
			}
			if err != nil {
				target.Error = err.Error()
			}
			plan.Targets = append(plan.Targets, target)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func remainingLimits(platform upstream.Platform, record map[string]any) map[string]float64 {
	out := map[string]float64{}
	if platform == upstream.PlatformNewAPI {
		if unlimited, ok := record["unlimited_quota"].(bool); ok && !unlimited {
			out["remain_quota"], _ = record["remain_quota"].(float64)
		}
		return out
	}
	for _, name := range []string{"quota", "rate_limit_5h", "rate_limit_1d", "rate_limit_7d"} {
		if value, ok := record[name].(float64); ok && value > 0 {
			usedName := strings.Replace(name, "rate_limit_", "usage_", 1)
			if name == "quota" {
				usedName = "quota_used"
			}
			used, _ := record[usedName].(float64)
			out[name] = max(0, value-used)
		}
	}
	return out
}

func (m *KeyManagement) applyBindings(w http.ResponseWriter, r *http.Request) {
	uid, aid, err := m.scope(r)
	if err != nil {
		httpjson.WriteError(w, 401, "工作区会话不可用")
		return
	}
	var input struct {
		Entries []bindingSelection `json:"entries"`
	}
	if httpjson.Decode(r, &input) != nil || len(input.Entries) == 0 || len(input.Entries) > 200 {
		httpjson.WriteError(w, 400, "请选择 1 至 200 条绑定")
		return
	}
	ctx := r.Context()
	plans, err := m.buildBindingPlans(ctx, uid, aid, "")
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	byID := map[string]bindingPlan{}
	for _, plan := range plans {
		byID[plan.ID] = plan
	}
	unlock, err := m.lockOperation(ctx, "managed-binding-submit|"+uid+"|"+aid)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	defer unlock()
	var items []jobs.Item
	seen := map[string]bool{}
	for _, selection := range input.Entries {
		if seen[selection.ID] {
			httpjson.WriteError(w, 400, "重复选择绑定")
			return
		}
		seen[selection.ID] = true
		var active bool
		err = m.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM operation_jobs j JOIN operation_items i ON j.id=i.job_id WHERE j.user_id=$1 AND j.admin_account_id=$2 AND j.kind='managed_key_binding' AND i.id=$3 AND i.status IN('queued','running','waiting'))", uid, aid, selection.ID).Scan(&active)
		if err != nil {
			apiResult(w, nil, err)
			return
		}
		if active {
			httpjson.WriteError(w, 409, "所选绑定已有进行中的任务")
			return
		}
		plan := byID[selection.ID]
		if plan.ID == "" || plan.Error != "" {
			httpjson.WriteError(w, 409, "绑定预览不可执行")
			return
		}
		conn, err := m.sites.connRepository.GetRealConnection(ctx, selection.ID, uid, aid)
		if err != nil || conn == nil {
			httpjson.WriteError(w, 409, "绑定已变化")
			return
		}
		if selection.Retain == "" {
			selection.Retain = conn.OwnGroupIDs[0]
		}
		if !slices.Contains(conn.OwnGroupIDs, selection.Retain) {
			httpjson.WriteError(w, 422, "保留分组已变化")
			return
		}
		for _, target := range plan.Targets {
			if target.Error != "" {
				httpjson.WriteError(w, 422, target.Error)
				return
			}
		}
		if err = validateSplitBudgets(plan.Targets, plan.Limits, selection.Budgets); err != nil {
			apiResult(w, nil, err)
			return
		}
		items = append(items, jobs.Item{ID: conn.ID, Payload: encodeBindingPayload(managedBindingJob{Selection: selection, Plan: plan, KeyID: conn.UpstreamKeyID, AccountID: conn.AdminAccountID})})
	}
	job, err := m.jobs.Submit(ctx, uid, aid, "managed_key_binding", items)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	httpjson.Write(w, 202, job)
}

func validateSplitBudgets(targets []bindingTarget, limits map[string]float64, budgets map[string]map[string]float64) error {
	for name, limit := range limits {
		var total float64
		for _, target := range targets {
			value := budgets[target.GroupID][name]
			if value <= 0 {
				return fmt.Errorf("请为每组填写 %s 的可用额度", name)
			}
			total += value
		}
		if total > limit {
			return fmt.Errorf("各组 %s 的合计超过可用额度", name)
		}
	}
	return nil
}

func (m *KeyManagement) alias(ctx context.Context, uid, aid, site, id, name string) error {
	if name == "" {
		return nil
	}
	_, err := m.db.Exec(ctx, `INSERT INTO managed_key_aliases(user_id,workspace_id,site_id,key_id,name) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, uid, aid, site, id, name)
	return err
}

func (m *KeyManagement) progress(ctx context.Context, uid, aid, operation, site, group string) (key, account, step string, err error) {
	_, err = m.db.Exec(ctx, `INSERT INTO managed_binding_progress(user_id,workspace_id,operation_id,own_group_id,site_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, uid, aid, operation, group, site)
	if err != nil {
		return
	}
	err = m.db.QueryRow(ctx, `SELECT key_id,account_id,step FROM managed_binding_progress WHERE user_id=$1 AND workspace_id=$2 AND operation_id=$3 AND own_group_id=$4`, uid, aid, operation, group).Scan(&key, &account, &step)
	return
}

func (m *KeyManagement) saveProgress(ctx context.Context, uid, aid, operation, group, key, account, step string) error {
	_, err := m.db.Exec(ctx, `UPDATE managed_binding_progress SET key_id=$5,account_id=$6,step=$7 WHERE user_id=$1 AND workspace_id=$2 AND operation_id=$3 AND own_group_id=$4`, uid, aid, operation, group, key, account, step)
	return err
}

func bindingGroupIDs(groups []string) []string {
	out := slices.Clone(groups)
	slices.Sort(out)
	return out
}
