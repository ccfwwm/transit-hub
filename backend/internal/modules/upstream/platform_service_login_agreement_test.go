package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSub2APILoginRequiresExplicitAgreementRevision(t *testing.T) {
	for _, revision := range []string{"", "older-revision", "accepted-revision"} {
		t.Run(revision, func(t *testing.T) {
			loginCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/login":
					loginCalls++
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["email"] != "test@example.com" || body["password"] != "password" {
						t.Fatal("missing credentials")
					}
					if body["login_agreement_revision"] != "accepted-revision" {
						w.WriteHeader(http.StatusForbidden)
						writeJSON(w, map[string]any{"reason": "LOGIN_AGREEMENT_REQUIRED"})
						return
					}
					writeJSON(w, map[string]any{"data": map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600}})
				case "/api/v1/auth/me":
					writeJSON(w, map[string]any{"data": map[string]any{"balance": 9.5, "total_recharged": 20}})
				case "/api/v1/usage/dashboard/stats":
					writeJSON(w, map[string]any{"data": map[string]any{"today_actual_cost": 1.5}})
				case "/api/v1/groups/available":
					availableGroupsFixture(w)
				case "/api/v1/groups/rates":
					writeJSON(w, map[string]any{"data": map[string]any{}})
				default:
					t.Errorf("unexpected path, must not auto-accept public agreement: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			service := NewPlatformService(NewHTTPClient(server.Client()))
			result, err := service.LoginWithOptions(server.URL, PlatformSub2API, "test@example.com", "password", false, revision)
			if loginCalls != 1 {
				t.Fatalf("login calls = %d", loginCalls)
			}
			if revision != "accepted-revision" {
				if errorKey(err) != ErrorLoginAgreementRequired {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Platform != PlatformSub2API || result.Session.Platform != PlatformSub2API || result.Session.RefreshToken != "refresh" {
				t.Fatal("incorrect platform/session")
			}
			if result.Metrics.Balance.Value == nil || *result.Metrics.Balance.Value != 9.5 || len(result.Metrics.Groups) != 3 {
				t.Fatal("metrics missing")
			}
		})
	}
}

func TestReloginPreservesAcceptedRevisionAndDoesNotAcceptChangedAgreement(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["login_agreement_revision"] != "previously-accepted" {
				t.Error("accepted revision not reused")
			}
			w.WriteHeader(http.StatusForbidden)
			writeJSON(w, map[string]any{"reason": "LOGIN_AGREEMENT_REQUIRED"})
		}))
		cache := newFakeSiteCache()
		site := &Site{ID: "site", UserID: "user", AdminAccountID: "admin", BaseURL: server.URL, RequestedPlatform: PlatformSub2API}
		cache.add(site)
		service := NewService(NewPlatformService(NewHTTPClient(server.Client())), nil, nil, cache)
		service.SetCredentialStore(&fakeCredentialStore{passwords: map[string]StoredSiteCredential{
			"site": {SiteID: "site", UserID: "user", AdminAccountID: "admin", Password: "password", LoginAgreementRevision: "previously-accepted"},
		}})
		response, err := service.relogin(context.Background(), site, automatic)
		if err != nil || response.Status != StatusError || response.ErrorKey == nil || *response.ErrorKey != ErrorLoginAgreementRequired {
			t.Fatalf("expected agreement error, got %v %v", response.ErrorKey, err)
		}
		server.Close()
	}
}
