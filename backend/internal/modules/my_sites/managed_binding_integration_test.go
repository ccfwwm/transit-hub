package my_sites

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"transithub/backend/internal/modules/jobs"
	"transithub/backend/internal/modules/upstream"
)

func TestManagedBindingName(t *testing.T) {
	rate := 0.150
	for input, want := range map[string]string{
		"https://api.foxrouter.com/v1": "foxrouter", "https://api.Example.co.uk": "example",
		"http://192.0.2.12": "", "http://foo.internal": "", "https://api.食狮.com.cn": "xn--85x722f",
	} {
		if got := bindingSlug(input); got != want {
			t.Errorf("bindingSlug(%s) = %q, want %q", input, got, want)
		}
	}
	if name, err := managedBindingName("foxrouter", " codex · 企业 分组 ", &rate, 6, upstream.PlatformSub2API); err != nil || name != "foxrouter-codex-企业-分组-0.15x-006" {
		t.Fatalf("name = %q, err = %v", name, err)
	}
	name, err := managedBindingName("foxrouter", strings.Repeat("限", 30), &rate, 1000, upstream.PlatformNewAPI)
	if err != nil || len(name) > 50 || !utf8.ValidString(name) || !strings.HasSuffix(name, "-0.15x-1000") {
		t.Fatalf("New API name = %q, err = %v", name, err)
	}
}

func TestForwardingAccountPlatformFollowsDestinationDispatch(t *testing.T) {
	for _, test := range []struct{ source, destination, want string }{
		{"imagevideo", "openai", "openai"},
		{"openai", "deepseek", "deepseek"},
		{"anthropic", "kimi", "kimi"},
		{"imagevideo", "composite", "imagevideo"},
		{"anthropic", "claude", "anthropic"},
		{"openai", "", "openai"},
	} {
		if platform := forwardingAccountPlatform(test.source, test.destination); platform != test.want {
			t.Errorf("%s to %s uses %s, want %s", test.source, test.destination, platform, test.want)
		}
	}
}

func TestManagedSplitResumesAfterAccountMembershipFailure(t *testing.T) {
	for _, platform := range []upstream.Platform{upstream.PlatformSub2API, upstream.PlatformNewAPI} {
		t.Run(string(platform), func(t *testing.T) {
			ctx := context.Background()
			db := securityTestDatabase(t)
			repo := NewRepository(db)
			oldCredential := "fixture-current-key-xyz"
			newCredential := "fixture-successor-key-xyz"
			keyRecords := map[string]map[string]any{"7": {"id": 7, "name": "source-key-name", "key": oldCredential, "group_id": 3}}
			if platform == upstream.PlatformNewAPI {
				keyRecords["7"]["group"] = "3"
				keyRecords["7"]["unlimited_quota"] = true
			}
			accounts := map[string]map[string]any{"1": {
				"id": 1, "name": "source-account-name", "platform": "openai", "type": "apikey", "schedulable": true,
				"group_ids":   []any{float64(8), float64(9)},
				"credentials": map[string]any{"api_key": oldCredential, "base_url": "https://fixture.example", "pool_mode": true},
			}}
			keyCreates, accountCreates := 0, 0
			failSwitch := true
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				path := r.URL.Path
				send := func(value any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": value}) }
				read := func() map[string]any { var b map[string]any; _ = json.NewDecoder(r.Body).Decode(&b); return b }
				list := func(source map[string]map[string]any) {
					items := []any{}
					for _, value := range source {
						items = append(items, value)
					}
					send(map[string]any{"items": items, "total": len(items)})
				}
				switch {
				case path == "/api/v1/admin/groups/all":
					send([]any{
						map[string]any{"id": 8, "name": "Pro"},
						map[string]any{"id": 9, "name": "Ultra"},
						map[string]any{"id": 10, "name": "Enterprise"},
						map[string]any{"id": 11, "name": "Team"},
					})
				case path == "/api/v1/keys" || path == "/api/token/":
					switch r.Method {
					case "GET":
						list(keyRecords)
					case "POST":
						record := read()
						id := 14 + keyCreates
						record["id"] = id
						record["key"] = newCredential + strconv.Itoa(id)
						keyRecords[strconv.Itoa(id)] = record
						keyCreates++
						send(record)
					case "PUT":
						body := read()
						id := keyString(body["id"])
						for name, value := range body {
							keyRecords[id][name] = value
						}
						send(keyRecords[id])
					}
				case strings.HasPrefix(path, "/api/v1/keys/") || strings.HasPrefix(path, "/api/token/"):
					id := strings.Split(strings.TrimPrefix(strings.TrimPrefix(path, "/api/v1/keys/"), "/api/token/"), "/")[0]
					record := keyRecords[id]
					if r.Method == "PUT" {
						for k, v := range read() {
							record[k] = v
						}
					}
					send(record)
				case path == "/api/v1/admin/accounts/data":
					id := r.URL.Query().Get("ids")
					send(map[string]any{"accounts": []any{accounts[id]}})
				case path == "/api/v1/admin/accounts":
					if r.Method == "GET" {
						list(accounts)
						return
					}
					body := read()
					id := 2 + accountCreates
					body["id"] = id
					body["schedulable"] = true
					accounts[strconv.Itoa(id)] = body
					accountCreates++
					send(body)
				case strings.HasPrefix(path, "/api/v1/admin/accounts/"):
					fields := strings.Split(strings.TrimPrefix(path, "/api/v1/admin/accounts/"), "/")
					id := fields[0]
					record := accounts[id]
					if r.Method == "GET" {
						send(record)
						return
					}
					body := read()
					if id == "1" && body["group_ids"] != nil && failSwitch {
						failSwitch = false
						http.Error(w, "temporary failure", 503)
						return
					}
					for name, value := range body {
						record[name] = value
					}
					send(record)
				default:
					t.Errorf("unexpected route %s %s", r.Method, path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			siteSession := upstream.Session{Platform: platform, BaseURL: server.URL, AccessToken: "upstream-fixture"}
			rate := 0.15
			site := &upstream.Site{ID: "site", BaseURL: "https://api.foxrouter.com", UserID: "user", AdminAccountID: "workspace", Platform: platform, Session: &siteSession, Metrics: upstream.Metrics{Groups: []upstream.GroupInfo{{ID: "3", Multiplier: &rate}}}}
			svc := NewService(repo, upstream.NewPlatformService(upstream.NewHTTPClient(server.Client())), realConnectLookup{site: site})
			if err := svc.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			runner := jobs.New(db)
			if err := runner.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			manager, err := NewKeyManagement(ctx, db, svc, runner)
			if err != nil {
				t.Fatal(err)
			}
			conn := RealConnection{ID: "connection", UserID: "user", WorkspaceAdminAccountID: "workspace", UpstreamSiteID: "site", UpstreamGroupID: "3", UpstreamKeyID: "7", UpstreamKey: oldCredential, AdminAccountID: "1", AdminAccountName: "source-account-name", OwnGroupIDs: []string{"8", "9"}, GroupType: "openai", CreatedAt: time.Now().Format(time.RFC3339Nano)}
			if err = repo.SaveRealConnection(ctx, conn); err != nil {
				t.Fatal(err)
			}
			_, err = manager.reserveBindingNames(ctx, "user", "workspace", []bindingNameRow{{ConnectionID: conn.ID, GroupID: "8", Slug: "foxrouter"}, {ConnectionID: conn.ID, GroupID: "9", Slug: "foxrouter"}})
			if err != nil {
				t.Fatal(err)
			}
			task := managedBindingJob{Plan: bindingPlan{ID: conn.ID, Targets: []bindingTarget{
				{GroupID: "8", Name: "foxrouter-pro-0.15x-001", Number: 1},
				{GroupID: "9", Name: "foxrouter-ultra-0.15x-002", Number: 2},
			}}, Selection: bindingSelection{Retain: "8"}, KeyID: "7", AccountID: "1"}
			admin := upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: server.URL, AccessToken: "admin-fixture"}
			if _, err = manager.executeBinding(ctx, conn, site, admin, task); err == nil {
				t.Fatal("fixture did not interrupt split")
			}
			if len(accounts["1"]["group_ids"].([]any)) != 2 {
				t.Fatal("original path was removed before successful cutover")
			}
			if _, err = manager.executeBinding(ctx, conn, site, admin, task); err != nil {
				t.Fatal(err)
			}
			if accountCreates != 1 || keyCreates != 1 {
				t.Fatalf("retry duplicated resources: keys=%d accounts=%d", keyCreates, accountCreates)
			}
			all, err := repo.ListRealConnections(ctx, "user", "workspace")
			if err != nil || len(all) != 2 {
				t.Fatalf("expected two bindings, count=%d err=%v", len(all), err)
			}
			for _, entry := range all {
				if len(entry.OwnGroupIDs) != 1 || entry.AdminAccountName != keyRecords[entry.UpstreamKeyID]["name"] {
					t.Fatalf("wrong groups or mismatched remote names for %s", entry.ID)
				}
			}
			if !slices.Equal(accounts["1"]["group_ids"].([]any), []any{float64(8)}) {
				t.Fatal("retained account has wrong groups")
			}
			state := State{UserID: "user", AdminAccountID: "workspace", Session: admin}
			if err = repo.Save(ctx, state); err != nil {
				t.Fatal(err)
			}
			// Existing split bindings and new groups share an upstream group.
			req := RealConnectRequest{UpstreamGroupID: "3", OwnGroupIDs: []string{"8", "10", "11"}}
			created, err := manager.connectManaged(ctx, "user", &state, site, req, "openai", "Upstream Pro")
			if err != nil {
				t.Fatal(err)
			}
			if len(created.Connections) != 3 || created.Connections[0].ID != conn.ID || keyCreates != 3 || accountCreates != 3 {
				t.Fatalf("existing binding was not reused or new groups shared a resource: bindings=%d keys=%d accounts=%d", len(created.Connections), keyCreates, accountCreates)
			}
			for i, binding := range created.Connections[1:] {
				groupID := req.OwnGroupIDs[i+1]
				accountGroups := accounts[binding.AdminAccountID]["group_ids"].([]any)
				if !slices.Equal(binding.OwnGroupIDs, []string{groupID}) || len(accountGroups) != 1 || keyString(accountGroups[0]) != groupID || binding.AdminAccountName != keyRecords[binding.UpstreamKeyID]["name"] {
					t.Fatalf("new binding did not get its own named key and forwarding group: %s", binding.ID)
				}
			}
			otherModel := "do-not-overwrite-existing"
			req.TestModelID = &otherModel
			retry, err := manager.connectManaged(ctx, "user", &state, site, req, "openai", "Upstream Pro")
			if err != nil || len(retry.Connections) != 3 || retry.Connections[1].ID != created.Connections[1].ID || keyCreates != 3 || accountCreates != 3 {
				t.Fatalf("repeat request did not reuse existing bindings: %v", err)
			}
		})
	}
}

func TestManagedUsageAggregatesRemainCompleteWithPaginatedLogs(t *testing.T) {
	ctx := context.Background()
	db := securityTestDatabase(t)
	repo := NewRepository(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key_id") != "7" {
			t.Error("usage request missing key ID")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/usage/dashboard/snapshot-v2" {
			_, _ = w.Write([]byte(`{"data":{"granularity":"day","start_date":"2026-11-22","end_date":"2026-11-22","trend":[{"date":"2026-11-22","requests":9999,"actual_cost":6.25}],"models":[{"model":"gpt","requests":9999,"actual_cost":6.25}]}}`))
			return
		}
		if r.URL.Path == "/api/v1/usage" && r.URL.Query().Get("page") == "1" {
			_, _ = w.Write([]byte(`{"data":{"items":[{"id":2,"api_key_id":7,"created_at":"2026-11-22T08:00:00+08:00","actual_cost":0.01,"model":"gpt","ip_address":"192.0.2.1"}],"total":9999}}`))
			return
		}
		t.Errorf("unexpected native usage request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()
	session := upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: server.URL, AccessToken: "fixture-auth"}
	site := &upstream.Site{ID: "site", UserID: "user", AdminAccountID: "workspace", Platform: session.Platform, Session: &session}
	client := upstream.NewPlatformService(upstream.NewHTTPClient(server.Client()))
	manager, err := NewKeyManagement(ctx, db, NewService(repo, client, nil), jobs.New(db))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 22, 0, 0, 0, 0, usageZone)
	report, err := manager.readKeyUsage(ctx, "user", "workspace", site, "7", "day", start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if report.State != "complete" || report.Total.Requests != 9999 || report.Total.Amount == nil || *report.Total.Amount != 6.25 || len(report.Logs) != 1 || report.Logs[0].IP != "192.0.2.1" {
		t.Fatalf("unexpected aggregate or recent logs: %+v", report)
	}
}
