package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// An explicit Key choice overrides its site's default. Empty IP lists are
// allowed only through this switch; ordinary policy editing remains fail-closed.
func (p *KeyPolicies) WhitelistEnabled(ctx context.Context, uid, aid, site, key string) (bool, error) {
	var enabled bool
	e := p.db.QueryRow(ctx, `SELECT enabled FROM upstream_key_whitelist_switches WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND key_id=$4`, uid, aid, site, key).Scan(&enabled)
	if e == nil {
		return enabled, nil
	}
	if e != pgx.ErrNoRows {
		return false, e
	}
	var disabled bool
	e = p.db.QueryRow(ctx, `SELECT coalesce((settings->>'keyWhitelistDisabled')::boolean,false) FROM upstream_sites WHERE id=$1 AND user_id=$2 AND admin_account_id=$3`, site, uid, aid).Scan(&disabled)
	return !disabled, e
}
func (p *KeyPolicies) DesiredIPs(ctx context.Context, uid, aid string, site Site, key, group string) ([]string, error) {
	enabled, e := p.WhitelistEnabled(ctx, uid, aid, site.ID, key)
	if e != nil {
		return nil, e
	}
	if !enabled {
		return []string{}, nil
	}
	var raw []byte
	err := p.db.QueryRow(ctx, "SELECT addresses FROM upstream_key_ip_overrides WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND key_id=$4", uid, aid, site.ID, key).Scan(&raw)
	if err == nil {
		var addresses []string
		err = json.Unmarshal(raw, &addresses)
		return addresses, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// Group policy retains its configured addresses even when its site is disabled.
	v, e := p.Group(ctx, uid, aid, site.ID, group)
	return v.AllowedIPs, e
}
func (p *KeyPolicies) SetWhitelistEnabled(ctx context.Context, uid, aid, site, key string, enabled bool) error {
	_, e := p.db.Exec(ctx, `INSERT INTO upstream_key_whitelist_switches(user_id,admin_account_id,site_id,key_id,enabled) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,admin_account_id,site_id,key_id) DO UPDATE SET enabled=excluded.enabled,updated_at=now()`, uid, aid, site, key, enabled)
	return e
}
func (p *KeyPolicies) CopyWhitelistSwitch(ctx context.Context, uid, aid, site, oldKey, newKey string) error {
	_, e := p.db.Exec(ctx, `INSERT INTO upstream_key_whitelist_switches(user_id,admin_account_id,site_id,key_id,enabled) SELECT user_id,admin_account_id,site_id,$5,enabled FROM upstream_key_whitelist_switches WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND key_id=$4 ON CONFLICT(user_id,admin_account_id,site_id,key_id) DO NOTHING`, uid, aid, site, oldKey, newKey)
	if e != nil {
		return e
	}
	_, e = p.db.Exec(ctx, "INSERT INTO upstream_key_ip_overrides(user_id,admin_account_id,site_id,key_id,addresses) SELECT user_id,admin_account_id,site_id,$5,addresses FROM upstream_key_ip_overrides WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND key_id=$4 ON CONFLICT DO NOTHING", uid, aid, site, oldKey, newKey)
	return e
}

func (p *KeyPolicies) SaveKeyIPOverride(ctx context.Context, uid, aid, site, key string, ips []string) error {
	raw, err := json.Marshal(ips)
	if err != nil {
		return err
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "INSERT INTO upstream_key_ip_overrides VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,admin_account_id,site_id,key_id) DO UPDATE SET addresses=EXCLUDED.addresses", uid, aid, site, key, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO upstream_key_whitelist_switches(user_id,admin_account_id,site_id,key_id,enabled) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,admin_account_id,site_id,key_id) DO UPDATE SET enabled=EXCLUDED.enabled,updated_at=now()", uid, aid, site, key, len(ips) > 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *KeyPolicies) BindingWhitelistEnabled(ctx context.Context, uid, aid, id string) (bool, error) {
	var site, key string
	e := p.db.QueryRow(ctx, `SELECT upstream_site_id,upstream_key_id FROM real_connections WHERE id=$1 AND user_id=$2 AND workspace_admin_account_id=$3`, id, uid, aid).Scan(&site, &key)
	if e != nil {
		return false, e
	}
	return p.WhitelistEnabled(ctx, uid, aid, site, key)
}
