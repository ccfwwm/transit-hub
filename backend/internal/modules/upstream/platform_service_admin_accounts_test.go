package upstream

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestAdminAccountsReadsServerLimitedPages(t *testing.T) {
	for _, withTotal := range []bool{true, false} {
		t.Run(strconv.FormatBool(withTotal), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				items := []map[string]any{}
				for id := (page-1)*2 + 1; id <= page*2 && id <= 5; id++ {
					items = append(items, map[string]any{"id": id, "schedulable": id != 5, "priority": id, "rate_multiplier": 0.2})
				}
				data := map[string]any{"items": items, "page_size": 2}
				if withTotal {
					data["total"] = 5
				}
				writeJSON(w, map[string]any{"data": data})
			}))
			defer server.Close()
			service := NewPlatformService(NewHTTPClient(server.Client()))
			rows, err := service.ListSub2APIAdminAccounts(Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "test"})
			if err != nil || len(rows) != 5 || calls != 3 {
				t.Fatalf("rows=%v calls=%d err=%v", rows, calls, err)
			}
			if rows[4].ID != "5" || rows[4].Schedulable == nil || *rows[4].Schedulable || *rows[4].Priority != 5 {
				t.Fatalf("last account lost: %+v", rows[4])
			}
		})
	}
}

func TestAdminAccountsRejectsIncompleteOrInvalidLists(t *testing.T) {
	for _, mode := range []string{"error", "repeated", "empty", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page := r.URL.Query().Get("page")
				if mode == "malformed" {
					writeJSON(w, map[string]any{"data": map[string]any{"unexpected": true}})
					return
				}
				if page == "2" && mode == "error" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				items := []map[string]any{{"id": 1, "schedulable": true}}
				if page == "2" && mode == "empty" {
					items = []map[string]any{}
				}
				writeJSON(w, map[string]any{"data": map[string]any{"items": items, "total": 2, "page_size": 1}})
			}))
			defer server.Close()
			service := NewPlatformService(NewHTTPClient(server.Client()))
			rows, err := service.ListSub2APIAdminAccounts(Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "test"})
			if err == nil || rows != nil {
				t.Fatalf("incomplete list accepted: %+v err=%v", rows, err)
			}
		})
	}
}
