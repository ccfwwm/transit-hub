package channel_monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"transithub/backend/internal/modules/upstream"
)

// Read models are published atomically. HTTP readers never contact an upstream,
// ensure rules, or calculate a priority plan.
func (r *Repository) ensureReadSchema(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
 CREATE TABLE IF NOT EXISTS channel_monitor_model_catalog(rule_id text PRIMARY KEY,user_id text NOT NULL,admin_account_id text NOT NULL,fingerprint text NOT NULL,models jsonb NOT NULL DEFAULT '[]',updated_at timestamptz NOT NULL DEFAULT now(),error text NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS channel_monitor_snapshots (
 user_id text NOT NULL, admin_account_id text NOT NULL, group_id text NOT NULL,
 version bigint NOT NULL, updated_at timestamptz NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY(user_id,admin_account_id,group_id));
 CREATE TABLE IF NOT EXISTS channel_monitor_views (
 user_id text NOT NULL, admin_account_id text NOT NULL, group_id text NOT NULL,
 rule_id text NOT NULL, ordinal integer NOT NULL, status text NOT NULL,
 enabled boolean NOT NULL, dispatch_paused boolean NOT NULL, search_text text NOT NULL,
 payload jsonb NOT NULL, PRIMARY KEY(user_id,admin_account_id,group_id,rule_id));
 CREATE INDEX IF NOT EXISTS idx_monitor_views_page ON channel_monitor_views(user_id,admin_account_id,group_id,ordinal,rule_id);
 CREATE INDEX IF NOT EXISTS idx_monitor_views_status ON channel_monitor_views(user_id,admin_account_id,group_id,status,ordinal);
 CREATE INDEX IF NOT EXISTS idx_monitor_results_retention ON channel_monitor_results(created_at);
 CREATE INDEX IF NOT EXISTS idx_monitor_results_model_history ON channel_monitor_results(rule_id,group_id,model,created_at DESC);
 CREATE INDEX IF NOT EXISTS idx_monitor_rate_results_retention ON channel_monitor_rate_results(created_at);
 CREATE TABLE IF NOT EXISTS channel_monitor_daily_rollups (
 user_id text NOT NULL,admin_account_id text NOT NULL,day date NOT NULL,
 total bigint NOT NULL,successful bigint NOT NULL,PRIMARY KEY(user_id,admin_account_id,day));`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS channel_monitor_priority_membership(user_id text NOT NULL,admin_account_id text NOT NULL,group_id text NOT NULL,fingerprint text NOT NULL,applied text NOT NULL DEFAULT '',updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(user_id,admin_account_id,group_id))`)
	return err
}

func (r *Repository) publishSnapshot(ctx context.Context, uid, aid string, base SummaryResponse) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, uid+"|"+aid); err != nil {
		return err
	}
	now := time.Now().UTC()
	version := now.UnixMicro()
	base.SnapshotVersion, base.UpdatedAt = version, &now
	groupIDs := []string{""}
	for _, g := range base.Groups {
		if g.GroupID != "" {
			groupIDs = append(groupIDs, g.GroupID)
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM channel_monitor_views WHERE user_id=$1 AND admin_account_id=$2`, uid, aid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM channel_monitor_snapshots WHERE user_id=$1 AND admin_account_id=$2`, uid, aid); err != nil {
		return err
	}
	for _, gid := range groupIDs {
		view := base
		view.Channels = []ChannelStatus{}
		view.Stats = SummaryStats{}
		for _, original := range base.Channels {
			if gid != "" && !containsString(original.OwnGroupIDs, gid) {
				continue
			}
			row := original
			if row.DispatchState == "" {
				row.DispatchState = dispatchState(row)
			}
			if gid != "" {
				for _, t := range row.GroupStates {
					if t.GroupID == gid {
						applyTargetToChannel(&row, t)
						break
					}
				}
			}
			if row.ManualPaused {
				row.Status = StatusManualPaused
			}
			if selected, ok := row.ModelSummaries[row.EffectiveTestModelID]; ok {
				row.ModelSummary = &selected
			}
			row.RecentResults = []Result{}
			view.Channels = append(view.Channels, row)
			applyStats(&view.Stats, row)
		}
		rows := make([][]any, 0, len(view.Channels))
		for i, row := range view.Channels {
			payload, e := json.Marshal(row)
			if e != nil {
				return e
			}
			search := strings.ToLower(strings.Join([]string{row.SiteName, row.UpstreamGroupName, row.AdminAccountName, row.AdminAccountID, strings.Join(row.OwnGroups, " ")}, " "))
			rows = append(rows, []any{uid, aid, gid, row.RuleID, i, row.Status, row.Enabled, row.Schedulable != nil && !*row.Schedulable, search, payload})
		}
		if len(rows) > 0 {
			if _, err = tx.CopyFrom(ctx, pgx.Identifier{"channel_monitor_views"}, []string{"user_id", "admin_account_id", "group_id", "rule_id", "ordinal", "status", "enabled", "dispatch_paused", "search_text", "payload"}, pgx.CopyFromRows(rows)); err != nil {
				return err
			}
		}
		view.Channels = []ChannelStatus{}
		view.RateRule.Rows = []RatePlanRow{}
		if view.RateRule.LastResult != nil {
			last := *view.RateRule.LastResult
			last.Rows = []RatePlanRow{}
			view.RateRule.LastResult = &last
		}
		payload, e := json.Marshal(view)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, `INSERT INTO channel_monitor_snapshots VALUES($1,$2,$3,$4,$5,$6)`, uid, aid, gid, version, now, payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) readSnapshot(ctx context.Context, uid, aid, gid string) (SummaryResponse, error) {
	v := SummaryResponse{Channels: []ChannelStatus{}, Groups: []GroupSummary{}, TestModelConfig: DefaultTestModelConfig(uid, aid), RateRule: RateRuleView{Rule: DefaultRateRule(uid, aid), Rows: []RatePlanRow{}, PriorityGroups: []PriorityGroupRule{}}}
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT payload FROM channel_monitor_snapshots WHERE user_id=$1 AND admin_account_id=$2 AND group_id=$3`, uid, aid, gid).Scan(&raw)
	if err == pgx.ErrNoRows {
		v.Stale = true
		return v, nil
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(raw, &v)
	v.Stale = v.UpdatedAt == nil || time.Since(*v.UpdatedAt) > 45*time.Second
	return v, err
}

// All filter predicates run in PostgreSQL before LIMIT. The stable ordinal is
// part of the published generation, so changing pages doesn't recalculate it.
func (r *Repository) readPage(ctx context.Context, uid, aid string, q ChannelPageQuery) (ChannelPage, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize != 50 && q.PageSize != 100 {
		q.PageSize = 25
	}
	res := ChannelPage{Channels: []ChannelStatus{}, RuleIDs: []string{}, Page: q.Page, PageSize: q.PageSize}
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT version,updated_at,payload FROM channel_monitor_snapshots WHERE user_id=$1 AND admin_account_id=$2 AND group_id=$3`, uid, aid, q.GroupID).Scan(&res.SnapshotVersion, &res.UpdatedAt, &raw)
	if err == pgx.ErrNoRows {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	args := []any{uid, aid, q.GroupID}
	where := `user_id=$1 AND admin_account_id=$2 AND group_id=$3`
	add := func(clause string, v any) { args = append(args, v); where += " AND " + fmt.Sprintf(clause, len(args)) }
	if q.RuleID != "" {
		add(`rule_id=$%d`, q.RuleID)
	}
	if q.SiteID != "" {
		add(`payload->>'siteId'=$%d`, q.SiteID)
	}
	if q.Search != "" {
		add(`strpos(search_text,$%d)>0`, strings.ToLower(strings.TrimSpace(q.Search)))
	}
	if q.Status == "monitor_paused" {
		where += " AND NOT enabled"
	} else if q.Status == "dispatch_paused" {
		where += " AND dispatch_paused"
	} else if q.Status != "" && q.Status != "all" {
		add(`status=$%d`, q.Status)
	}
	if q.Dispatch == "paused" {
		where += " AND dispatch_paused"
	} else if q.Dispatch == "monitor" {
		where += " AND NOT enabled"
	} else if q.Dispatch != "" && q.Dispatch != "all" {
		add(`payload->>'dispatchState'=$%d`, q.Dispatch)
	}
	if q.GroupStatus != "" && q.GroupStatus != "all" {
		var snap SummaryResponse
		if err = json.Unmarshal(raw, &snap); err != nil {
			return res, err
		}
		allowed := []string{}
		for _, g := range snap.Groups {
			if (q.GroupStatus == "available" && g.Available > 0) || (q.GroupStatus == "unavailable" && g.Available == 0) {
				allowed = append(allowed, g.GroupID)
			}
		}
		add(`EXISTS (SELECT 1 FROM jsonb_array_elements_text(payload->'ownGroupIds') g WHERE g=ANY($%d::text[]))`, allowed)
	}
	err = tx.QueryRow(ctx, `SELECT count(*),coalesce(array_agg(rule_id ORDER BY ordinal,rule_id),'{}'::text[]) FROM channel_monitor_views WHERE `+where, args...).Scan(&res.Total, &res.RuleIDs)
	if err != nil {
		return res, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := tx.Query(ctx, `SELECT payload FROM channel_monitor_views WHERE `+where+fmt.Sprintf(` ORDER BY ordinal,rule_id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var b []byte
		var ch ChannelStatus
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return res, err
		}
		if err = json.Unmarshal(b, &ch); err != nil {
			rows.Close()
			return res, err
		}
		res.Channels = append(res.Channels, ch)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return res, err
	}
	return res, tx.Commit(ctx)
}

func (s *Service) RefreshReadModel(ctx context.Context, uid, aid string) error {
	r, ok := s.store.(*Repository)
	if !ok {
		return nil
	}
	// A caller supplies the owning workspace, independent of the user's current
	// selection. Do not temporarily switch the persisted current workspace.
	ctx = context.WithValue(ctx, readWorkspaceKey{}, aid)
	base, err := s.summaryInGroup(ctx, uid, "", true)
	if err != nil {
		return err
	}
	for i := range base.Channels {
		base.Channels[i].DispatchState = dispatchState(base.Channels[i])
	}
	if err = s.applyStatusSource(ctx, uid, aid, &base); err != nil {
		return err
	}
	if s.keyPolicies != nil {
		policies := map[string]*upstream.GroupKeyPolicy{}
		for i := range base.Channels {
			row := &base.Channels[i]
			key := row.SiteID + "|" + row.UpstreamGroupID
			policy, known := policies[key]
			if !known {
				if value, e := s.keyPolicies.Group(ctx, uid, aid, row.SiteID, row.UpstreamGroupID); e == nil {
					policy = &value
				}
				policies[key] = policy
			}
			row.KeyPolicy = policy
			if enabled, e := s.keyPolicies.BindingWhitelistEnabled(ctx, uid, aid, row.ConnectionID); e == nil {
				row.KeyWhitelistEnabled = &enabled
			}
		}
	}
	if err = r.publishSnapshot(ctx, uid, aid, base); err != nil {
		return err
	}
	return s.reconcileMembershipPriority(ctx, uid, aid)
}

type readWorkspaceKey struct{}

func (s *Service) StartReadModels(ctx context.Context) {
	r, ok := s.store.(*Repository)
	if !ok {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		lastCleanup := time.Time{}
		for {
			rows, err := r.db.Query(ctx, `SELECT user_id,id FROM admin_accounts UNION SELECT DISTINCT user_id,workspace_admin_account_id FROM real_connections WHERE user_id<>'' AND workspace_admin_account_id<>''`)
			workspaces := [][2]string{}
			if err == nil {
				for rows.Next() {
					var pair [2]string
					if rows.Scan(&pair[0], &pair[1]) == nil {
						workspaces = append(workspaces, pair)
					}
				}
				rows.Close()
			}
			for _, pair := range workspaces {
				run, cancel := context.WithTimeout(ctx, 45*time.Second)
				if e := s.RefreshReadModel(run, pair[0], pair[1]); e != nil {
					log.Printf("[monitor-read] refresh failed workspace=%s: %v", pair[1], e)
				}
				cancel()
			}
			if time.Since(lastCleanup) > time.Hour {
				s.pruneHistory(ctx)
				lastCleanup = time.Now()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Service) pruneHistory(ctx context.Context) {
	r, ok := s.store.(*Repository)
	if !ok {
		return
	}
	// Rollups are idempotently recomputed before raw samples are removed. Bounded
	// deletes avoid the unbounded growth and long locks of historical log tables.
	_, err := r.db.Exec(ctx, `INSERT INTO channel_monitor_daily_rollups SELECT rules.user_id,rules.admin_account_id,res.created_at::date,count(*),count(*) FILTER(WHERE res.success) FROM channel_monitor_results res JOIN channel_monitor_rules rules ON rules.id=res.rule_id WHERE res.created_at>=CURRENT_DATE-8 AND res.created_at<CURRENT_DATE GROUP BY 1,2,3 ON CONFLICT(user_id,admin_account_id,day) DO UPDATE SET total=EXCLUDED.total,successful=EXCLUDED.successful`)
	if err != nil {
		return
	}
	for _, table := range []string{"channel_monitor_results", "channel_monitor_rate_results"} {
		for i := 0; i < 20; i++ {
			result, e := r.db.Exec(ctx, `DELETE FROM `+table+` WHERE id IN(SELECT id FROM `+table+` WHERE created_at<now()-interval '7 days' ORDER BY created_at LIMIT 5000)`)
			if e != nil || result.RowsAffected() < 5000 {
				break
			}
		}
	}
	_, _ = r.db.Exec(ctx, `DELETE FROM channel_monitor_daily_rollups WHERE day<CURRENT_DATE-90`)
	_, _ = r.db.Exec(ctx, `DELETE FROM operation_jobs j WHERE created_at<now()-interval '7 days' AND NOT EXISTS(SELECT 1 FROM operation_items i WHERE i.job_id=j.id AND i.status IN('queued','waiting','running'))`)
}
