package admin_accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

// EnsureSchema 负责 admin_accounts 表和 users 表的工作区字段。
// 各业务模块的 admin_account_id 列由各自的 EnsureSchema 负责添加，
// 此处仅管理 admin_accounts 表本身、users.current_admin_account_id，
// 以及对旧数据的 legacy workspace 创建和归属分配。
//
// 调用前提：所有业务模块的 EnsureSchema 必须先执行完毕，
// 以保证业务表和 admin_account_id 列已存在，legacy 迁移 SQL 不会报错。
func (r *Repository) EnsureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_accounts (id text PRIMARY KEY, user_id text NOT NULL, platform text NOT NULL, base_url text NOT NULL, identity text NOT NULL, display_name text NOT NULL, auth_method text NOT NULL, last_used_at timestamptz NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE (user_id, platform, base_url, identity))`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS current_admin_account_id text NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_admin_accounts_user_updated ON admin_accounts (user_id, updated_at DESC, id ASC)`,
	}
	for _, statement := range statements {
		if _, err := r.db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	if err := r.createLegacyAccounts(ctx); err != nil {
		return err
	}
	return r.assignLegacyRows(ctx)
}

// createLegacyAccounts 为已有业务数据但尚无 admin account 的用户创建 legacy workspace。
func (r *Repository) createLegacyAccounts(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		WITH scoped_users AS (
			SELECT DISTINCT user_id FROM upstream_sites WHERE user_id <> ''
			UNION SELECT DISTINCT user_id FROM group_rate_snapshots WHERE user_id <> ''
			UNION SELECT DISTINCT user_id FROM strategy_settings WHERE user_id <> ''
			UNION SELECT DISTINCT user_id FROM notification_channel_settings WHERE user_id <> ''
			UNION SELECT DISTINCT user_id FROM my_site_states WHERE user_id <> ''
			UNION SELECT DISTINCT user_id FROM real_connections WHERE user_id <> ''
			UNION SELECT DISTINCT user_id FROM dashboard_daily_stats WHERE user_id <> ''
			UNION SELECT DISTINCT user_id FROM dashboard_balance_filter WHERE user_id <> ''
		), legacy AS (
			SELECT users.id AS user_id, 'adminacct_' || encode(sha256(convert_to(users.id || '|legacy||legacy', 'UTF8')), 'hex') AS account_id
			FROM users JOIN scoped_users ON scoped_users.user_id = users.id
		)
		INSERT INTO admin_accounts (id, user_id, platform, base_url, identity, display_name, auth_method, last_used_at, created_at, updated_at)
		SELECT account_id, user_id, 'legacy', '', 'legacy', 'Legacy workspace', 'legacy', now(), now(), now() FROM legacy
		ON CONFLICT (user_id, platform, base_url, identity) DO UPDATE SET updated_at = EXCLUDED.updated_at
	`)
	return err
}

// assignLegacyRows 将 admin_account_id 为空的旧业务行归属到用户的第一个工作区（legacy workspace）。
// 不删除旧数据，只补值。
//
// 关键设计：旧数据始终归属到 platform='legacy' 的工作区（即第一个工作区），
// 而非 users.current_admin_account_id。这保证即使用户已有多个 workspace 并
// 切换到了非 legacy 的工作区，历史数据也不会散落到其他 workspace。
func (r *Repository) assignLegacyRows(ctx context.Context) error {
	statements := []string{
		// 为尚无当前 workspace 的用户设置 legacy account 为默认。
		`UPDATE users SET current_admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE users.id = legacy.user_id AND users.current_admin_account_id = '' AND legacy.platform = 'legacy'`,
		// 各业务表旧行归属到用户的 legacy workspace（第一个工作区）。
		`UPDATE upstream_sites SET admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE upstream_sites.user_id = legacy.user_id AND upstream_sites.admin_account_id = '' AND legacy.platform = 'legacy'`,
		`UPDATE group_rate_snapshots SET admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE group_rate_snapshots.user_id = legacy.user_id AND group_rate_snapshots.admin_account_id = '' AND legacy.platform = 'legacy'`,
		`UPDATE strategy_settings SET admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE strategy_settings.user_id = legacy.user_id AND strategy_settings.admin_account_id = '' AND legacy.platform = 'legacy'`,
		`UPDATE notification_channel_settings SET admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE notification_channel_settings.user_id = legacy.user_id AND notification_channel_settings.admin_account_id = '' AND legacy.platform = 'legacy'`,
		`UPDATE my_site_states SET admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE my_site_states.user_id = legacy.user_id AND my_site_states.admin_account_id = '' AND legacy.platform = 'legacy'`,
		`UPDATE real_connections SET workspace_admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE real_connections.user_id = legacy.user_id AND real_connections.workspace_admin_account_id = '' AND legacy.platform = 'legacy'`,
		// dashboard 指标表旧行归属到用户的 legacy workspace。
		`UPDATE dashboard_daily_stats SET admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE dashboard_daily_stats.user_id = legacy.user_id AND dashboard_daily_stats.admin_account_id = '' AND legacy.platform = 'legacy'`,
		`UPDATE dashboard_balance_filter SET admin_account_id = legacy.id FROM admin_accounts AS legacy WHERE dashboard_balance_filter.user_id = legacy.user_id AND dashboard_balance_filter.admin_account_id = '' AND legacy.platform = 'legacy'`,
	}
	for _, statement := range statements {
		if _, err := r.db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) List(ctx context.Context, userID string) ([]Account, error) {
	rows, err := r.db.Query(ctx, `SELECT a.id, a.user_id, a.platform, a.base_url, a.identity, a.display_name, a.auth_method, a.id = users.current_admin_account_id AS current, a.last_used_at, a.created_at, a.updated_at FROM admin_accounts AS a JOIN users ON users.id = a.user_id WHERE a.user_id = $1 ORDER BY current DESC, a.last_used_at DESC NULLS LAST, a.updated_at DESC, a.id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]Account, 0)
	for rows.Next() {
		var account Account
		if err := rows.Scan(&account.ID, &account.UserID, &account.Platform, &account.BaseURL, &account.Identity, &account.DisplayName, &account.AuthMethod, &account.Current, &account.LastUsedAt, &account.CreatedAt, &account.UpdatedAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (r *Repository) Current(ctx context.Context, userID string) (*Account, error) {
	row := r.db.QueryRow(ctx, `SELECT a.id, a.user_id, a.platform, a.base_url, a.identity, a.display_name, a.auth_method, true AS current, a.last_used_at, a.created_at, a.updated_at FROM users JOIN admin_accounts AS a ON a.id = users.current_admin_account_id AND a.user_id = users.id WHERE users.id = $1`, userID)
	var account Account
	if err := row.Scan(&account.ID, &account.UserID, &account.Platform, &account.BaseURL, &account.Identity, &account.DisplayName, &account.AuthMethod, &account.Current, &account.LastUsedAt, &account.CreatedAt, &account.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &account, nil
}

func (r *Repository) CurrentID(ctx context.Context, userID string) (string, error) {
	var id string
	err := r.db.QueryRow(ctx, `SELECT current_admin_account_id FROM users WHERE id = $1 AND current_admin_account_id <> ''`, userID).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (r *Repository) UpsertAndSwitch(ctx context.Context, userID string, input UpsertInput) (Account, error) {
	accountID := StableID(userID, input.Platform, input.BaseURL, input.Identity)
	if strings.TrimSpace(input.DisplayName) == "" {
		input.DisplayName = input.Identity
	}
	now := time.Now()
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO admin_accounts (id, user_id, platform, base_url, identity, display_name, auth_method, last_used_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $8) ON CONFLICT (user_id, platform, base_url, identity) DO UPDATE SET display_name = EXCLUDED.display_name, auth_method = EXCLUDED.auth_method, last_used_at = EXCLUDED.last_used_at, updated_at = EXCLUDED.updated_at`, accountID, userID, input.Platform, input.BaseURL, input.Identity, input.DisplayName, input.AuthMethod, now)
	if err != nil {
		return Account{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET current_admin_account_id = $2 WHERE id = $1`, userID, accountID); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Account{}, err
	}
	account, err := r.Current(ctx, userID)
	if err != nil {
		return Account{}, err
	}
	return *account, nil
}

func (r *Repository) Switch(ctx context.Context, userID string, accountID string) (*Account, error) {
	result, err := r.db.Exec(ctx, `UPDATE users SET current_admin_account_id = $2 WHERE id = $1 AND EXISTS (SELECT 1 FROM admin_accounts WHERE id = $2 AND user_id = $1)`, userID, accountID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, nil
	}
	_, _ = r.db.Exec(ctx, `UPDATE admin_accounts SET last_used_at = now(), updated_at = now() WHERE id = $1 AND user_id = $2`, accountID, userID)
	return r.Current(ctx, userID)
}

func (r *Repository) Update(ctx context.Context, userID string, accountID string, displayName string) (*Account, error) {
	result, err := r.db.Exec(ctx, `UPDATE admin_accounts SET display_name = $3, updated_at = now() WHERE id = $1 AND user_id = $2`, accountID, userID, strings.TrimSpace(displayName))
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, nil
	}
	accounts, err := r.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].ID == accountID {
			return &accounts[i], nil
		}
	}
	return nil, nil
}

func StableID(userID, platform, baseURL, identity string) string {
	parts := strings.Join([]string{strings.TrimSpace(userID), strings.TrimSpace(platform), strings.TrimSpace(baseURL), strings.TrimSpace(identity)}, "|")
	sum := sha256.Sum256([]byte(parts))
	return "adminacct_" + hex.EncodeToString(sum[:])
}
