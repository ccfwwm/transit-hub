package my_sites

import (
	"context"
	"fmt"

	"transithub/backend/internal/modules/upstream"
)

// Inventory membership and past policy writes do not authorize a security write.
// Require a current binding and the full Key actually used by our Sub2API account.
// During recovery an account may already use the replacement before the local
// binding transaction commits, so only that persisted rotation permits both Keys.
func (k *KeySecurity) validateBoundConsumers(ctx context.Context, uid, aid string, site upstream.Site, keyIDs, rotationSecrets []string) (bool, error) {
	conns, err := k.sites.connRepository.ListRealConnections(ctx, uid, aid)
	if err != nil {
		return false, err
	}
	selected := []RealConnection{}
	for _, c := range conns {
		if c.UpstreamSiteID != site.ID {
			continue
		}
		for _, id := range keyIDs {
			if id != "" && c.UpstreamKeyID == id {
				selected = append(selected, c)
				break
			}
		}
	}
	if len(selected) == 0 {
		return false, nil
	}
	upstreamSecrets := map[string]string{}
	if len(rotationSecrets) == 0 {
		for _, c := range selected {
			if _, ok := upstreamSecrets[c.UpstreamKeyID]; ok {
				continue
			}
			record, _, err := k.sites.platformService.ReadSecurityKey(ctx, *site.Session, c.UpstreamKeyID)
			if err != nil {
				return false, fmt.Errorf("无法核对上游完整 Key，未修改白名单")
			}
			secret, _ := record["key"].(string)
			if site.Platform == upstream.PlatformNewAPI {
				secret, err = k.sites.platformService.SecurityTokenKey(ctx, *site.Session, c.UpstreamKeyID)
				if err != nil {
					return false, fmt.Errorf("无法核对上游完整 Key，未修改白名单")
				}
			}
			upstreamSecrets[c.UpstreamKeyID] = secret
		}
	}
	session, err := k.sites.RequireSession(ctx, uid, aid)
	if err != nil || session.Platform != upstream.PlatformSub2API {
		return false, fmt.Errorf("无法核对自有 Sub2API，未修改上游 Key")
	}
	for _, c := range selected {
		if c.AdminAccountID == "" || c.UpstreamKey == "" {
			return false, nil
		}
		account, err := k.sites.platformService.FetchForwardingAccount(ctx, session, c.AdminAccountID)
		if err != nil {
			return false, fmt.Errorf("无法核对自有转发账号，未修改上游 Key")
		}
		if len(rotationSecrets) == 0 {
			if !upstream.SameMonitorKey(site.Platform, c.UpstreamKey, account.APIKey) || !upstream.SameMonitorKey(site.Platform, c.UpstreamKey, upstreamSecrets[c.UpstreamKeyID]) {
				return false, nil
			}
			continue
		}
		local, remote := false, false
		for _, secret := range rotationSecrets {
			if secret == "" {
				continue
			}
			local = local || upstream.SameMonitorKey(site.Platform, c.UpstreamKey, secret)
			remote = remote || upstream.SameMonitorKey(site.Platform, account.APIKey, secret)
		}
		if !local || !remote {
			return false, nil
		}
	}
	return true, nil
}
