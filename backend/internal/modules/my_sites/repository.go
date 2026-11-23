package my_sites

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transithub/backend/internal/modules/upstream"
)

type Repository struct {
	db     *pgxpool.Pool
	cipher *upstream.SiteCredentialCipher
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) SetCredentialCipher(cipher *upstream.SiteCredentialCipher) { r.cipher = cipher }

const stateSessionScope = "my_site_states.session"
const connectionKeyScope = "real_connections.upstream_key"

func (r *Repository) MigrateCredentials(ctx context.Context) error {
	if r.cipher == nil {
		return nil
	}
	if err := upstream.MigrateCredentialJSON(ctx, r.db, "my_site_states", "session", "jsonb_typeof(session)='object'", func(raw []byte) ([]byte, error) {
		return r.cipher.ProtectJSON(stateSessionScope, raw)
	}); err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id, upstream_key FROM real_connections WHERE upstream_key<>'' AND upstream_key_fingerprint='' FOR UPDATE`)
	if err != nil {
		return err
	}
	type row struct{ id, key string }
	var values []row
	for rows.Next() {
		var value row
		if err = rows.Scan(&value.id, &value.key); err != nil {
			rows.Close()
			return err
		}
		values = append(values, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, value := range values {
		secret, err := r.cipher.OpenField(connectionKeyScope, value.key)
		if err != nil {
			return err
		}
		protected, fingerprint, err := protectConnectionKey(r.cipher, secret)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE real_connections SET upstream_key=$2,upstream_key_fingerprint=$3 WHERE id=$1`, value.id, protected, fingerprint); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func protectConnectionKey(cipher *upstream.SiteCredentialCipher, value string) (string, string, error) {
	protected, err := cipher.ProtectField(connectionKeyScope, value)
	return protected, cipher.SecretIndex(connectionKeyScope, value), err
}

func connectionKeyMatch(cipher *upstream.SiteCredentialCipher, value string) string {
	if cipher == nil {
		return value
	}
	return cipher.SecretIndex(connectionKeyScope, value)
}

func (r *Repository) EnsureSchema(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS my_site_states (
			user_id text NOT NULL,
			admin_account_id text NOT NULL DEFAULT '',
			base_url text NOT NULL,
			email text NOT NULL,
			session jsonb NOT NULL,
			mappings jsonb NOT NULL DEFAULT '[]'::jsonb,
			own_groups jsonb NOT NULL DEFAULT '[]'::jsonb,
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `ALTER TABLE my_site_states ADD COLUMN IF NOT EXISTS own_groups jsonb NOT NULL DEFAULT '[]'::jsonb`)
	if err != nil {
		return err
	}
	statements := []string{
		`ALTER TABLE my_site_states ADD COLUMN IF NOT EXISTS admin_account_id text NOT NULL DEFAULT ''`,
		`ALTER TABLE my_site_states DROP CONSTRAINT IF EXISTS my_site_states_pkey`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_my_site_states_workspace ON my_site_states (user_id, admin_account_id)`,
	}
	for _, statement := range statements {
		if _, err := r.db.Exec(ctx, statement); err != nil {
			return err
		}
	}

	// real_connections 表存储真实对接的绑定记录：上游 key + admin 账号 + 自有分组关联。
	// 注意两个不同的 admin account 字段：
	//   - workspace_admin_account_id: TransitHub 工作区归属（对应 admin_accounts 表），
	//     用于 workspace 数据隔离，语义同其他业务表的 admin_account_id 列。
	//   - admin_account_id: 上游平台的 admin 转发账号 ID，是真实对接的业务字段。
	_, err = r.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS real_connections (
			id text PRIMARY KEY,
			user_id text NOT NULL,
			workspace_admin_account_id text NOT NULL DEFAULT '',
			upstream_site_id text NOT NULL,
			upstream_group_id text NOT NULL,
			upstream_group_name text NOT NULL,
			upstream_key_id text NOT NULL,
			upstream_key text NOT NULL,
			admin_account_id text NOT NULL,
			admin_account_name text NOT NULL,
			own_group_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
			group_type text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `ALTER TABLE real_connections ADD COLUMN IF NOT EXISTS workspace_admin_account_id text NOT NULL DEFAULT ''`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `ALTER TABLE real_connections ADD COLUMN IF NOT EXISTS upstream_api_url text NOT NULL DEFAULT '',
		ADD COLUMN IF NOT EXISTS upstream_key_fingerprint text NOT NULL DEFAULT ''`)
	return err
}

func (r *Repository) Get(ctx context.Context, userID string, adminAccountID string) (*State, error) {
	row := r.db.QueryRow(ctx, `SELECT user_id, admin_account_id, base_url, email, session, mappings, own_groups FROM my_site_states WHERE user_id = $1 AND admin_account_id = $2`, userID, adminAccountID)
	var state State
	var sessionJSON []byte
	var mappingsJSON []byte
	var ownGroupsJSON []byte
	if err := row.Scan(&state.UserID, &state.AdminAccountID, &state.BaseURL, &state.Email, &sessionJSON, &mappingsJSON, &ownGroupsJSON); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	sessionJSON, err := r.cipher.OpenJSON(stateSessionScope, sessionJSON)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sessionJSON, &state.Session); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(mappingsJSON, &state.Mappings); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(ownGroupsJSON, &state.OwnGroups); err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *Repository) Save(ctx context.Context, state State) error {
	sessionJSON, err := json.Marshal(state.Session)
	if err != nil {
		return err
	}
	sessionJSON, err = r.cipher.ProtectJSON(stateSessionScope, sessionJSON)
	if err != nil {
		return err
	}
	mappingsJSON, err := json.Marshal(state.Mappings)
	if err != nil {
		return err
	}
	ownGroupsJSON, err := json.Marshal(state.OwnGroups)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO my_site_states (user_id, admin_account_id, base_url, email, session, mappings, own_groups, updated_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, now())
		ON CONFLICT (user_id, admin_account_id) DO UPDATE SET
			base_url = EXCLUDED.base_url,
			email = EXCLUDED.email,
			session = EXCLUDED.session,
			mappings = EXCLUDED.mappings,
			own_groups = EXCLUDED.own_groups,
			updated_at = EXCLUDED.updated_at
	`, state.UserID, state.AdminAccountID, state.BaseURL, state.Email, string(sessionJSON), string(mappingsJSON), string(ownGroupsJSON))
	return err
}

// SaveRealConnection 持久化一条真实对接绑定记录。
func (r *Repository) SaveRealConnection(ctx context.Context, conn RealConnection) error {
	ownGroupIDsJSON, err := json.Marshal(conn.OwnGroupIDs)
	if err != nil {
		return err
	}
	key, fingerprint, err := protectConnectionKey(r.cipher, conn.UpstreamKey)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO real_connections (id, user_id, workspace_admin_account_id, upstream_site_id, upstream_group_id, upstream_group_name, upstream_key_id, upstream_key, admin_account_id, admin_account_name, own_group_ids, group_type, created_at, upstream_api_url, upstream_key_fingerprint)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15)
	`, conn.ID, conn.UserID, conn.WorkspaceAdminAccountID, conn.UpstreamSiteID, conn.UpstreamGroupID, conn.UpstreamGroupName,
		conn.UpstreamKeyID, key, conn.AdminAccountID, conn.AdminAccountName,
		string(ownGroupIDsJSON), conn.GroupType, conn.CreatedAt, conn.UpstreamAPIURL, fingerprint)
	return err
}

func (r *Repository) UpdateRealConnectionGroups(ctx context.Context, id string, userID string, adminAccountID string, ownGroupIDs []string) error {
	groupsJSON, err := json.Marshal(ownGroupIDs)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		UPDATE real_connections
		SET own_group_ids = $4::jsonb
		WHERE id = $1 AND user_id = $2 AND workspace_admin_account_id = $3
	`, id, userID, adminAccountID, string(groupsJSON))
	return err
}

// ListRealConnections 查询指定用户的所有真实对接绑定记录，按创建时间倒序。
func (r *Repository) ListRealConnections(ctx context.Context, userID string, adminAccountID string) ([]RealConnection, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, workspace_admin_account_id, upstream_site_id, upstream_group_id, upstream_group_name,
		       upstream_key_id, upstream_key, admin_account_id, admin_account_name,
		       own_group_ids, group_type, created_at, upstream_api_url
		FROM real_connections WHERE user_id = $1 AND workspace_admin_account_id = $2 ORDER BY created_at DESC
	`, userID, adminAccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []RealConnection
	for rows.Next() {
		var conn RealConnection
		var ownGroupIDsJSON []byte
		var createdAt time.Time
		if err := rows.Scan(&conn.ID, &conn.UserID, &conn.WorkspaceAdminAccountID, &conn.UpstreamSiteID, &conn.UpstreamGroupID, &conn.UpstreamGroupName,
			&conn.UpstreamKeyID, &conn.UpstreamKey, &conn.AdminAccountID, &conn.AdminAccountName,
			&ownGroupIDsJSON, &conn.GroupType, &createdAt, &conn.UpstreamAPIURL); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(ownGroupIDsJSON, &conn.OwnGroupIDs); err != nil {
			return nil, err
		}
		key, err := r.cipher.OpenField(connectionKeyScope, conn.UpstreamKey)
		if err != nil {
			return nil, err
		}
		conn.UpstreamKey = key
		conn.CreatedAt = createdAt.Format(time.RFC3339)
		connections = append(connections, conn)
	}
	return connections, rows.Err()
}

// GetRealConnection 根据 ID 和用户 ID 查询单条真实对接绑定记录。
func (r *Repository) GetRealConnection(ctx context.Context, id string, userID string, adminAccountID string) (*RealConnection, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, user_id, workspace_admin_account_id, upstream_site_id, upstream_group_id, upstream_group_name,
		       upstream_key_id, upstream_key, admin_account_id, admin_account_name,
		       own_group_ids, group_type, created_at, upstream_api_url
		FROM real_connections WHERE id = $1 AND user_id = $2 AND workspace_admin_account_id = $3
	`, id, userID, adminAccountID)
	var conn RealConnection
	var ownGroupIDsJSON []byte
	var createdAt time.Time
	if err := row.Scan(&conn.ID, &conn.UserID, &conn.WorkspaceAdminAccountID, &conn.UpstreamSiteID, &conn.UpstreamGroupID, &conn.UpstreamGroupName,
		&conn.UpstreamKeyID, &conn.UpstreamKey, &conn.AdminAccountID, &conn.AdminAccountName,
		&ownGroupIDsJSON, &conn.GroupType, &createdAt, &conn.UpstreamAPIURL); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(ownGroupIDsJSON, &conn.OwnGroupIDs); err != nil {
		return nil, err
	}
	key, err := r.cipher.OpenField(connectionKeyScope, conn.UpstreamKey)
	if err != nil {
		return nil, err
	}
	conn.UpstreamKey = key
	conn.CreatedAt = createdAt.Format(time.RFC3339)
	return &conn, nil
}

// DeleteRealConnection 根据 ID 和用户 ID 删除一条真实对接绑定记录。
func (r *Repository) DeleteRealConnection(ctx context.Context, id string, userID string, adminAccountID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM real_connections WHERE id = $1 AND user_id = $2 AND workspace_admin_account_id = $3`, id, userID, adminAccountID)
	return err
}
