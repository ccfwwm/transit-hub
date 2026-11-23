package upstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"transithub/backend/internal/modules/jobs"
)

func TestManualModelPreviewCreatesOneTemporaryKeyAndCleansUp(t *testing.T) {
	dsn := os.Getenv("MONITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "monitor_verify") {
		t.Fatal("disposable database required")
	}
	ctx := context.Background()
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	creates, invocations, deletes, catalogs := 0, 0, 0, 0
	const secret = "fixture-temporary-credential"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		reply := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"data": data, "success": true}) }
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/keys":
			creates++
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["group_id"] != float64(4) || !strings.HasPrefix(safeString(input, "name"), "THcheck-") {
				t.Error("temporary Key did not use the selected group")
			}
			reply(map[string]any{"id": 7})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/keys/7":
			reply(map[string]any{"id": 7, "key": secret, "group_id": 4})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/keys/7":
			deletes++
			reply(nil)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			catalogs++
			reply([]map[string]string{{"id": "fixture-model"}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/responses":
			invocations++
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["model"] != "fixture-model" || r.Header.Get("Authorization") != "Bearer "+secret {
				t.Error("probe did not use the selected model and credential")
			}
			_, _ = fmt.Fprint(w, `{"output":[{"content":[{"type":"output_text","text":"OK"}]}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	platform := NewPlatformService(NewHTTPClient(server.Client()))
	s, err := NewHealthService(db, NewService(platform, nil, nil, nil), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	policies := NewKeyPolicies(db, platform)
	if err = policies.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	s.Register(http.NewServeMux(), jobs.New(db), policies)
	session := Session{BaseURL: server.URL, Platform: PlatformSub2API, AccessToken: "fixture-management-session"}
	data, err := s.seal(previewData{Login: &LoginResult{Session: session, Platform: PlatformSub2API}})
	if err != nil {
		t.Fatal(err)
	}
	const uid, aid, previewID = "manual-preview-user", "manual-preview-workspace", "manual-preview-fixture"
	if _, err = db.Exec(ctx, `INSERT INTO upstream_health_previews(id,user_id,admin_account_id,encrypted) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET encrypted=EXCLUDED.encrypted,expires_at=now()+interval '10 minutes'`, previewID, uid, aid, data); err != nil {
		t.Fatal(err)
	}
	for _, catalogOnly := range []bool{true, false} {
		id, err := randomID()
		if err != nil {
			t.Fatal(err)
		}
		task := manualModelTask{Selection: manualModelSelection{GroupID: "4", Model: "fixture-model", Mode: "text", Platform: "openai", CatalogOnly: catalogOnly}, PreviewID: previewID, Phase: "new", StartedAt: time.Now()}
		sealed, err := s.seal(task)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `INSERT INTO upstream_preview_probes(id,preview_id,user_id,admin_account_id,encrypted) VALUES($1,$2,$3,$4,$5)`, id, previewID, uid, aid, sealed); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			out, err := s.runManualModelTest(ctx, uid, aid, id, policies)
			result, ok := out.Value.(manualModelResult)
			if err != nil || !ok || !result.Success || catalogOnly && (len(result.Models) != 1 || result.Models[0] != "fixture-model") {
				t.Fatalf("manual test did not finish: %+v %v", out, err)
			}
		}
	}
	if creates != 2 || catalogs != 1 || invocations != 1 || deletes != 2 {
		t.Fatalf("unexpected operation counts: create=%d catalog=%d invoke=%d delete=%d", creates, catalogs, invocations, deletes)
	}
}
