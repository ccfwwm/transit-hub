package my_sites

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/authctx"
	"transithub/backend/internal/shared/httpjson"
)

type KeyManagement struct {
	db         *pgxpool.Pool
	sites      *Service
	jobs       *jobs.Service
	platform   *upstream.PlatformService
	mu         sync.Mutex
	reads      singleflight.Group
	cache      map[string]keyCacheEntry
	usageSlots chan struct{}
}

type keyCacheEntry struct {
	until time.Time
	value any
}

func NewKeyManagement(ctx context.Context, db *pgxpool.Pool, sites *Service, runner *jobs.Service) (*KeyManagement, error) {
	m := &KeyManagement{db: db, sites: sites, jobs: runner, platform: sites.platformService, cache: make(map[string]keyCacheEntry), usageSlots: make(chan struct{}, 4)}
	_, err := db.Exec(ctx, `
	CREATE TABLE IF NOT EXISTS managed_binding_names (
		user_id text NOT NULL, workspace_id text NOT NULL, connection_id text NOT NULL, own_group_id text NOT NULL,
		slug text NOT NULL, ordinal integer NOT NULL, applied_name text NOT NULL DEFAULT '',
		PRIMARY KEY(user_id,workspace_id,connection_id,own_group_id), UNIQUE(user_id,workspace_id,slug,ordinal)
	);
	CREATE TABLE IF NOT EXISTS managed_key_aliases (
		user_id text NOT NULL, workspace_id text NOT NULL, site_id text NOT NULL, key_id text NOT NULL, name text NOT NULL,
		first_seen timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,workspace_id,site_id,key_id,name)
	);
	CREATE TABLE IF NOT EXISTS managed_key_lineage (
  user_id text NOT NULL, workspace_id text NOT NULL, site_id text NOT NULL, previous_id text NOT NULL, successor_id text NOT NULL,
  PRIMARY KEY(user_id,workspace_id,site_id,previous_id)
 );
 CREATE TABLE IF NOT EXISTS managed_binding_progress (
		user_id text NOT NULL, workspace_id text NOT NULL, operation_id text NOT NULL, own_group_id text NOT NULL,
		site_id text NOT NULL DEFAULT '', key_id text NOT NULL DEFAULT '', account_id text NOT NULL DEFAULT '', step text NOT NULL DEFAULT '', recovery jsonb NOT NULL DEFAULT '{}',
		PRIMARY KEY(user_id,workspace_id,operation_id,own_group_id)
	);
	CREATE TABLE IF NOT EXISTS managed_binding_splits (
		user_id text NOT NULL, workspace_id text NOT NULL, connection_id text NOT NULL, split_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY(user_id,workspace_id,connection_id)
	);
	CREATE TABLE IF NOT EXISTS managed_binding_slugs (
		user_id text NOT NULL, workspace_id text NOT NULL, site_id text NOT NULL, slug text NOT NULL,
		PRIMARY KEY(user_id,workspace_id,site_id)
	);`)
	if err != nil {
		return nil, err
	}
	sites.keyManagement = m
	runner.Register("managed_key_binding", m.runBinding)
	runner.Register("upstream_key_cleanup", m.runKeyCleanup)
	return m, nil
}

func (m *KeyManagement) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/upstream-keys", m.listKeys)
	mux.HandleFunc("POST /api/upstream-keys/cleanup", m.cleanupKeys)
	mux.HandleFunc("POST /api/upstream-keys/usage-summary", m.keyUsageSummary)
	mux.HandleFunc("POST /api/upstream-keys/{site}", m.createKey)
	mux.HandleFunc("GET /api/upstream-keys/{site}/{key}", m.detailKey)
	mux.HandleFunc("PATCH /api/upstream-keys/{site}/{key}", m.editKey)
	mux.HandleFunc("DELETE /api/upstream-keys/{site}/{key}", m.deleteKey)
	mux.HandleFunc("GET /api/upstream-keys/{site}/{key}/usage", m.keyUsage)
	mux.HandleFunc("POST /api/upstream-keys/{site}/{key}/test", m.testKey)
	mux.HandleFunc("GET /api/upstream-key-bindings/preview", m.previewBindings)
	mux.HandleFunc("POST /api/upstream-key-bindings/apply", m.applyBindings)
	mux.HandleFunc("PUT /api/upstream-key-bindings/sites/{site}/slug", m.setSlug)
}

func (m *KeyManagement) scope(r *http.Request) (string, string, error) {
	uid, _ := authctx.UserID(r.Context())
	if uid == "" {
		return "", "", fmt.Errorf("请先登录")
	}
	aid, err := m.sites.currentAdminAccountID(r.Context(), uid)
	return uid, aid, err
}

func (m *KeyManagement) owned(ctx context.Context, uid, aid, id string) (*upstream.Site, error) {
	site, err := m.sites.upstreamLookup.GetSite(ctx, id)
	if err != nil || site == nil || site.UserID != uid || site.AdminAccountID != aid || site.Session == nil {
		return nil, fmt.Errorf("站点不存在或上游尚未登录")
	}
	return site, nil
}

func (m *KeyManagement) requestSite(w http.ResponseWriter, r *http.Request) (string, string, *upstream.Site) {
	uid, aid, err := m.scope(r)
	if err != nil {
		httpjson.WriteError(w, 401, "工作区会话不可用")
		return "", "", nil
	}
	site, err := m.owned(r.Context(), uid, aid, r.PathValue("site"))
	if err != nil {
		httpjson.WriteError(w, 404, err.Error())
		return "", "", nil
	}
	return uid, aid, site
}

func (m *KeyManagement) cached(ctx context.Context, key string, load func(context.Context) (any, error)) (any, error) {
	result := m.reads.DoChan(key, func() (any, error) {
		m.mu.Lock()
		entry, ok := m.cache[key]
		m.mu.Unlock()
		if ok && time.Now().Before(entry.until) {
			return entry.value, nil
		}
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		value, err := load(run)
		if err == nil {
			m.mu.Lock()
			if len(m.cache) >= 512 {
				for k, v := range m.cache {
					if time.Now().After(v.until) {
						delete(m.cache, k)
					}
				}
				if len(m.cache) >= 512 {
					for k := range m.cache {
						delete(m.cache, k)
						break
					}
				}
			}
			m.cache[key] = keyCacheEntry{time.Now().Add(5 * time.Minute), value}
			m.mu.Unlock()
		}
		return value, err
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case answer := <-result:
		return answer.Val, answer.Err
	}
}

func inventoryCacheKey(site *upstream.Site) string {
	s := site.Session
	auth := sha256.Sum256([]byte(s.BaseURL + "\x00" + s.AccessToken + "\x00" + s.Cookie + "\x00" + s.UserID))
	return fmt.Sprintf("%s/%s/%s/%x", site.UserID, site.AdminAccountID, site.ID, auth)
}

type keyEntry struct {
	Remaining map[string]float64  `json:"remaining,omitempty"`
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	SiteID    string              `json:"siteId"`
	GroupID   string              `json:"groupId"`
	GroupName string              `json:"groupName"`
	Status    string              `json:"status"`
	LastUsed  any                 `json:"lastUsed,omitempty"`
	Used      any                 `json:"used,omitempty"`
	Config    map[string]any      `json:"config"`
	Bindings  []keyBindingSummary `json:"bindings"`
}

type keyBindingSummary struct {
	ID          string     `json:"id"`
	Account     string     `json:"account"`
	Groups      []string   `json:"groups"`
	Update      bool       `json:"update"`
	SharedSince *time.Time `json:"sharedSince,omitempty"`
}

type keySite struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Slug     string               `json:"slug"`
	Platform upstream.Platform    `json:"platform"`
	Groups   []upstream.GroupInfo `json:"groups"`
	Error    string               `json:"error,omitempty"`
}

func keyString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return ""
}

func safeKeyEntry(site *upstream.Site, record map[string]any) keyEntry {
	row := keyEntry{
		Remaining: remainingLimits(site.Platform, record),
		ID:        upstream.SecurityKeyID(record), Name: keyString(record["name"]), SiteID: site.ID,
		GroupID: upstream.SecurityKeyGroup(site.Platform, record), Status: keyString(record["status"]),
		Config: upstream.ManagedKeyConfig(site.Platform, record), Bindings: []keyBindingSummary{},
		LastUsed: record["last_used_at"], Used: record["quota_used"],
	}
	if site.Platform == upstream.PlatformNewAPI {
		row.LastUsed = record["accessed_time"]
		row.Used = record["used_quota"]
	}
	for _, g := range site.Metrics.Groups {
		if g.ID == row.GroupID {
			row.GroupName = g.Name
			break
		}
	}
	return row
}

func (m *KeyManagement) inventory(ctx context.Context, site *upstream.Site) ([]keyEntry, error) {
	value, err := m.cached(ctx, inventoryCacheKey(site)+"/inventory", func(run context.Context) (any, error) {
		records, err := m.platform.SecurityKeys(run, *site.Session)
		if err != nil {
			return nil, err
		}
		rows := make([]keyEntry, 0, len(records))
		for _, record := range records {
			rows = append(rows, safeKeyEntry(site, record))
		}
		return rows, nil
	})
	if err != nil {
		return nil, err
	}
	return append([]keyEntry(nil), value.([]keyEntry)...), nil
}

func (m *KeyManagement) invalidate(site *upstream.Site) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cache, inventoryCacheKey(site)+"/inventory")
}

func (m *KeyManagement) workspaceSites(ctx context.Context, uid, aid string) (map[string]*upstream.Site, error) {
	provider, ok := m.sites.upstreamLookup.(monitorSiteList)
	if !ok {
		return nil, fmt.Errorf("上游列表不可用")
	}
	return provider.MonitorSites(ctx, uid, aid)
}

func (m *KeyManagement) listKeys(w http.ResponseWriter, r *http.Request) {
	uid, aid, err := m.scope(r)
	if err != nil {
		httpjson.WriteError(w, 401, "工作区会话不可用")
		return
	}
	ctx := r.Context()
	sites, err := m.workspaceSites(ctx, uid, aid)
	if err != nil {
		httpjson.WriteError(w, 500, "无法读取上游站点")
		return
	}
	conns, err := m.sites.connRepository.ListRealConnections(ctx, uid, aid)
	if err != nil {
		httpjson.WriteError(w, 500, "无法读取绑定")
		return
	}
	state, err := m.sites.repository.Get(ctx, uid, aid)
	if err != nil {
		httpjson.WriteError(w, 500, "无法读取自有分组")
		return
	}
	labels := map[string]string{}
	if state != nil {
		for _, group := range state.OwnGroups {
			labels[group.ID] = group.Name
		}
	}
	slugs, err := m.siteSlugs(ctx, uid, aid)
	if err != nil {
		httpjson.WriteError(w, 500, "无法读取域名简称")
		return
	}
	summaries, err := m.bindingSummaries(ctx, uid, aid, conns, sites, labels, slugs)
	if err != nil {
		httpjson.WriteError(w, 500, "无法读取绑定命名状态")
		return
	}
	metadata := make([]keySite, 0, len(sites))
	for id, site := range sites {
		slug := slugs[id]
		if slug == "" {
			slug = bindingSlug(site.BaseURL)
		}
		metadata = append(metadata, keySite{ID: id, Name: site.Name, Slug: slug, Platform: site.Platform, Groups: site.Metrics.Groups})
	}
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].Name < metadata[j].Name })
	var group errgroup.Group
	group.SetLimit(4)
	perSite := make([][]keyEntry, len(metadata))
	for i := range metadata {
		if filter := r.URL.Query().Get("site"); filter != "" && filter != metadata[i].ID {
			continue
		}
		group.Go(func() error {
			site := sites[metadata[i].ID]
			if site.Session == nil {
				metadata[i].Error = "上游尚未登录"
				return nil
			}
			rows, err := m.inventory(ctx, site)
			if err != nil {
				metadata[i].Error = "上游 Key 列表暂不可用"
				return nil
			}
			for j := range rows {
				if values := summaries[site.ID+"\x00"+rows[j].ID]; len(values) > 0 {
					rows[j].Bindings = append([]keyBindingSummary(nil), values...)
					for k := range rows[j].Bindings {
						if rows[j].Name != rows[j].Bindings[k].Account {
							rows[j].Bindings[k].Update = true
						}
					}
				}
			}
			perSite[i] = rows
			return nil
		})
	}
	_ = group.Wait()
	rows := []keyEntry{}
	for _, entries := range perSite {
		rows = append(rows, entries...)
	}
	own := []GroupOption{}
	if state != nil {
		own = state.OwnGroups
	}
	httpjson.Write(w, 200, map[string]any{"sites": metadata, "items": rows, "ownGroups": own})
}

func (m *KeyManagement) siteSlugs(ctx context.Context, uid, aid string) (map[string]string, error) {
	rows, err := m.db.Query(ctx, `SELECT site_id,slug FROM managed_binding_slugs WHERE user_id=$1 AND workspace_id=$2`, uid, aid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, slug string
		if err = rows.Scan(&id, &slug); err != nil {
			return nil, err
		}
		out[id] = slug
	}
	return out, rows.Err()
}

func (m *KeyManagement) bindingSummaries(ctx context.Context, uid, aid string, conns []RealConnection, sites map[string]*upstream.Site, labels, slugs map[string]string) (map[string][]keyBindingSummary, error) {
	splits, err := m.db.Query(ctx, `SELECT connection_id,split_at FROM managed_binding_splits WHERE user_id=$1 AND workspace_id=$2`, uid, aid)
	if err != nil {
		return nil, err
	}
	shared := map[string]time.Time{}
	for splits.Next() {
		var id string
		var at time.Time
		if err = splits.Scan(&id, &at); err != nil {
			splits.Close()
			return nil, err
		}
		shared[id] = at
	}
	err = splits.Err()
	splits.Close()
	if err != nil {
		return nil, err
	}
	names, err := m.db.Query(ctx, `SELECT connection_id,own_group_id,slug,ordinal,applied_name FROM managed_binding_names WHERE user_id=$1 AND workspace_id=$2`, uid, aid)
	if err != nil {
		return nil, err
	}
	defer names.Close()
	existing := map[string]bindingNameRow{}
	for names.Next() {
		var n bindingNameRow
		if err = names.Scan(&n.ConnectionID, &n.GroupID, &n.Slug, &n.Number, &n.Name); err != nil {
			return nil, err
		}
		existing[n.ConnectionID+"\x00"+n.GroupID] = n
	}
	out := map[string][]keyBindingSummary{}
	for _, conn := range conns {
		if conn.UpstreamKeyID == "" {
			continue
		}
		row := keyBindingSummary{ID: conn.ID, Account: conn.AdminAccountName, Groups: []string{}, Update: len(conn.OwnGroupIDs) != 1}
		for _, group := range conn.OwnGroupIDs {
			label := labels[group]
			if label == "" {
				label = group
			}
			row.Groups = append(row.Groups, label)
			if site := sites[conn.UpstreamSiteID]; site != nil {
				n := existing[conn.ID+"\x00"+group]
				rate := managedRate(site, conn.UpstreamGroupID)
				if rate == nil && n.Name != "" && n.Name == conn.AdminAccountName {
					continue
				}
				want, e := managedBindingName(n.Slug, label, rate, n.Number, site.Platform)
				row.Update = row.Update || n.Number == 0 || e != nil || want != n.Name || want != conn.AdminAccountName
			}
		}
		if at, ok := shared[conn.ID]; ok {
			row.SharedSince = &at
		}
		key := conn.UpstreamSiteID + "\x00" + conn.UpstreamKeyID
		out[key] = append(out[key], row)
	}
	return out, names.Err()
}

func (m *KeyManagement) setSlug(w http.ResponseWriter, r *http.Request) {
	uid, aid, site := m.requestSite(w, r)
	if site == nil {
		return
	}
	var input struct {
		Slug string `json:"slug"`
	}
	if httpjson.Decode(r, &input) != nil {
		httpjson.WriteError(w, 400, "域名简称无效")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(input.Slug))
	for _, c := range slug {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			httpjson.WriteError(w, 400, "简称仅支持字母、数字和连字符")
			return
		}
	}
	if slug == "" || len(slug) > 32 || strings.Trim(slug, "-") != slug {
		httpjson.WriteError(w, 400, "域名简称无效")
		return
	}
	_, err := m.db.Exec(r.Context(), `INSERT INTO managed_binding_slugs VALUES($1,$2,$3,$4) ON CONFLICT(user_id,workspace_id,site_id) DO UPDATE SET slug=EXCLUDED.slug`, uid, aid, site.ID, slug)
	if err != nil {
		httpjson.WriteError(w, 500, "无法保存域名简称")
		return
	}
	httpjson.Write(w, 200, input)
}

func apiResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		httpjson.WriteError(w, 422, err.Error())
		return
	}
	httpjson.Write(w, 200, value)
}

func encodeBindingPayload(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
