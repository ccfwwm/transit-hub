package channel_monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

func membershipHashes(conns []my_sites.RealConnection) map[string]string {
	groups := map[string]bool{}
	for _, c := range conns {
		if c.AdminAccountID == "" {
			continue
		}
		for _, g := range c.OwnGroupIDs {
			groups[g] = true
		}
	}
	out := map[string]string{}
	for g := range groups {
		component := map[string]bool{g: true}
		changed := true
		for changed {
			changed = false
			for _, c := range conns {
				if c.AdminAccountID == "" {
					continue
				}
				linked := false
				for _, id := range c.OwnGroupIDs {
					if component[id] {
						linked = true
						break
					}
				}
				if linked {
					for _, id := range c.OwnGroupIDs {
						if !component[id] {
							component[id] = true
							changed = true
						}
					}
				}
			}
		}
		ids := []string{}
		for _, c := range conns {
			if c.AdminAccountID == "" {
				continue
			}
			linked := false
			for _, id := range c.OwnGroupIDs {
				if component[id] {
					linked = true
					break
				}
			}
			if linked {
				own := append([]string(nil), c.OwnGroupIDs...)
				sort.Strings(own)
				raw, _ := json.Marshal(own)
				ids = append(ids, c.ID+"|"+c.AdminAccountID+"|"+c.UpstreamSiteID+"|"+c.UpstreamGroupID+"|"+string(raw))
			}
		}
		sort.Strings(ids)
		b, _ := json.Marshal(ids)
		sum := sha256.Sum256(b)
		out[g] = hex.EncodeToString(sum[:])
	}
	return out
}
func (s *Service) MembershipChanged(ctx context.Context, uid, aid string) {
	if e := s.reconcileMembershipPriority(ctx, uid, aid); e != nil {
		log.Printf("[priority-membership] queue failed workspace=%s: %v", aid, e)
	}
}

// Persist membership and its applied version separately. Snapshot refresh also
// reconciles this outbox, covering crashes between saving a binding and queuing.
func (s *Service) reconcileMembershipPriority(ctx context.Context, uid, aid string) error {
	repo, ok := s.store.(*Repository)
	if !ok || s.jobs == nil {
		return nil
	}
	state, e := s.states.Get(ctx, uid, aid)
	if e != nil {
		return e
	}
	if state == nil || state.Session.Platform != upstream.PlatformSub2API {
		return nil
	}
	conns, e := s.conns.ListRealConnections(ctx, uid, aid)
	if e != nil {
		return e
	}
	hashes := membershipHashes(conns)
	for _, g := range state.OwnGroups {
		if _, ok := hashes[g.ID]; !ok {
			hashes[g.ID] = "empty"
		}
	}
	for g, hash := range hashes {
		// A transaction lock serializes the event hook and periodic reconciliation.
		tx, e := repo.db.Begin(ctx)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, uid+"|"+aid+"|membership|"+g); e != nil {
			tx.Rollback(ctx)
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO channel_monitor_priority_membership(user_id,admin_account_id,group_id,fingerprint) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,admin_account_id,group_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,updated_at=now() WHERE channel_monitor_priority_membership.fingerprint<>EXCLUDED.fingerprint`, uid, aid, g, hash)
		var pending, active bool
		if e == nil {
			e = tx.QueryRow(ctx, `SELECT fingerprint<>applied FROM channel_monitor_priority_membership WHERE user_id=$1 AND admin_account_id=$2 AND group_id=$3`, uid, aid, g).Scan(&pending)
		}
		if e == nil && pending {
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operation_jobs j JOIN operation_items i ON i.job_id=j.id WHERE j.user_id=$1 AND j.admin_account_id=$2 AND j.kind='membership_priority' AND i.id=$3 AND i.status IN('queued','waiting','running'))`, uid, aid, g).Scan(&active)
		}
		if e == nil && pending && !active {
			_, e = s.jobs.Submit(ctx, uid, aid, "membership_priority", []jobs.Item{{ID: g}})
		}
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) registerMembershipPriority(j *jobs.Service) {
	j.Register("membership_priority", s.runMembershipPriority)
}
func (s *Service) runMembershipPriority(ctx context.Context, uid, aid, g string, _ json.RawMessage) (jobs.Outcome, error) {
	repo := s.store.(*Repository)
	var hash string
	if e := repo.db.QueryRow(ctx, `SELECT fingerprint FROM channel_monitor_priority_membership WHERE user_id=$1 AND admin_account_id=$2 AND group_id=$3`, uid, aid, g).Scan(&hash); e != nil {
		return jobs.Outcome{Pending: true, Delay: time.Minute, Value: map[string]string{"message": "等待绑定变更提交"}}, nil
	}
	s.invalidateAccountCache(uid, aid)
	s.invalidateGroupCache(uid, aid)
	result, e := s.applyPriorityForGroup(ctx, uid, aid, g, "", "membership-change")
	if e != nil || !result.Success {
		message := result.Message
		if e != nil {
			message = e.Error()
		}
		log.Printf("[priority-membership] retry group=%s: %s", g, message)
		return jobs.Outcome{Pending: true, Delay: time.Minute, Value: map[string]string{"message": message}}, nil
	}
	tag, e := repo.db.Exec(ctx, `UPDATE channel_monitor_priority_membership SET applied=$4,updated_at=now() WHERE user_id=$1 AND admin_account_id=$2 AND group_id=$3 AND fingerprint=$4`, uid, aid, g, hash)
	if e != nil {
		return jobs.Outcome{}, e
	}
	if tag.RowsAffected() == 0 {
		return jobs.Outcome{Pending: true, Delay: time.Second}, nil
	}
	if e = s.RefreshReadModel(ctx, uid, aid); e != nil {
		return jobs.Outcome{}, fmt.Errorf("优先级已写入，但快照刷新失败: %w", e)
	}
	return jobs.Outcome{Value: result}, nil
}
