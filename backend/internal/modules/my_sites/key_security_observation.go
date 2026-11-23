package my_sites

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
)

// Called with sanitized metadata from existing authenticated upstream responses.
// Matching responses update the read model without another upstream request.
func (k *KeySecurity) ObserveKeys(values []upstream.KeySecurityObservation) {
	if k.cipher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, v := range values {
		if v.AuthFingerprint == "" {
			continue
		}
		rows, e := k.db.Query(ctx, `SELECT s.id,s.user_id,s.admin_account_id,s.session FROM upstream_sites s JOIN key_security_policies p ON p.user_id=s.user_id AND p.admin_account_id=s.admin_account_id WHERE p.audit_enabled AND (rtrim(s.base_url,'/')=$1 OR rtrim(s.session->>'BaseURL','/')=$1) AND EXISTS(SELECT 1 FROM real_connections c WHERE c.user_id=s.user_id AND c.workspace_admin_account_id=s.admin_account_id AND c.upstream_site_id=s.id AND c.upstream_key_id=$2 AND c.admin_account_id<>'' AND c.upstream_key<>'')`, v.BaseURL, v.KeyID)
		if e != nil {
			return
		}
		type scope struct{ site, uid, aid string }
		scopes := []scope{}
		for rows.Next() {
			var s scope
			var raw []byte
			if rows.Scan(&s.site, &s.uid, &s.aid, &raw) != nil || len(raw) == 0 {
				continue
			}
			session, err := upstream.DecodeStoredSiteSession(k.sites.storageCipher, raw)
			if err == nil && upstream.KeyObservationAuth(session.AccessToken, session.Cookie, session.UserID) == v.AuthFingerprint {
				scopes = append(scopes, s)
			}
		}
		rows.Close()
		for _, s := range scopes {
			_ = k.policies.Observe(ctx, s.uid, s.aid, s.site, v.KeyID, v.AllowedIPs)
			group := v.GroupID
			if group == "" {
				_ = k.db.QueryRow(ctx, `SELECT upstream_group_id FROM real_connections WHERE user_id=$1 AND workspace_admin_account_id=$2 AND upstream_site_id=$3 AND upstream_key_id=$4 LIMIT 1`, s.uid, s.aid, s.site, v.KeyID).Scan(&group)
			}
			site, e := k.ownedSite(ctx, s.uid, s.aid, s.site)
			if e != nil {
				continue
			}
			want, e := k.policies.DesiredIPs(ctx, s.uid, s.aid, *site, v.KeyID, group)
			if e != nil {
				continue
			}
			if len(want) == 0 && len(v.AllowedIPs) == 0 {
				continue
			}
			got, e := upstream.NormalizeAllowedIPs(v.AllowedIPs)
			if e == nil && strings.Join(want, "\n") == strings.Join(got, "\n") {
				continue
			}
			// Coalesce observations from list + detail and active security verification.
			tag, e := k.db.Exec(ctx, `INSERT INTO key_security_observation_claims(user_id,admin_account_id,site_id,key_id,queued_at) VALUES($1,$2,$3,$4,now()) ON CONFLICT(user_id,admin_account_id,site_id,key_id) DO UPDATE SET queued_at=now() WHERE key_security_observation_claims.queued_at<now()-interval '1 minute'`, s.uid, s.aid, s.site, v.KeyID)
			if e != nil || tag.RowsAffected() == 0 {
				continue
			}
			var active bool
			e = k.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operation_jobs j JOIN operation_items i ON i.job_id=j.id WHERE j.user_id=$1 AND j.admin_account_id=$2 AND j.kind='key_security' AND i.id=$3 AND i.status IN('queued','running','waiting'))`, s.uid, s.aid, s.site+"/"+v.KeyID).Scan(&active)
			if e != nil || active {
				continue
			}
			t := securityTarget{SiteID: s.site, KeyID: v.KeyID, GroupID: group, Action: "audit"}
			raw, _ := json.Marshal(t)
			_, _ = k.jobs.Submit(ctx, s.uid, s.aid, "key_security", []jobs.Item{{ID: s.site + "/" + v.KeyID, Payload: raw}})
		}
	}
}
