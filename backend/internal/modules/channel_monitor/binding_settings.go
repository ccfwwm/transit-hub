package channel_monitor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

func (s *Service) SetPrimaryModel(ctx context.Context, c my_sites.RealConnection, model string) error {
	return s.ConfigureBinding(ctx, c, &model, nil)
}
func (s *Service) ConfigureBinding(ctx context.Context, c my_sites.RealConnection, model *string, allow *bool) error {
	rule, e := s.store.EnsureRuleForConnection(ctx, c.UserID, c.WorkspaceAdminAccountID, c)
	if e != nil {
		return e
	}
	var normalized *string
	if model != nil {
		v := strings.TrimSpace(*model)
		normalized = &v
	}
	// Update only requested fields so rule/scheduler/manual settings remain intact.
	if repo, ok := s.store.(*Repository); ok {
		_, e = repo.db.Exec(ctx, `UPDATE channel_monitor_rules SET test_model_id=coalesce($4::text,test_model_id),allow_when_upstream_rate_gte_own=coalesce($5::boolean,allow_when_upstream_rate_gte_own),next_check_at=now(),updated_at=now() WHERE id=$1 AND user_id=$2 AND admin_account_id=$3`, rule.ID, c.UserID, c.WorkspaceAdminAccountID, normalized, allow)
		return e
	}
	if normalized != nil {
		rule.TestModelID = *normalized
	}
	if allow != nil {
		rule.AllowWhenUpstreamRateGteOwn = *allow
		if e = s.store.UpdateRuleRateSettings(ctx, rule.ID, UpdateRuleRequest{AllowWhenUpstreamRateGteOwn: allow}); e != nil {
			return e
		}
	}
	rule.UpdatedAt = time.Now()
	return s.store.UpdateRule(ctx, rule)
}
func (s *Service) PopulatePrimaryModels(ctx context.Context, uid, aid string, conns []my_sites.RealConnection) error {
	rules, e := s.store.ListRulesForWorkspace(ctx, uid, aid)
	if e != nil {
		return e
	}
	byConn := map[string]Rule{}
	for _, r := range rules {
		byConn[r.ConnectionID] = r
	}
	for i := range conns {
		conns[i].TestModelID = byConn[conns[i].ID].TestModelID
		conns[i].AllowWhenUpstreamRateGteOwn = byConn[conns[i].ID].AllowWhenUpstreamRateGteOwn
	}
	return nil
}
func (s *Service) BindingModelCatalog(ctx context.Context, site upstream.Site, group string) ([]string, error) {
	repo, ok := s.store.(*Repository)
	if !ok {
		return nil, fmt.Errorf("模型目录不可用")
	}
	rows, e := repo.db.Query(ctx, `SELECT DISTINCT model FROM upstream_model_health WHERE site_id=$1 AND user_id=$2 AND admin_account_id=$3 AND group_id=$4 AND model<>'' AND coalesce(payload->>'scope','') NOT IN('infrastructure','channel_reference') ORDER BY model`, site.ID, site.UserID, site.AdminAccountID, group)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		if !strings.HasPrefix(id, "__") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, rows.Err()
}
