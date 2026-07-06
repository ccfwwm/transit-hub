package channel_monitor

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"transithub/backend/internal/modules/my_sites"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureSchema(ctx context.Context) error {
	if _, err := r.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS channel_monitor_rules (
			id text PRIMARY KEY,
			user_id text NOT NULL,
			admin_account_id text NOT NULL,
			connection_id text NOT NULL,
			enabled boolean NOT NULL DEFAULT true,
			check_interval_minutes integer NOT NULL DEFAULT 10,
			failure_threshold integer NOT NULL DEFAULT 3,
			balance_threshold double precision NOT NULL DEFAULT 1,
			desired_schedulable boolean NULL,
			manual_paused boolean NOT NULL DEFAULT false,
			consecutive_failures integer NOT NULL DEFAULT 0,
			last_status text NOT NULL DEFAULT 'unknown',
			last_message text NOT NULL DEFAULT '',
			last_latency_ms integer NULL,
			last_checked_at timestamptz NULL,
			next_check_at timestamptz NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
		ALTER TABLE channel_monitor_rules ADD COLUMN IF NOT EXISTS desired_schedulable boolean NULL
	`); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_monitor_rules_connection
		ON channel_monitor_rules (user_id, admin_account_id, connection_id)
	`); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_channel_monitor_rules_due
		ON channel_monitor_rules (enabled, next_check_at)
	`); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS channel_monitor_results (
			id text PRIMARY KEY,
			rule_id text NOT NULL,
			connection_id text NOT NULL,
			status text NOT NULL,
			success boolean NOT NULL,
			message text NOT NULL DEFAULT '',
			latency_ms integer NULL,
			model text NOT NULL DEFAULT '',
			action text NOT NULL DEFAULT '',
			started_at timestamptz NOT NULL,
			finished_at timestamptz NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_channel_monitor_results_rule_created
		ON channel_monitor_results (rule_id, created_at DESC)
	`)
	return err
}

func (r *Repository) EnsureRulesForExistingConnections(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO channel_monitor_rules (
			id, user_id, admin_account_id, connection_id, enabled, check_interval_minutes,
			failure_threshold, balance_threshold, manual_paused, consecutive_failures,
			last_status, next_check_at, created_at, updated_at
		)
		SELECT
			rc.id, rc.user_id, rc.workspace_admin_account_id, rc.id, true, $1,
			$2, $3, false, 0, $4, now(), now(), now()
		FROM real_connections AS rc
		WHERE rc.user_id <> '' AND rc.workspace_admin_account_id <> ''
		ON CONFLICT (id) DO NOTHING
	`, DefaultCheckIntervalMinutes, DefaultFailureThreshold, DefaultBalanceThreshold, StatusUnknown)
	return err
}

func (r *Repository) EnsureRuleForConnection(ctx context.Context, userID, adminAccountID string, conn my_sites.RealConnection) (Rule, error) {
	rule := DefaultRule(userID, adminAccountID, conn.ID)
	_, err := r.db.Exec(ctx, `
		INSERT INTO channel_monitor_rules (
			id, user_id, admin_account_id, connection_id, enabled, check_interval_minutes,
			failure_threshold, balance_threshold, manual_paused, consecutive_failures,
			last_status, next_check_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, true, $5, $6, $7, false, 0, $8, $9, $10, $10)
		ON CONFLICT (id) DO NOTHING
	`, rule.ID, rule.UserID, rule.AdminAccountID, rule.ConnectionID, rule.CheckIntervalMinutes, rule.FailureThreshold, rule.BalanceThreshold, rule.LastStatus, rule.NextCheckAt, rule.CreatedAt)
	if err != nil {
		return Rule{}, err
	}
	saved, err := r.GetRule(ctx, rule.ID)
	if err != nil {
		return Rule{}, err
	}
	if saved == nil {
		return rule, nil
	}
	return *saved, nil
}

func (r *Repository) ListRulesForWorkspace(ctx context.Context, userID, adminAccountID string) ([]Rule, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, admin_account_id, connection_id, enabled, check_interval_minutes,
			failure_threshold, balance_threshold, desired_schedulable, manual_paused, consecutive_failures,
			last_status, last_message, last_latency_ms, last_checked_at, next_check_at, created_at, updated_at
		FROM channel_monitor_rules
		WHERE user_id = $1 AND admin_account_id = $2
		ORDER BY created_at ASC
	`, userID, adminAccountID)
	if err != nil {
		return nil, err
	}
	return scanRules(rows)
}

func (r *Repository) GetRule(ctx context.Context, id string) (*Rule, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, admin_account_id, connection_id, enabled, check_interval_minutes,
			failure_threshold, balance_threshold, desired_schedulable, manual_paused, consecutive_failures,
			last_status, last_message, last_latency_ms, last_checked_at, next_check_at, created_at, updated_at
		FROM channel_monitor_rules
		WHERE id = $1
	`, id)
	if err != nil {
		return nil, err
	}
	rules, err := scanRules(rows)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, nil
	}
	return &rules[0], nil
}

func (r *Repository) UpdateRule(ctx context.Context, rule Rule) error {
	_, err := r.db.Exec(ctx, `
		UPDATE channel_monitor_rules
		SET enabled = $2,
			check_interval_minutes = $3,
			failure_threshold = $4,
			balance_threshold = $5,
			desired_schedulable = $6,
			manual_paused = $7,
			consecutive_failures = $8,
			last_status = $9,
			last_message = $10,
			last_latency_ms = $11,
			last_checked_at = $12,
			next_check_at = $13,
			updated_at = now()
		WHERE id = $1
	`, rule.ID, rule.Enabled, rule.CheckIntervalMinutes, rule.FailureThreshold, rule.BalanceThreshold,
		rule.DesiredSchedulable, rule.ManualPaused, rule.ConsecutiveFailures, rule.LastStatus, rule.LastMessage, rule.LastLatencyMS,
		rule.LastCheckedAt, rule.NextCheckAt)
	return err
}

func (r *Repository) AddResult(ctx context.Context, result Result) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO channel_monitor_results (
			id, rule_id, connection_id, status, success, message, latency_ms, model, action, started_at, finished_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, result.ID, result.RuleID, result.ConnectionID, result.Status, result.Success, result.Message, result.LatencyMS, result.Model, result.Action, result.StartedAt, result.FinishedAt, result.CreatedAt)
	return err
}

func (r *Repository) ListRecentResults(ctx context.Context, ruleID string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, rule_id, connection_id, status, success, message, latency_ms, model, action, started_at, finished_at, created_at
		FROM channel_monitor_results
		WHERE rule_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, ruleID, limit)
	if err != nil {
		return nil, err
	}
	return scanResults(rows)
}

func (r *Repository) ListDueRules(ctx context.Context, limit int) ([]Rule, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, admin_account_id, connection_id, enabled, check_interval_minutes,
			failure_threshold, balance_threshold, desired_schedulable, manual_paused, consecutive_failures,
			last_status, last_message, last_latency_ms, last_checked_at, next_check_at, created_at, updated_at
		FROM channel_monitor_rules
		WHERE enabled = true AND (next_check_at IS NULL OR next_check_at <= now())
		ORDER BY next_check_at ASC NULLS FIRST, created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	return scanRules(rows)
}

func scanRules(rows pgx.Rows) ([]Rule, error) {
	defer rows.Close()
	var rules []Rule
	for rows.Next() {
		var rule Rule
		if err := rows.Scan(
			&rule.ID, &rule.UserID, &rule.AdminAccountID, &rule.ConnectionID, &rule.Enabled,
			&rule.CheckIntervalMinutes, &rule.FailureThreshold, &rule.BalanceThreshold, &rule.DesiredSchedulable,
			&rule.ManualPaused, &rule.ConsecutiveFailures, &rule.LastStatus, &rule.LastMessage,
			&rule.LastLatencyMS, &rule.LastCheckedAt, &rule.NextCheckAt, &rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func scanResults(rows pgx.Rows) ([]Result, error) {
	defer rows.Close()
	var results []Result
	for rows.Next() {
		var result Result
		if err := rows.Scan(
			&result.ID, &result.RuleID, &result.ConnectionID, &result.Status, &result.Success,
			&result.Message, &result.LatencyMS, &result.Model, &result.Action, &result.StartedAt,
			&result.FinishedAt, &result.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

var _ = time.Second
