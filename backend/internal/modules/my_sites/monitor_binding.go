package my_sites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"transithub/backend/internal/modules/upstream"
)

type MonitorBindingRequest struct {
	SiteID      string   `json:"siteId,omitempty"`
	GroupID     string   `json:"groupId,omitempty"`
	APIURL      string   `json:"apiUrl"`
	APIKey      string   `json:"apiKey"`
	AccountID   string   `json:"accountId,omitempty"`
	OwnGroupIDs []string `json:"ownGroupIds,omitempty"`
}
type MonitorBindingItem struct {
	AccountID     string `json:"accountId"`
	AccountName   string `json:"accountName"`
	ConnectionID  string `json:"connectionId,omitempty"`
	SiteName      string `json:"siteName,omitempty"`
	GroupName     string `json:"groupName,omitempty"`
	Status        string `json:"status"`
	NeedsTakeover bool   `json:"needsTakeover,omitempty"`
	Message       string `json:"message,omitempty"`
}
type MonitorBindingResult struct {
	Items   []MonitorBindingItem `json:"items"`
	Matched int                  `json:"matched"`
	Skipped int                  `json:"skipped"`
}

type monitorBindingWriter interface {
	SaveMonitorBinding(context.Context, RealConnection) error
}
type monitorSiteList interface {
	MonitorSites(context.Context, string, string) (map[string]*upstream.Site, error)
}

// Imports existing forwarding accounts by exact API base and full Key. No Key
// creation, upstream whitelist mutation, or generation is performed here.
func (s *Service) SyncMonitorBindings(ctx context.Context, uid, aid string, req MonitorBindingRequest) (MonitorBindingResult, error) {
	return s.syncMonitorBindings(ctx, uid, aid, req, true)
}

// Background key security has an explicit persisted workspace scope. It must
// not depend on which workspace happens to be selected in a browser tab.
func (s *Service) syncMonitorBindings(ctx context.Context, uid, aid string, req MonitorBindingRequest, requireCurrent bool) (MonitorBindingResult, error) {
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.APIURL = strings.TrimSpace(req.APIURL)
	if strings.HasPrefix(strings.ToLower(req.APIKey), "bearer ") {
		req.APIKey = strings.TrimSpace(req.APIKey[7:])
	}
	s.repairMu.Lock()
	defer s.repairMu.Unlock()
	result := MonitorBindingResult{Items: []MonitorBindingItem{}}
	if requireCurrent {
		current, e := s.currentAdminAccountID(ctx, uid)
		if e != nil || current != aid {
			return result, fmt.Errorf("当前工作区已变化，请重新同步")
		}
	}
	writer, ok := s.connRepository.(monitorBindingWriter)
	if !ok {
		return result, fmt.Errorf("绑定存储未就绪")
	}
	siteProvider, ok := s.upstreamLookup.(monitorSiteList)
	if !ok {
		return result, fmt.Errorf("上游列表不可用")
	}
	sites, e := siteProvider.MonitorSites(ctx, uid, aid)
	if e != nil {
		return result, e
	}
	state, e := s.authenticatedState(ctx, uid, aid)
	if e != nil {
		return result, e
	}
	if state == nil || state.Session.Platform != upstream.PlatformSub2API {
		return result, fmt.Errorf("当前自有站暂不支持读取渠道凭据")
	}
	accounts, e := s.platformService.ListSub2APIAdminAccounts(state.Session)
	if e != nil {
		return result, fmt.Errorf("无法读取自有渠道")
	}
	existing, e := s.connRepository.ListRealConnections(ctx, uid, aid)
	if e != nil {
		return result, e
	}
	if req.APIURL != "" || req.APIKey != "" {
		if upstream.MonitorAPIBase(req.APIURL) == "" || strings.TrimSpace(req.APIKey) == "" {
			return result, fmt.Errorf("请同时填写有效 API 地址和 Key")
		}
	}
	// Newer sub2api lists redact API keys. Hydrate only relevant API-key
	// accounts through its authorized single-account export, with bounded reads.
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	credentialErrors := make([]error, len(accounts))
	for i := range accounts {
		a := accounts[i]
		if req.AccountID != "" && a.ID != req.AccountID {
			continue
		}
		if req.APIURL != "" && a.BaseURL != "" && upstream.MonitorAPIBase(a.BaseURL) != upstream.MonitorAPIBase(req.APIURL) {
			continue
		}
		if a.APIKey != "" && !strings.Contains(a.APIKey, "*") {
			continue
		}
		if a.Type != "apikey" && a.Type != "api_key" && a.Type != "api-key" {
			continue
		}
		slots <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-slots }()
			run, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			detail, err := s.platformService.FetchForwardingAccount(run, state.Session, accounts[i].ID)
			if err == nil {
				detail.Schedulable = accounts[i].Schedulable
				detail.Priority = accounts[i].Priority
				detail.RateMultiplier = accounts[i].RateMultiplier
				accounts[i] = detail
			} else {
				credentialErrors[i] = err
			}
		}(i)
	}
	wg.Wait()
	if req.APIKey != "" {
		filtered := []upstream.Sub2APIAdminAccountStatus{}
		for _, account := range accounts {

			if req.AccountID != "" && account.ID != req.AccountID {
				continue
			}
			if upstream.MonitorAPIBase(account.BaseURL) == upstream.MonitorAPIBase(req.APIURL) && (upstream.SameMonitorKey(upstream.PlatformSub2API, account.APIKey, req.APIKey) || upstream.SameMonitorKey(upstream.PlatformNewAPI, account.APIKey, req.APIKey)) {
				filtered = append(filtered, account)
			}
		}
		if len(filtered) == 0 {
			if req.AccountID != "" {
				return result, fmt.Errorf("API 地址及 Key 与该自有渠道不一致")
			}
			if len(req.OwnGroupIDs) == 0 {
				return result, fmt.Errorf("未匹配到自有渠道，请选择监控所属的自有分组")
			}
			groups, e := s.platformService.FetchAdminAllGroups(state.Session)
			if e != nil {
				return result, fmt.Errorf("无法验证自有分组")
			}
			valid := map[string]bool{}
			for _, g := range groups {
				valid[g.ID] = true
			}
			for _, id := range req.OwnGroupIDs {
				if !valid[id] {
					return result, fmt.Errorf("自有分组不存在")
				}
			}
			filtered = append(filtered, upstream.Sub2APIAdminAccountStatus{BaseURL: req.APIURL, APIKey: req.APIKey, GroupIDs: uniqueStrings(req.OwnGroupIDs), Name: "API Key 监控"})
		}
		accounts = filtered
	}
	// Cache by site and Key only inside this operation; never write secrets to job output.
	type match struct {
		site *upstream.Site
		key  upstream.Sub2APIKeyItem
		err  error
	}
	cache := map[string]match{}
	mappingChanged := false
	for accountIndex, account := range accounts {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if req.AccountID != "" && account.ID != req.AccountID {
			continue
		}
		item := MonitorBindingItem{AccountID: account.ID, AccountName: account.Name, Status: "skipped"}

		if upstream.MonitorAPIBase(account.BaseURL) == "" || account.APIKey == "" || strings.Contains(account.APIKey, "*") {
			item.Message = "无可读取的 API 地址或完整 Key"
			if req.APIKey == "" && accountIndex < len(credentialErrors) && credentialErrors[accountIndex] != nil {
				item.Message = credentialErrors[accountIndex].Error()
			}
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		matches := []match{}
		for _, site := range sites {
			if site == nil || site.UserID != uid || site.AdminAccountID != aid || site.Session == nil {
				continue
			}
			if req.SiteID != "" && req.SiteID != site.ID {
				continue
			}
			base := upstream.MonitorAPIBase(account.BaseURL)
			candidate := req.SiteID == site.ID || upstream.MonitorAPICandidate(base, site.BaseURL) || upstream.MonitorAPICandidate(base, site.Session.BaseURL)
			for _, apiURL := range site.Settings.GroupAPIURLs {
				if upstream.MonitorAPICandidate(base, apiURL) {
					candidate = true
				}
			}
			for _, previous := range existing {
				if previous.UpstreamSiteID == site.ID && (account.ID != "" && previous.AdminAccountID == account.ID || upstream.SameMonitorKey(site.Platform, previous.UpstreamKey, account.APIKey)) {
					candidate = true
				}
			}
			if !candidate {
				continue
			}
			fingerprint := sha256.Sum256([]byte(site.ID + "|" + account.APIKey))
			key := hex.EncodeToString(fingerprint[:])
			m, exists := cache[key]
			if !exists {
				keyItem, err := s.platformService.FindAPIKeyGroup(ctx, *site, account.APIKey)
				m = match{site: site, key: keyItem, err: err}
				cache[key] = m
			}
			if m.err != nil && req.SiteID == site.ID && req.GroupID != "" {
				for _, group := range site.Metrics.Groups {
					if group.ID == req.GroupID {
						m.key.GroupID = group.ID
						m.key.GroupName = group.Name
						m.err = nil
						break
					}
				}
			}
			if m.err == nil {
				matches = append(matches, m)
			} else {
				item.Message = m.err.Error()
			}
		}
		if len(matches) != 1 {
			if len(matches) > 1 {
				item.Message = "多个上游账号匹配同一 Key，请先消除重复站点"
			} else if item.Message == "" {
				item.Message = "API 地址未匹配当前工作区的已登录上游"
			}
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		selected := matches[0]
		groupID, groupName := selected.key.GroupID, selected.key.GroupName
		groupType := ""
		found := false
		for _, g := range selected.site.Metrics.Groups {
			if g.ID == groupID || g.Name == groupID {
				groupID = g.ID
				groupName = g.Name
				if g.Platform != nil {
					groupType = normalizeConnectionGroupType(*g.Platform)
				}
				found = true
				break
			}
		}
		if !found && groupName == "" {
			item.Message = "Key 分组尚未同步，请先刷新上游分组"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		if groupType == "" {
			groupType = normalizeConnectionGroupType(account.Platform)
		}
		if groupType == "" {
			for _, g := range state.OwnGroups {
				for _, id := range account.GroupIDs {
					if g.ID == id && g.Platform != "" {
						groupType = g.Platform
						break
					}
				}
			}
		}
		if groupType == "" {
			item.Message = "无法确认渠道模型类型"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		if len(account.GroupIDs) == 0 {
			item.Message = "自有渠道未分配分组"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		conn := RealConnection{UserID: uid, WorkspaceAdminAccountID: aid, UpstreamSiteID: selected.site.ID, UpstreamAPIURL: account.BaseURL, UpstreamGroupID: groupID, UpstreamGroupName: groupName, UpstreamKeyID: selected.key.ID, UpstreamKey: account.APIKey, AdminAccountID: account.ID, AdminAccountName: account.Name, OwnGroupIDs: uniqueStrings(account.GroupIDs), GroupType: groupType, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		existingIndex := -1
		item.NeedsTakeover = account.ID != ""
		for index, v := range existing {
			if account.ID != "" && v.AdminAccountID == account.ID || v.AdminAccountID == "" && v.UpstreamSiteID == conn.UpstreamSiteID && upstream.SameMonitorKey(selected.site.Platform, v.UpstreamKey, account.APIKey) {
				conn.ID = v.ID
				conn.CreatedAt = v.CreatedAt
				existingIndex = index
				item.NeedsTakeover = account.ID != "" && v.AdminAccountID == ""
				break
			}
		}
		if conn.ID == "" {
			sum := sha256.Sum256([]byte(uid + "|" + aid + "|" + account.ID + "|" + selected.site.ID + "|" + account.APIKey))
			conn.ID = "import_" + hex.EncodeToString(sum[:16])
		}
		if e = writer.SaveMonitorBinding(ctx, conn); e != nil {
			return result, fmt.Errorf("保存监控绑定失败")
		}
		if existingIndex >= 0 {
			existing[existingIndex] = conn
		} else {
			existing = append(existing, conn)
		}
		for _, own := range state.OwnGroups {
			if !containsMonitorGroup(conn.OwnGroupIDs, own.ID) {
				continue
			}
			target := UpstreamGroupRef{SiteID: conn.UpstreamSiteID, GroupName: conn.UpstreamGroupName}
			exists := false
			for i := range state.Mappings {
				if state.Mappings[i].OwnGroup == own.Name {
					exists = true
					if !hasUpstreamTarget(state.Mappings[i].UpstreamTargets, target) {
						state.Mappings[i].UpstreamTargets = append(state.Mappings[i].UpstreamTargets, target)
						mappingChanged = true
					}
					break
				}
			}
			if !exists {
				state.Mappings = append(state.Mappings, GroupMapping{OwnGroup: own.Name, UpstreamTargets: []UpstreamGroupRef{target}})
				mappingChanged = true
			}
		}
		item.Status = "matched"
		item.ConnectionID = conn.ID
		item.SiteName = selected.site.Name
		item.GroupName = groupName
		if account.ID == "" {
			item.Message = "已接入监控；未关联自有转发渠道"
		}
		result.Matched++
		result.Items = append(result.Items, item)
	}
	if mappingChanged {
		if e = s.repository.Save(ctx, *state); e != nil {
			return result, fmt.Errorf("保存分组映射失败")
		}
	}
	return result, nil
}

func (r *Repository) SaveMonitorBinding(ctx context.Context, conn RealConnection) error {
	raw, _ := json.Marshal(conn.OwnGroupIDs)
	tag, e := r.db.Exec(ctx, `INSERT INTO real_connections(id,user_id,workspace_admin_account_id,upstream_site_id,upstream_group_id,upstream_group_name,upstream_key_id,upstream_key,admin_account_id,admin_account_name,own_group_ids,group_type,created_at,upstream_api_url) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(id) DO UPDATE SET upstream_site_id=EXCLUDED.upstream_site_id,upstream_group_id=EXCLUDED.upstream_group_id,upstream_group_name=EXCLUDED.upstream_group_name,upstream_key_id=EXCLUDED.upstream_key_id,upstream_key=EXCLUDED.upstream_key,admin_account_id=EXCLUDED.admin_account_id,admin_account_name=EXCLUDED.admin_account_name,own_group_ids=EXCLUDED.own_group_ids,group_type=EXCLUDED.group_type,upstream_api_url=EXCLUDED.upstream_api_url WHERE real_connections.user_id=EXCLUDED.user_id AND real_connections.workspace_admin_account_id=EXCLUDED.workspace_admin_account_id`, conn.ID, conn.UserID, conn.WorkspaceAdminAccountID, conn.UpstreamSiteID, conn.UpstreamGroupID, conn.UpstreamGroupName, conn.UpstreamKeyID, conn.UpstreamKey, conn.AdminAccountID, conn.AdminAccountName, raw, conn.GroupType, conn.CreatedAt, conn.UpstreamAPIURL)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("绑定不属于当前工作区")
	}
	return nil
}

func containsMonitorGroup(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
