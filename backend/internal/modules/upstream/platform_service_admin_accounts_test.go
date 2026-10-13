package upstream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestPriorityUpdateVerifiesPersistedAccount(t *testing.T) {
	for _, mode := range []string{"saved", "ignored", "missing", "wrong-account", "failed-read"} {
		t.Run(mode, func(t *testing.T) {
			calls := []string{}
			priority := 9
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method)
				if r.URL.Path != "/api/v1/admin/accounts/123" || r.Header.Get("Authorization") != "Bearer test" {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				if r.Method == http.MethodPut {
					var body map[string]int
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["priority"] != 2 {
						t.Errorf("unexpected write body: %v err=%v", body, err)
					}
					if mode != "ignored" {
						priority = body["priority"]
					}
					// Even a response claiming the desired value must be independently verified.
					writeJSON(w, map[string]any{"data": map[string]any{"id": 123, "priority": 2}})
					return
				}
				if mode == "failed-read" {
					w.WriteHeader(http.StatusServiceUnavailable)
					writeJSON(w, map[string]any{"error": "unavailable"})
					return
				}
				data := map[string]any{"id": 123, "priority": priority}
				if mode == "missing" {
					delete(data, "priority")
				}
				if mode == "wrong-account" {
					data["id"] = 456
				}
				writeJSON(w, map[string]any{"data": data})
			}))
			defer server.Close()
			err := NewPlatformService(NewHTTPClient(server.Client())).UpdateSub2APIAdminAccountPriority(Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "test"}, "123", 2)
			wantCalls := 2
			if mode == "failed-read" {
				wantCalls = 4 // The transient read failure is retried twice.
			}
			if (err == nil) != (mode == "saved") || len(calls) != wantCalls || calls[0] != "PUT" || calls[1] != "GET" {
				t.Fatalf("unverified priority accepted: mode=%s calls=%v err=%v", mode, calls, err)
			}
		})
	}
}

func TestPriorityUpdateRetriesOnlyTransientUpstreamFailures(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusTooManyRequests, http.StatusBadRequest, http.StatusUnauthorized} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			puts, gets := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					puts++
					if puts == 1 {
						w.WriteHeader(status)
						// Proxy failures may have a plain-text body.
						_, _ = w.Write([]byte("temporarily unavailable"))
						return
					}
				} else {
					gets++
					if gets == 1 {
						w.WriteHeader(http.StatusServiceUnavailable)
						writeJSON(w, map[string]any{"message": "temporarily unavailable"})
						return
					}
				}
				writeJSON(w, map[string]any{"data": map[string]any{"id": 123, "priority": 2}})
			}))
			defer server.Close()
			err := NewPlatformService(NewHTTPClient(server.Client())).UpdateSub2APIAdminAccountPriority(Session{Platform: PlatformSub2API, BaseURL: server.URL, AccessToken: "test"}, "123", 2)
			transient := status == http.StatusInternalServerError || status == http.StatusBadGateway || status == http.StatusTooManyRequests
			if transient && (err != nil || puts != 2 || gets != 2) {
				t.Fatalf("transient priority failure not recovered: puts=%d gets=%d err=%v", puts, gets, err)
			}
			if !transient && (err == nil || puts != 1 || gets != 0) {
				t.Fatalf("permanent error retried: puts=%d gets=%d err=%v", puts, gets, err)
			}
		})
	}
}

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
