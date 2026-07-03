package settings

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureSchema(ctx context.Context) error {
	if _, err := r.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS notification_channel_settings (
			user_id text NOT NULL,
			admin_account_id text NOT NULL DEFAULT '',
			settings jsonb NOT NULL DEFAULT '{}'::jsonb,
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS strategy_settings (
			user_id text NOT NULL,
			admin_account_id text NOT NULL DEFAULT '',
			settings jsonb NOT NULL DEFAULT '{}'::jsonb,
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}
	statements := []string{
		`ALTER TABLE notification_channel_settings ADD COLUMN IF NOT EXISTS admin_account_id text NOT NULL DEFAULT ''`,
		`ALTER TABLE strategy_settings ADD COLUMN IF NOT EXISTS admin_account_id text NOT NULL DEFAULT ''`,
		`ALTER TABLE notification_channel_settings DROP CONSTRAINT IF EXISTS notification_channel_settings_pkey`,
		`ALTER TABLE strategy_settings DROP CONSTRAINT IF EXISTS strategy_settings_pkey`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_channel_settings_workspace ON notification_channel_settings (user_id, admin_account_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_strategy_settings_workspace ON strategy_settings (user_id, admin_account_id)`,
	}
	for _, statement := range statements {
		if _, err := r.db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// GetFirstStrategy 返回任意一条策略设置（不指定用户）。
// 启动时用于初始化上游定时同步配置，在单管理员场景下等同于获取唯一的设置记录。
func (r *Repository) GetFirstStrategy(ctx context.Context) (StrategySettings, error) {
	settings := DefaultStrategySettings()
	row := r.db.QueryRow(ctx, `SELECT settings FROM strategy_settings LIMIT 1`)
	var settingsJSON []byte
	if err := row.Scan(&settingsJSON); err != nil {
		if err == pgx.ErrNoRows {
			return settings, nil
		}
		return settings, err
	}
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func (r *Repository) GetStrategy(ctx context.Context, userID string, adminAccountID string) (StrategySettings, error) {
	settings := DefaultStrategySettings()
	row := r.db.QueryRow(ctx, `SELECT settings FROM strategy_settings WHERE user_id = $1 AND admin_account_id = $2`, userID, adminAccountID)
	var settingsJSON []byte
	if err := row.Scan(&settingsJSON); err != nil {
		if err == pgx.ErrNoRows {
			return settings, nil
		}
		return settings, err
	}
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func (r *Repository) SaveStrategy(ctx context.Context, userID string, adminAccountID string, settings StrategySettings) error {
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO strategy_settings (user_id, admin_account_id, settings, updated_at)
		VALUES ($1, $2, $3::jsonb, now())
		ON CONFLICT (user_id, admin_account_id) DO UPDATE SET
			settings = EXCLUDED.settings,
			updated_at = EXCLUDED.updated_at
	`, userID, adminAccountID, string(settingsJSON))
	return err
}

func (r *Repository) GetNotificationChannels(ctx context.Context, userID string, adminAccountID string) (NotificationChannelSettings, error) {
	settings := DefaultNotificationChannelSettings()
	row := r.db.QueryRow(ctx, `SELECT settings FROM notification_channel_settings WHERE user_id = $1 AND admin_account_id = $2`, userID, adminAccountID)
	var settingsJSON []byte
	if err := row.Scan(&settingsJSON); err != nil {
		if err == pgx.ErrNoRows {
			return settings, nil
		}
		return settings, err
	}
	if err := unmarshalNotificationChannelSettings(settingsJSON, &settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func unmarshalNotificationChannelSettings(data []byte, settings *NotificationChannelSettings) error {
	if err := json.Unmarshal(data, settings); err == nil {
		return nil
	}
	var legacy struct {
		Dingtalk DingtalkChannelSettings `json:"dingtalk"`
		Feishu   WebhookChannelSettings  `json:"feishu"`
		Telegram TelegramChannelSettings `json:"telegram"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&legacy); err != nil {
		return err
	}
	if legacy.Dingtalk.Enabled || legacy.Dingtalk.Webhook != "" || legacy.Dingtalk.Secret != "" {
		settings.Dingtalk = []DingtalkChannelSettings{legacy.Dingtalk}
	}
	if legacy.Feishu.Enabled || legacy.Feishu.Webhook != "" || legacy.Feishu.Secret != "" {
		settings.Feishu = []WebhookChannelSettings{legacy.Feishu}
	}
	if legacy.Telegram.Enabled || legacy.Telegram.BotToken != "" || legacy.Telegram.ChatID != "" || legacy.Telegram.ProxyURL != "" {
		settings.Telegram = []TelegramChannelSettings{legacy.Telegram}
	}
	return nil
}

func (r *Repository) SaveNotificationChannels(ctx context.Context, userID string, adminAccountID string, settings NotificationChannelSettings) error {
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO notification_channel_settings (user_id, admin_account_id, settings, updated_at)
		VALUES ($1, $2, $3::jsonb, now())
		ON CONFLICT (user_id, admin_account_id) DO UPDATE SET
			settings = EXCLUDED.settings,
			updated_at = EXCLUDED.updated_at
	`, userID, adminAccountID, string(settingsJSON))
	return err
}
