package my_sites

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/httpjson"
)

type keyRef struct {
	SiteID string `json:"siteId"`
	KeyID  string `json:"keyId"`
}

var keyIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var errBoundKeyCleanup = errors.New("Key 已对接或有进行中的对接任务，请先处理转发账号")

func (ref keyRef) itemID() string { return ref.SiteID + "/" + ref.KeyID }
func (ref keyRef) valid() bool {
	return ref.SiteID != "" && len(ref.SiteID) <= 200 && keyIDPattern.MatchString(ref.KeyID)
}

func (m *KeyManagement) cleanupKeys(w http.ResponseWriter, r *http.Request) {
	uid, aid, err := m.scope(r)
	if err != nil {
		httpjson.WriteError(w, http.StatusUnauthorized, "工作区会话不可用")
		return
	}
	var req struct {
		Keys []keyRef `json:"keys"`
	}
	if httpjson.Decode(r, &req) != nil || len(req.Keys) == 0 || len(req.Keys) > 500 {
		httpjson.WriteError(w, http.StatusBadRequest, "请选择 1 至 500 个 Key")
		return
	}
	items := make([]jobs.Item, 0, len(req.Keys))
	seen := make(map[keyRef]bool)
	sites, err := m.workspaceSites(r.Context(), uid, aid)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "无法读取工作区站点")
		return
	}
	for _, ref := range req.Keys {
		if !ref.valid() || sites[ref.SiteID] == nil {
			httpjson.WriteError(w, http.StatusBadRequest, "存在无效或不属于当前工作区的 Key")
			return
		}
		if !seen[ref] {
			items = append(items, jobs.Item{ID: ref.itemID(), Payload: encodeBindingPayload(ref)})
			seen[ref] = true
		}
	}
	job, err := m.jobs.Submit(r.Context(), uid, aid, "upstream_key_cleanup", items)
	if err != nil {
		apiResult(w, nil, err)
		return
	}
	httpjson.Write(w, http.StatusAccepted, job)
}

func (m *KeyManagement) runKeyCleanup(ctx context.Context, uid, aid, itemID string, raw json.RawMessage) (jobs.Outcome, error) {
	var ref keyRef
	if json.Unmarshal(raw, &ref) != nil || !ref.valid() || ref.itemID() != itemID {
		return jobs.Outcome{}, fmt.Errorf("无效 Key 清理任务")
	}
	site, err := m.owned(ctx, uid, aid, ref.SiteID)
	if err != nil {
		return jobs.Outcome{}, err
	}
	if err = m.removeIndependentKey(ctx, uid, aid, site, ref.KeyID); err != nil {
		return jobs.Outcome{}, err
	}
	return jobs.Outcome{Value: map[string]string{"message": "已核验 Key 清理结果"}}, nil
}

func (m *KeyManagement) removeIndependentKey(ctx context.Context, uid, aid string, site *upstream.Site, id string) error {
	unlock, err := m.lockOperation(ctx, "key-security|"+site.ID+"|"+id)
	if err != nil {
		return err
	}
	defer unlock()
	bound, err := m.isBound(ctx, uid, aid, site.ID, id)
	if err != nil {
		return err
	}
	if bound {
		return errBoundKeyCleanup
	}
	var rotating bool
	err = m.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM key_security_rotations
		WHERE user_id=$1 AND admin_account_id=$2 AND site_id=$3 AND $4 IN(old_key_id,new_key_id)
		AND phase NOT IN('done','cancelled'))`, uid, aid, site.ID, id).Scan(&rotating)
	if err != nil {
		return err
	}
	if rotating {
		return fmt.Errorf("Key 正在轮换，请等待轮换完成")
	}
	if err = m.platform.DeleteSecurityKey(ctx, *site.Session, id); err != nil {
		return fmt.Errorf("上游删除失败或暂不能核验，请重试清理")
	}
	m.invalidate(site)
	return nil
}
