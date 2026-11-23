package my_sites

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
)

type rotationNaming struct {
	PreviousName string `json:"previousName,omitempty"`
	Name         string `json:"name,omitempty"`
	ConnectionID string `json:"connectionId,omitempty"`
	GroupID      string `json:"groupId,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

func (m *KeyManagement) prepareRotationNaming(ctx context.Context, uid, aid string, site *upstream.Site, session upstream.Session, conns []RealConnection, account upstream.Sub2APIAdminAccountStatus, r *rotationState) error {
	if r.Naming.Name != "" {
		return nil
	}
	naming := r.Naming
	if naming.PreviousName == "" {
		old, _, err := m.platform.ReadSecurityKey(ctx, *site.Session, r.OldID)
		var requestErr *upstream.RequestError
		if err != nil && !errors.Is(err, upstream.ErrSecurityKeyMissing) && (!errors.As(err, &requestErr) || requestErr.HTTPStatus != 404) {
			return err
		}
		naming.PreviousName = keyString(old["name"])
		if naming.PreviousName == "" && len(conns) == 1 {
			naming.PreviousName = account.Name
		}
	}

	keys, err := m.platform.SecurityKeys(ctx, *site.Session)
	if err != nil {
		return err
	}
	keys = slices.DeleteFunc(keys, func(key map[string]any) bool {
		id := upstream.SecurityKeyID(key)
		return id == r.OldID || id == r.NewID
	})
	if len(conns) == 1 && len(conns[0].OwnGroupIDs) == 1 && slices.Equal(conns[0].OwnGroupIDs, bindingGroupIDs(account.GroupIDs)) {
		err = m.planSingleRotationName(ctx, uid, aid, site, session, conns[0], keys, r.NewID, &naming)
		if err != nil {
			return err
		}
	} else {
		naming.Warning = "绑定涉及多个分组或分组映射不一致，请核对后同步命名"
	}
	if naming.Name == "" {
		naming.Name = naming.PreviousName
		if naming.Name == "" || nameCollision(keys, nil, naming.Name, "", "") {
			return fmt.Errorf("无法保留原 Key 名称，请核对名称和绑定后重试")
		}
	}
	if _, err = m.db.Exec(ctx, "UPDATE key_security_rotations SET naming=$2 WHERE id=$1", r.ID, naming); err != nil {
		return err
	}
	r.Naming = naming
	return nil
}

func (m *KeyManagement) planSingleRotationName(ctx context.Context, uid, aid string, site *upstream.Site, session upstream.Session, conn RealConnection, keys []map[string]any, newID string, naming *rotationNaming) error {
	state, err := m.sites.repository.Get(ctx, uid, aid)
	if err != nil {
		return err
	}
	groupID := conn.OwnGroupIDs[0]
	groupName := ""
	if state != nil {
		for _, group := range state.OwnGroups {
			if group.ID == groupID {
				groupName = group.Name
				break
			}
		}
	}
	rate := managedRate(site, conn.UpstreamGroupID)
	if rate == nil && site.Platform == upstream.PlatformSub2API {
		record, _, readErr := m.platform.ReadSecurityKey(ctx, *site.Session, newID)
		if readErr != nil {
			return readErr
		}
		rate = verifiedKeyGroupRate(record, conn.UpstreamGroupID)
	}
	slugs, err := m.siteSlugs(ctx, uid, aid)
	if err != nil {
		return err
	}
	slug := slugs[site.ID]
	if slug == "" {
		slug = bindingSlug(site.BaseURL)
	}
	if _, err = managedBindingName(slug, groupName, rate, 1, site.Platform); err != nil {
		naming.Warning = err.Error() + "，请补齐后同步命名"
		return nil
	}
	rows, err := m.reserveBindingNames(ctx, uid, aid, []bindingNameRow{{ConnectionID: conn.ID, GroupID: groupID, Slug: slug}})
	if err != nil {
		return err
	}
	row := rows[0]
	accounts, err := m.platform.ListSub2APIAdminAccounts(session)
	if err != nil {
		return err
	}
	for attempts := 0; attempts < 10; attempts++ {
		name, err := managedBindingName(row.Slug, groupName, rate, row.Number, site.Platform)
		if err != nil {
			naming.Warning = err.Error()
			return nil
		}
		if !nameCollision(keys, accounts, name, "", conn.AdminAccountID) {
			naming.Name, naming.ConnectionID, naming.GroupID = name, conn.ID, groupID
			return nil
		}
		row.Number, err = m.skipOccupiedOrdinal(ctx, uid, aid, row)
		if err != nil {
			naming.Warning = err.Error()
			return nil
		}
	}
	naming.Warning = "规范名称被占用，请核对同名对象后同步命名"
	return nil
}

func (m *KeyManagement) renameRotatedAccount(ctx context.Context, session upstream.Session, conns []RealConnection, account upstream.Sub2APIAdminAccountStatus, naming rotationNaming) error {
	if naming.ConnectionID == "" {
		return nil
	}
	if len(conns) != 1 || conns[0].ID != naming.ConnectionID || !slices.Equal(conns[0].OwnGroupIDs, []string{naming.GroupID}) || !slices.Equal(bindingGroupIDs(account.GroupIDs), []string{naming.GroupID}) {
		return fmt.Errorf("轮换期间自有账号分组已变化，请核对绑定")
	}
	if account.Name == naming.Name {
		return nil
	}
	if err := m.platform.UpdateForwardingAccountIdentity(ctx, session, conns[0].AdminAccountID, naming.Name, nil); err != nil {
		return err
	}
	detail, err := m.platform.FetchForwardingAccountDetail(ctx, session, conns[0].AdminAccountID)
	if err != nil || detail.Name != naming.Name {
		return fmt.Errorf("自有账号名称回读不一致")
	}
	return nil
}

func (m *KeyManagement) finishManagedRotation(ctx context.Context, uid, aid string, site *upstream.Site, conns []RealConnection, r rotationState) error {
	if err := m.alias(ctx, uid, aid, site.ID, r.OldID, r.Naming.PreviousName); err != nil {
		return err
	}
	if err := m.alias(ctx, uid, aid, site.ID, r.NewID, keyString(r.Config["name"])); err != nil {
		return err
	}
	if err := m.platform.RenameManagedKey(ctx, *site.Session, r.NewID, r.Naming.Name); err != nil {
		return err
	}
	if _, err := m.db.Exec(ctx, "INSERT INTO managed_key_lineage(user_id,workspace_id,site_id,previous_id,successor_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", uid, aid, site.ID, r.OldID, r.NewID); err != nil {
		return err
	}
	if r.Naming.ConnectionID != "" {
		return m.recordBindingName(ctx, conns[0], bindingTarget{GroupID: r.Naming.GroupID, Name: r.Naming.Name})
	}
	return nil
}

func rotationMessage(r rotationState) string {
	if r.Naming.Warning != "" {
		return "Key 已轮换，命名待处理。" + r.Naming.Warning
	}
	if r.Naming.ConnectionID != "" {
		return "Key 已轮换，两端名称已同步"
	}
	return "Key 已轮换，旧 Key 已撤销"
}

func rotationResult(r rotationState) jobs.Outcome {
	return jobs.Outcome{Value: map[string]any{"message": rotationMessage(r), "oldKeyId": r.OldID, "newKeyId": r.NewID, "name": r.Naming.Name, "namingWarning": r.Naming.Warning}}
}
