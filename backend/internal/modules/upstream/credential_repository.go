package upstream

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const automaticReloginCooldownMS int64 = 15 * 60 * 1000

type StoredSiteCredential struct {
	SiteID                          string
	UserID                          string
	AdminAccountID                  string
	Password                        string
	LastAutomaticReloginAtUnixMilli int64
}

type CredentialStore interface {
	SavePassword(ctx context.Context, credential StoredSiteCredential) error
	LoadPassword(ctx context.Context, userID, adminAccountID, siteID string) (StoredSiteCredential, bool, error)
	Delete(ctx context.Context, userID, adminAccountID, siteID string) error
	MarkAutomaticReloginAttempt(ctx context.Context, userID, adminAccountID, siteID string, attemptedAtUnixMilli int64) error
}

type CredentialRepository struct {
	db     *pgxpool.Pool
	cipher *SiteCredentialCipher
}

func NewCredentialRepository(db *pgxpool.Pool, encodedKey string) (*CredentialRepository, error) {
	if strings.TrimSpace(encodedKey) == "" {
		return nil, nil
	}
	cipher, err := NewSiteCredentialCipher(encodedKey)
	if err != nil {
		return nil, err
	}
	return &CredentialRepository{db: db, cipher: cipher}, nil
}

func (r *CredentialRepository) EnsureSchema(ctx context.Context) error {
	if r == nil {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS upstream_site_credentials (
			site_id text PRIMARY KEY,
			user_id text NOT NULL,
			admin_account_id text NOT NULL,
			password_ciphertext text NOT NULL,
			last_automatic_relogin_at bigint NOT NULL DEFAULT 0,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`)
	return err
}

func (r *CredentialRepository) SavePassword(ctx context.Context, credential StoredSiteCredential) error {
	ciphertext, err := r.cipher.Encrypt(credential.Password)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO upstream_site_credentials (site_id, user_id, admin_account_id, password_ciphertext, last_automatic_relogin_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (site_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			admin_account_id = EXCLUDED.admin_account_id,
			password_ciphertext = EXCLUDED.password_ciphertext,
			last_automatic_relogin_at = EXCLUDED.last_automatic_relogin_at,
			updated_at = now()
	`, credential.SiteID, credential.UserID, credential.AdminAccountID, ciphertext, credential.LastAutomaticReloginAtUnixMilli)
	return err
}

func (r *CredentialRepository) LoadPassword(ctx context.Context, userID, adminAccountID, siteID string) (StoredSiteCredential, bool, error) {
	var ciphertext string
	credential := StoredSiteCredential{SiteID: siteID, UserID: userID, AdminAccountID: adminAccountID}
	err := r.db.QueryRow(ctx, `
		SELECT password_ciphertext, last_automatic_relogin_at
		FROM upstream_site_credentials
		WHERE site_id = $1 AND user_id = $2 AND admin_account_id = $3
	`, siteID, userID, adminAccountID).Scan(&ciphertext, &credential.LastAutomaticReloginAtUnixMilli)
	if err != nil {
		if err == pgx.ErrNoRows {
			return StoredSiteCredential{}, false, nil
		}
		return StoredSiteCredential{}, false, err
	}
	password, err := r.cipher.Decrypt(ciphertext)
	if err != nil {
		return StoredSiteCredential{}, false, err
	}
	credential.Password = password
	return credential, true, nil
}

func (r *CredentialRepository) Delete(ctx context.Context, userID, adminAccountID, siteID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM upstream_site_credentials WHERE site_id = $1 AND user_id = $2 AND admin_account_id = $3`, siteID, userID, adminAccountID)
	return err
}

func (r *CredentialRepository) MarkAutomaticReloginAttempt(ctx context.Context, userID, adminAccountID, siteID string, attemptedAtUnixMilli int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE upstream_site_credentials
		SET last_automatic_relogin_at = $4, updated_at = now()
		WHERE site_id = $1 AND user_id = $2 AND admin_account_id = $3
	`, siteID, userID, adminAccountID, attemptedAtUnixMilli)
	return err
}
