package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"transithub/backend/internal/shared/authctx"
)

func TestGroupAPIURLKeepsManagementSessionAndWorkspaceSeparate(t *testing.T) {
	cache := newFakeSiteCache()
	cache.add(&Site{ID: "s", UserID: "u", AdminAccountID: "w", BaseURL: "https://apikl.ai", Session: &Session{BaseURL: "https://apikl.ai", AccessToken: "private"}, Metrics: Metrics{Groups: []GroupInfo{{ID: "g"}}}})
	s := NewService(nil, nil, nil, cache)
	resolver := &fakeAccountResolver{current: map[string]string{"u": "w"}}
	s.SetAdminAccountResolver(resolver)
	h := Handler{service: s, accounts: resolver}
	request := func(body, workspace string) int {
		resolver.current["u"] = workspace
		r := httptest.NewRequest(http.MethodPut, "/api/upstream-sites/s/group-api-url?groupId=g", strings.NewReader(body))
		r.SetPathValue("id", "s")
		r = r.WithContext(authctx.WithUserID(r.Context(), "u"))
		w := httptest.NewRecorder()
		h.groupAPIURL(w, r)
		return w.Code
	}
	if request(`{"apiUrl":"https://dc.apikl.ai"}`, "w") != 200 {
		t.Fatal("API route save failed")
	}
	site, _ := cache.Get(context.Background(), "s")
	if site.BaseURL != "https://apikl.ai" || site.Session.BaseURL != "https://apikl.ai" || site.Session.AccessToken != "private" || EffectiveAPIURL(*site, "g", "https://old.example") != "https://dc.apikl.ai" {
		t.Fatal("API route changed management identity")
	}
	if request(`{"apiUrl":"https://foreign.test"}`, "other") == 200 {
		t.Fatal("cross-workspace route modified")
	}
	if request(`{"apiUrl":"file:///tmp/file"}`, "w") == 200 {
		t.Fatal("invalid API URL accepted")
	}
	if _, e := s.UpdateSettings(context.Background(), "u", "s", SiteSettings{}); e != nil {
		t.Fatal(e)
	}
	site, _ = cache.Get(context.Background(), "s")
	if EffectiveAPIURL(*site, "g", "") != "https://dc.apikl.ai" {
		t.Fatal("threshold form erased API address")
	}
	if request(`{"apiUrl":""}`, "w") != 200 {
		t.Fatal("reset failed")
	}
	site, _ = cache.Get(context.Background(), "s")
	if EffectiveAPIURL(*site, "g", "https://account.example") != "https://apikl.ai" || EffectiveAPIURL(*site, "g", "") != "https://apikl.ai" {
		t.Fatal("explicit reset did not restore the configured site default")
	}
}
