package my_sites

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"transithub/backend/internal/modules/upstream"
)

func (m *KeyManagement) connectManaged(ctx context.Context, uid string, state *State, site *upstream.Site, req RealConnectRequest, groupType, groupName string) (RealConnectResponse, error) {
	aid := state.AdminAccountID
	groups := uniqueStrings(req.OwnGroupIDs)
	if len(groups) == 0 || len(groups) > 30 {
		return RealConnectResponse{}, fmt.Errorf("请选择 1 至 30 个自有分组")
	}
	unlock, err := m.lockOperation(ctx, "managed-create|"+uid+"|"+aid+"|"+site.ID+"|"+req.UpstreamGroupID)
	if err != nil {
		return RealConnectResponse{}, err
	}
	defer unlock()
	existing, err := m.sites.connRepository.ListRealConnections(ctx, uid, aid)
	if err != nil {
		return RealConnectResponse{}, err
	}
	own, err := m.platform.FetchAdminAllGroups(state.Session)
	if err != nil {
		return RealConnectResponse{}, err
	}
	labels := map[string]string{}
	for _, group := range own {
		labels[group.ID] = group.Name
	}
	for _, id := range groups {
		_, parseErr := strconv.Atoi(id)
		if parseErr != nil || labels[id] == "" {
			return RealConnectResponse{}, fmt.Errorf("自有分组 %s 不存在", id)
		}
	}
	slugs, err := m.siteSlugs(ctx, uid, aid)
	if err != nil {
		return RealConnectResponse{}, err
	}
	slug := slugs[site.ID]
	if slug == "" {
		slug = bindingSlug(site.BaseURL)
	}
	rate := managedRate(site, req.UpstreamGroupID)
	if _, err = managedBindingName(slug, labels[groups[0]], rate, 1, site.Platform); err != nil {
		return RealConnectResponse{}, err
	}
	initializations := []bindingNameRow{}
	orderedBindings(existing)
	for _, conn := range existing {
		if conn.AdminAccountID == "" || conn.UpstreamKeyID == "" || conn.UpstreamSiteID != site.ID {
			continue
		}
		for _, group := range conn.OwnGroupIDs {
			initializations = append(initializations, bindingNameRow{ConnectionID: conn.ID, GroupID: group, Slug: slug})
		}
	}
	if _, err = m.reserveBindingNames(ctx, uid, aid, initializations); err != nil {
		return RealConnectResponse{}, err
	}
	defer m.invalidate(site)
	response := RealConnectResponse{Connections: []RealConnection{}}
	for _, group := range groups {
		conn, err := m.connectManagedGroup(ctx, uid, state, site, req, groupType, groupName, group, labels[group], slug, rate, existing)
		if err != nil {
			if len(response.Connections) > 0 {
				return response, fmt.Errorf("已完成 %d 个分组，剩余分组可重试: %w", len(response.Connections), err)
			}
			return response, err
		}
		response.Connections = append(response.Connections, conn)
	}
	response.Connection = response.Connections[0]
	return response, nil
}

func (m *KeyManagement) connectManagedGroup(ctx context.Context, uid string, state *State, site *upstream.Site, req RealConnectRequest, platform, groupName, group, label, slug string, rate *float64, existing []RealConnection) (RealConnection, error) {
	aid := state.AdminAccountID
	for _, c := range existing {
		if c.UpstreamSiteID != site.ID || c.UpstreamGroupID != req.UpstreamGroupID || !slices.Contains(c.OwnGroupIDs, group) {
			continue
		}
		if len(c.OwnGroupIDs) != 1 || c.AdminAccountID == "" {
			return RealConnection{}, fmt.Errorf("已有共享对接，请先在 Key 管理中拆分")
		}
		if err := m.sites.configureBindingModel(ctx, &c, req.TestModelID, req.AllowWhenUpstreamRateGteOwn); err != nil {
			return c, err
		}
		return c, nil
	}
	hash := sha256.Sum256([]byte(uid + "\x00" + aid + "\x00" + site.ID + "\x00" + req.UpstreamGroupID + "\x00" + group))
	id := fmt.Sprintf("mk-%x", hash[:16])
	names, err := m.reserveBindingNames(ctx, uid, aid, []bindingNameRow{{ConnectionID: id, GroupID: group, Slug: slug}})
	if err != nil {
		return RealConnection{}, err
	}
	name, err := managedBindingName(names[0].Slug, label, rate, names[0].Number, site.Platform)
	if err != nil {
		return RealConnection{}, err
	}
	keyID, accountID, step, err := m.progress(ctx, uid, aid, id, site.ID, group)
	if err != nil {
		return RealConnection{}, err
	}
	if step == "" {
		keys, err := m.platform.SecurityKeys(ctx, *site.Session)
		if err != nil {
			return RealConnection{}, err
		}
		accounts, err := m.platform.ListSub2APIAdminAccounts(state.Session)
		if err != nil {
			return RealConnection{}, err
		}
		for retries := 0; nameCollision(keys, accounts, name, "", ""); retries++ {
			if retries == 10 {
				return RealConnection{}, fmt.Errorf("存在多个名称冲突")
			}
			number, e := m.skipOccupiedOrdinal(ctx, uid, aid, names[0])
			if e != nil {
				return RealConnection{}, e
			}
			name, e = managedBindingName(names[0].Slug, label, rate, number, site.Platform)
			if e != nil {
				return RealConnection{}, e
			}
		}
		if err = m.saveProgress(ctx, uid, aid, id, group, "", "", "creating"); err != nil {
			return RealConnection{}, err
		}
	}
	ips, err := m.sites.keyIPs(ctx, uid, aid, site.ID, req.UpstreamGroupID)
	if err != nil {
		return RealConnection{}, err
	}
	config, err := newBindingKeyConfig(site.Platform, name, req.UpstreamGroupID, ips)
	if err != nil {
		return RealConnection{}, err
	}
	keyID, secret, err := m.ensureManagedKey(ctx, site, keyID, name, config, step != "ready")
	if err != nil {
		return RealConnection{}, err
	}
	if step != "ready" {
		if err = m.saveProgress(ctx, uid, aid, id, group, keyID, accountID, "key_ready"); err != nil {
			return RealConnection{}, err
		}
	}
	base := upstream.EffectiveAPIURL(*site, req.UpstreamGroupID, "")
	groupInt, err := strconv.Atoi(group)
	if err != nil {
		return RealConnection{}, err
	}
	payload := buildAccountPayload(platform, base, secret, []int{groupInt}, name)
	accountID, err = m.ensureManagedAccount(ctx, state.Session, accountID, name, RealConnection{}, secret, payload)
	if err != nil {
		return RealConnection{}, err
	}
	if err = m.saveProgress(ctx, uid, aid, id, group, keyID, accountID, "ready"); err != nil {
		return RealConnection{}, err
	}
	if err = m.setManagedAccountGroups(ctx, state.Session, accountID, []string{group}); err != nil {
		return RealConnection{}, err
	}
	conn := RealConnection{
		ID: id, UserID: uid, WorkspaceAdminAccountID: aid, UpstreamSiteID: site.ID, UpstreamAPIURL: base,
		UpstreamGroupID: req.UpstreamGroupID, UpstreamGroupName: groupName, UpstreamKeyID: keyID, UpstreamKey: secret,
		AdminAccountID: accountID, AdminAccountName: name, OwnGroupIDs: []string{group}, GroupType: platform, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err = m.sites.connRepository.SaveRealConnection(ctx, conn); err != nil {
		return RealConnection{}, err
	}
	if _, err = m.db.Exec(ctx, `UPDATE managed_binding_names SET applied_name=$5 WHERE user_id=$1 AND workspace_id=$2 AND connection_id=$3 AND own_group_id=$4`, uid, aid, id, group, name); err != nil {
		return conn, err
	}
	if err = m.saveProgress(ctx, uid, aid, id, group, keyID, accountID, "done"); err != nil {
		return conn, err
	}
	m.sites.addUpstreamMapping(ctx, uid, aid, conn.OwnGroupIDs, site.ID, groupName)
	if err = m.sites.configureBindingModel(ctx, &conn, req.TestModelID, req.AllowWhenUpstreamRateGteOwn); err != nil {
		return conn, err
	}
	m.sites.membershipChanged(ctx, conn, conn.OwnGroupIDs)
	return conn, nil
}

func newBindingKeyConfig(platform upstream.Platform, name, group string, ips []string) (map[string]any, error) {
	if platform == upstream.PlatformNewAPI {
		return map[string]any{"name": name, "group": group, "unlimited_quota": true, "expired_time": -1, "allow_ips": strings.Join(ips, "\n")}, nil
	}
	id, err := strconv.Atoi(group)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": name, "group_id": id, "ip_whitelist": ips}, nil
}
