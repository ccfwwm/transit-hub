package my_sites

import (
	"context"
	"fmt"
	"strings"

	"transithub/backend/internal/modules/upstream"
)

type connectionAccountUpdater interface {
	UpdateRealConnectionAccount(context.Context, RealConnection, string) error
}

// RepairRealConnectionAccount only recreates an explicitly requested missing
// destination. The existing upstream key and group scope remain unchanged.
func (s *Service) RepairRealConnectionAccount(ctx context.Context, userID, connectionID string) (string, error) {
	s.repairMu.Lock()
	defer s.repairMu.Unlock()
	workspace, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return "", err
	}
	updater, ok := s.connRepository.(connectionAccountUpdater)
	if !ok {
		return "", requestError(ErrorRequest)
	}
	conn, err := s.connRepository.GetRealConnection(ctx, connectionID, userID, workspace)
	if err != nil {
		return "", err
	}
	if conn == nil || conn.UserID != userID || conn.WorkspaceAdminAccountID != workspace {
		return "", requestError(ErrorRequest)
	}
	state, err := s.authenticatedState(ctx, userID, workspace)
	if err != nil {
		return "", err
	}
	if state.Session.Platform != upstream.PlatformSub2API {
		return "", requestError(ErrorRequest)
	}
	accounts, err := s.platformService.ListSub2APIAdminAccounts(state.Session)
	if err != nil {
		return "", err
	}
	for _, account := range accounts {
		if account.ID == conn.AdminAccountID {
			return account.ID, nil
		}
	}
	site, err := s.upstreamLookup.GetSite(ctx, conn.UpstreamSiteID)
	if err != nil {
		return "", err
	}
	if site == nil || site.UserID != userID || site.AdminAccountID != workspace || strings.TrimSpace(site.BaseURL) == "" || strings.TrimSpace(conn.UpstreamKey) == "" || strings.TrimSpace(conn.GroupType) == "" {
		return "", requestError(ErrorRequest)
	}
	groups, err := s.platformService.FetchAdminAllGroups(state.Session)
	if err != nil {
		return "", err
	}
	available := map[string]bool{}
	for _, group := range groups {
		available[group.ID] = true
	}
	if len(conn.OwnGroupIDs) == 0 {
		return "", requestError(ErrorInvalidGroup)
	}
	for _, id := range conn.OwnGroupIDs {
		if !available[id] {
			return "", requestError(ErrorInvalidGroup)
		}
	}
	groupIDs, err := stringsToInts(conn.OwnGroupIDs)
	if err != nil {
		return "", requestError(ErrorInvalidGroup)
	}
	name := conn.AdminAccountName
	if strings.TrimSpace(name) == "" {
		name = site.Name + " - " + conn.UpstreamGroupName
	}
	payload := buildAccountPayload(conn.GroupType, site.BaseURL, conn.UpstreamKey, groupIDs, name)
	// Sub2API creation does not accept schedulable. Create expired, explicitly
	// disable scheduling, then remove the temporary expiration.
	payload["expires_at"] = 1
	payload["auto_pause_on_expired"] = true
	accountID, err := s.platformService.CreateSub2APIAdminAccount(state.Session, payload)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(accountID) == "" {
		return "", requestError(ErrorRequest)
	}
	rollback := func(cause error) (string, error) {
		if cleanupErr := s.platformService.DeleteSub2APIAdminAccount(state.Session, accountID); cleanupErr != nil {
			return "", fmt.Errorf("repair failed; disabled/expired account %s needs cleanup: %w", accountID, cause)
		}
		return "", cause
	}
	if err := s.platformService.SetSub2APIAdminAccountSchedulable(state.Session, accountID, false); err != nil {
		return rollback(err)
	}
	if err := s.platformService.ClearSub2APIAdminAccountExpiration(state.Session, accountID); err != nil {
		return rollback(err)
	}
	if err := updater.UpdateRealConnectionAccount(ctx, *conn, accountID); err != nil {
		return rollback(err)
	}
	return accountID, nil
}

func (r *Repository) UpdateRealConnectionAccount(ctx context.Context, conn RealConnection, accountID string) error {
	result, err := r.db.Exec(ctx, `UPDATE real_connections SET admin_account_id=$5
 WHERE id=$1 AND user_id=$2 AND workspace_admin_account_id=$3 AND admin_account_id=$4`,
		conn.ID, conn.UserID, conn.WorkspaceAdminAccountID, conn.AdminAccountID, accountID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return requestError(ErrorRequest)
	}
	return nil
}
