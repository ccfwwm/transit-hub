package channel_monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/authctx"
)

func TestModelTargetsKeepPartialChannelAndReuseEvidence(t *testing.T) {
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("disposable database required")
	}
	ctx := context.Background()
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS real_connections(id text PRIMARY KEY,user_id text,workspace_admin_account_id text)`); e != nil {
		t.Fatal(e)
	}
	repo := NewRepository(db)
	if e = repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	_, _ = db.Exec(ctx, `DELETE FROM channel_monitor_results WHERE rule_id='model-integration';DELETE FROM channel_monitor_model_states WHERE rule_id='model-integration';DELETE FROM channel_monitor_group_states WHERE rule_id='model-integration';DELETE FROM channel_monitor_rules WHERE id='model-integration'`)
	_, fixture, _, platform := targetFixture()
	fixture.conns.connections[0].ID = "model-integration"
	fixture.conns.connections[0].OwnGroupIDs = []string{"a"}
	conn := fixture.conns.connections[0]
	if _, e = db.Exec(ctx, `INSERT INTO real_connections VALUES($1,'user-1','admin-1') ON CONFLICT DO NOTHING`, conn.ID); e != nil {
		t.Fatal(e)
	}
	if e = repo.EnsureWorkspaceRules(ctx, "user-1", "admin-1"); e != nil {
		t.Fatal(e)
	}
	config := DefaultTestModelConfig("user-1", "admin-1")
	config.StatusSource = "active"
	config.GroupModels = []TestModelGroupConfig{{GroupID: "a", ModelID: "good", Mode: "text"}, {GroupID: "a", ModelID: "bad", Mode: "text"}}
	if e = repo.SaveTestModelConfig(ctx, config); e != nil {
		t.Fatal(e)
	}
	s := NewService(repo, fixture.conns, fixture.states, fixture.upstreams, platform, fakeAccounts{})
	rule, e := repo.GetRule(ctx, conn.ID)
	if e != nil || rule == nil {
		t.Fatal(e)
	}
	if _, e = s.runRule(ctx, *rule, "manual"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.runRule(ctx, *rule, "manual"); e != nil {
		t.Fatal(e)
	}
	states, e := repo.ListTargetStates(ctx, "user-1", "admin-1")
	if e != nil {
		t.Fatal(e)
	}
	target := states[conn.ID]["a"]
	if len(target.Models) != 1 || target.Models[0].Model != "good" || target.Status != StatusUnknown || len(platform.testOptions) != 0 {
		t.Fatalf("legacy config caused model calls or changed evidence selection: %+v", target)
	}
	if calls := len(platform.schedulableCalls); calls != 0 {
		t.Fatal("partial failure disabled entire account")
	}
	// A persistent upstream observation is consumed once, even if fetched again.
	_, e = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS upstream_model_health(site_id text NOT NULL,user_id text NOT NULL,admin_account_id text NOT NULL,source_id text NOT NULL,model text NOT NULL,group_id text NOT NULL,source text NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(site_id,source_id));CREATE TABLE IF NOT EXISTS upstream_health_mappings(site_id text NOT NULL,source_id text NOT NULL,group_id text NOT NULL,model text NOT NULL,PRIMARY KEY(site_id,source_id))`)
	if e != nil {
		t.Fatal(e)
	}
	config.GroupModels = []TestModelGroupConfig{{GroupID: "a", ModelID: "reported-model", Mode: "text"}}
	if e = repo.SaveTestModelConfig(ctx, config); e != nil {
		t.Fatal(e)
	}

	config.StatusSource = "upstream"
	if e = repo.SaveTestModelConfig(ctx, config); e != nil {
		t.Fatal(e)
	}
	// NewHealthService requires a real platform wrapper but performs no requests.
	us := upstream.NewService(upstream.NewPlatformService(upstream.NewHTTPClient(nil)), nil, nil, nil)
	healthService, e := upstream.NewHealthService(db, us, "")
	if e != nil {
		t.Fatal(e)
	}
	s.SetModelHealth(healthService)
	if e = healthService.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	stamp := time.Now().UTC()
	payload, _ := json.Marshal(upstream.ModelHealth{ID: "reported", Model: "reported-model", GroupID: conn.UpstreamGroupID, Source: "sub2api_v1", Scope: "group_model", Status: StatusFailed, CheckedAt: &stamp, CollectedAt: stamp})
	_, e = db.Exec(ctx, `INSERT INTO upstream_model_health VALUES($1,'user-1','admin-1','reported','reported-model',$2,'sub2api_v1',$3) ON CONFLICT(site_id,source_id) DO UPDATE SET payload=EXCLUDED.payload`, fixture.upstreams.site.ID, conn.UpstreamGroupID, payload)
	if e != nil {
		t.Fatal(e)
	}
	before := len(platform.testOptions)
	for i := 0; i < 2; i++ {
		if _, e = s.runRule(ctx, *rule, "manual"); e != nil {
			t.Fatal(e)
		}
	}
	if len(platform.testOptions) != before {
		t.Fatal("fresh reported state triggered billable probe")
	}
	modelStates, e := repo.modelStates(ctx, rule.ID)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range modelStates {
		if v.Model == "reported-model" {
			found = true
			if v.ConsecutiveFailures != 1 {
				t.Fatal(fmt.Sprintf("same observation counted %d times", v.ConsecutiveFailures))
			}
		}
	}
	if !found {
		t.Fatal("missing model observation")
	}
	// The browser merges this exact Key's directory with upstream evidence,
	// preserving unknown models and never exposing the Key or contacting upstream.
	for _, v := range []upstream.ModelHealth{
		{ID: "kuma-own", Source: "newapi_kuma", Model: "own channel", GroupID: conn.UpstreamGroupID, Scope: "channel_reference", Status: StatusHealthy, CheckedAt: &stamp},
		{ID: "kuma-other", Source: "newapi_kuma", Model: "other channel", GroupID: "unrelated", Scope: "channel_reference", Status: StatusFailed},
		{ID: "kuma-unmatched", Source: "newapi_kuma", Model: "unmatched channel", Scope: "channel_reference", Status: StatusFailed},
		{ID: "kuma-server", Source: "newapi_kuma", Model: "server", Scope: "infrastructure", Status: StatusHealthy},
	} {
		raw, _ := json.Marshal(v)
		_, err := db.Exec(ctx, `INSERT INTO upstream_model_health VALUES($1,'user-1','admin-1',$2,$3,$4,$5,$6) ON CONFLICT(site_id,source_id) DO UPDATE SET payload=EXCLUDED.payload,group_id=EXCLUDED.group_id`, fixture.upstreams.site.ID, v.ID, v.Model, v.GroupID, v.Source, raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, e = db.Exec(ctx, `INSERT INTO upstream_health_mappings VALUES($1,'kuma-own',$2,'channel-alias') ON CONFLICT(site_id,source_id) DO UPDATE SET group_id=EXCLUDED.group_id,model=EXCLUDED.model`, fixture.upstreams.site.ID, conn.UpstreamGroupID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(ctx, `INSERT INTO channel_monitor_model_catalog(rule_id,user_id,admin_account_id,fingerprint,models) VALUES($1,'user-1','admin-1',$2,'["reported-model","unreported-model"]') ON CONFLICT(rule_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,models=EXCLUDED.models`, rule.ID, configFingerprint(conn, AccountTestOptions{}))
	if e != nil {
		t.Fatal(e)
	}
	h := Handler{service: s}
	request := httptest.NewRequest("GET", "/api/channel-monitor/rules/"+rule.ID+"/models", nil).WithContext(authctx.WithUserID(ctx, "user-1"))
	request.SetPathValue("id", rule.ID)
	response := httptest.NewRecorder()
	before = len(platform.testOptions)
	h.browseModels(response, request)
	var browser modelBrowser
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &browser) != nil || browser.Total != 1 || browser.Source != "upstream" {
		t.Fatalf("model browser failed: %s", response.Body.String())
	}
	if browser.Items[0].Model != "reported-model" || len(platform.testOptions) != before {
		t.Fatal("status view included unreported catalog entries or invoked a paid probe")
	}
	if browser.ModelSummary == nil || browser.ModelSummary.ID != "reported" || browser.ModelSummary.Status != StatusFailed {
		t.Fatalf("reported model lost priority over the channel reference: %+v", browser.ModelSummary)
	}
	requestAll := httptest.NewRequest("GET", "/api/channel-monitor/rules/"+rule.ID+"/models?includeCatalog=1", nil).WithContext(authctx.WithUserID(ctx, "user-1"))
	requestAll.SetPathValue("id", rule.ID)
	allResponse := httptest.NewRecorder()
	h.browseModels(allResponse, requestAll)
	var allBrowser modelBrowser
	if allResponse.Code != 200 || json.Unmarshal(allResponse.Body.Bytes(), &allBrowser) != nil || allBrowser.Total != 2 || allBrowser.ReportedTotal != 1 || len(platform.testOptions) != before {
		t.Fatalf("full directory missing or invoked probe: %s", allResponse.Body.String())
	}
	for _, m := range allBrowser.Items {
		if m.Model == "unreported-model" && (m.Status != "unknown" || len(m.Timeline) > 0) {
			t.Fatal("catalog model acquired fabricated status")
		}
	}
	if len(browser.References) != 2 {
		t.Fatalf("New API group references lost or mixed unrelated channels: %+v", browser.References)
	}
	for _, ref := range browser.References {
		if ref.Usable {
			t.Fatal("manual alias promoted a channel reference to model evidence")
		}
	}
	other := request.WithContext(authctx.WithUserID(ctx, "other-user"))
	response = httptest.NewRecorder()
	h.browseModels(response, other)
	if response.Code == 200 {
		t.Fatal("cross-user model directory leaked")
	}
	// Upstream mode never falls back to a billable probe when no report matches.
	config.GroupModels = []TestModelGroupConfig{{GroupID: "a", ModelID: "not-reported", Mode: "text"}}
	if e = repo.SaveTestModelConfig(ctx, config); e != nil {
		t.Fatal(e)
	}
	base := SummaryResponse{
		Channels: []ChannelStatus{{RuleID: rule.ID, SiteID: fixture.upstreams.site.ID, UpstreamGroupID: conn.UpstreamGroupID, OwnGroupIDs: []string{"a"}, Enabled: true, Supported: true, EffectiveTestModelID: "not-reported", GroupStates: []TargetState{{GroupID: "a", Model: "not-reported"}}}},
		Groups:   []GroupSummary{{GroupID: "a"}},
	}
	if e = s.applyStatusSource(ctx, "user-1", "admin-1", &base); e != nil {
		t.Fatal(e)
	}
	if e = repo.publishSnapshot(ctx, "user-1", "admin-1", base); e != nil {
		t.Fatal(e)
	}
	response = httptest.NewRecorder()
	h.browseModels(response, request)
	browser = modelBrowser{}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &browser) != nil || browser.ModelSummary == nil || browser.ModelSummary.ID != "kuma-own" || browser.ModelSummary.Status != StatusHealthy || browser.MonitoredModel != "not-reported" || browser.ReportedTotal != 1 {
		t.Fatalf("channel fallback or model count incorrect: %s", response.Body.String())
	}
	for _, groupID := range []string{"", "a"} {
		page, err := repo.readPage(ctx, "user-1", "admin-1", ChannelPageQuery{GroupID: groupID, RuleID: rule.ID})
		if err != nil || len(page.Channels) != 1 {
			t.Fatalf("snapshot missing for group %q: %v", groupID, err)
		}
		row := page.Channels[0]
		if row.Status != StatusUnknown || row.ModelSummary == nil || row.ModelSummary.ID != browser.ModelSummary.ID || row.ModelSummary.Status != browser.ModelSummary.Status {
			t.Fatalf("snapshot changed model health or disagreed with detail: %+v", row)
		}
	}
	if _, ok := healthService.MatchConfigured(ctx, fixture.upstreams.site.ID, conn.UpstreamGroupID, conn.UpstreamGroupName, "channel-alias"); ok {
		t.Fatal("channel reference was accepted as dispatch evidence")
	}
	before = len(platform.testOptions)
	if _, e = s.runRule(ctx, *rule, "manual"); e != nil {
		t.Fatal(e)
	}
	if len(platform.testOptions) != before {
		t.Fatal("upstream-only mode made a paid call")
	}
	// One-shot verification remains available in passive mode and never changes dispatch.
	// A previously accepted active configuration is migrated at startup.
	if _, e = db.Exec(ctx, `UPDATE channel_monitor_test_model_configs SET status_source='active'
		WHERE user_id='user-1' AND admin_account_id='admin-1'`); e != nil {
		t.Fatal(e)
	}
	if e = repo.EnsureSchema(ctx); e != nil {
		t.Fatal(e)
	}
	var source string
	if e = db.QueryRow(ctx, `SELECT status_source FROM channel_monitor_test_model_configs WHERE user_id='user-1' AND admin_account_id='admin-1'`).Scan(&source); e != nil || source != "upstream" {
		t.Fatalf("legacy source was not migrated: %s, %v", source, e)
	}
	patch := httptest.NewRequest("PATCH", "/status-source", bytes.NewBufferString(`{"source":"active"}`)).WithContext(authctx.WithUserID(ctx, "user-1"))
	res := httptest.NewRecorder()
	h.statusSource(res, patch)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"source":"upstream"`) {
		t.Fatalf("legacy client re-enabled active monitoring: %s", res.Body.String())
	}
	request = httptest.NewRequest("POST", "/models/check", nil).WithContext(authctx.WithUserID(ctx, "user-1"))
	res = httptest.NewRecorder()
	h.checkModel(res, request)
	if res.Code != 410 {
		t.Fatalf("legacy model check was accepted: %s", res.Body.String())
	}
	if _, e = s.manualModelCheck(ctx, "user-1", "admin-1", rule.ID, nil); e == nil || len(platform.testOptions) != 0 {
		t.Fatal("a persisted manual model check reached the upstream API")
	}

	// The default is still passive under the scheduler, even with no upstream report.
	if _, e = s.runRule(ctx, *rule, "scheduled"); e != nil || len(platform.testOptions) != 0 {
		t.Fatalf("default scheduler ran a model probe: %v", e)
	}
	platform.probe = func(options AccountTestOptions) (AccountTestResult, error) {
		return AccountTestResult{Success: true, Model: options.ModelID, LatencyMS: 42, Message: "model responded"}, nil
	}
	ctx = context.WithValue(ctx, readWorkspaceKey{}, "admin-1")
	saved, e := s.UpdateRule(ctx, "user-1", rule.ID, UpdateRuleRequest{ActiveCheckEnabled: boolPtr(true)})
	if e != nil || !saved.ActiveCheckEnabled {
		t.Fatalf("cannot enable active checks: %+v, %v", saved, e)
	}
	if _, e = s.runRule(ctx, *rule, "manual"); e != nil || len(platform.testOptions) != 0 {
		t.Fatalf("sync triggered a paid probe: %v", e)
	}
	for i := 0; i < 2; i++ {
		if _, e = s.runRule(ctx, *rule, "scheduled"); e != nil {
			t.Fatal(e)
		}
	}
	if len(platform.testOptions) != 1 || platform.testOptions[0].ModelID != "not-reported" {
		t.Fatalf("active check ignored the selected model or interval: %+v", platform.testOptions)
	}
	if e = s.RefreshReadModel(ctx, "user-1", "admin-1"); e != nil {
		t.Fatal(e)
	}
	page, e := repo.readPage(ctx, "user-1", "admin-1", ChannelPageQuery{GroupID: "a", RuleID: rule.ID, ModelStatus: StatusHealthy})
	if e != nil || page.Total != 1 || len(page.Channels) != 1 {
		t.Fatalf("active observation missing from filtered snapshot: %v, %+v", e, page)
	}
	row := page.Channels[0]
	if !row.ActiveCheckEnabled || row.ModelSummary == nil || row.ModelSummary.Source != activeStatusSource || len(row.ModelSummary.Timeline) != 1 {
		t.Fatalf("active evidence missing from compact row: %+v", row.ModelSummary)
	}
	response = httptest.NewRecorder()
	h.browseModels(response, requestAll)
	browser = modelBrowser{}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &browser) != nil ||
		browser.ModelSummary == nil || browser.ModelSummary.Source != activeStatusSource || browser.ModelSummary.Status != StatusHealthy {
		t.Fatalf("active model status missing from browser: %s", response.Body.String())
	}
	if saved, e = s.UpdateRule(ctx, "user-1", rule.ID, UpdateRuleRequest{ActiveCheckEnabled: boolPtr(false)}); e != nil || saved.ActiveCheckEnabled {
		t.Fatalf("cannot disable active checks: %+v, %v", saved, e)
	}
	if _, e = s.runRule(ctx, *rule, "scheduled"); e != nil || len(platform.testOptions) != 1 {
		t.Fatalf("scheduler probed after disabling active checks: %v", e)
	}
}
