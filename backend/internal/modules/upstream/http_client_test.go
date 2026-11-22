package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAccountExportsArePacedAcrossConcurrentRequests(t *testing.T) {
	const interval = 100 * time.Millisecond
	starts := make(chan time.Time, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts <- time.Now()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	client := NewHTTPClient(server.Client())
	client.SetAccountExportInterval(interval)
	path := server.URL + "/prefix/api/v1/admin/accounts/data?ids="
	results := make(chan error, 3)
	for _, id := range []string{"1", "2", "3"} {
		go func() {
			_, err := client.requestJSON(path+id, requestOptions{})
			results <- err
		}()
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	previous := <-starts
	for range 2 {
		next := <-starts
		if gap := next.Sub(previous); gap < interval-20*time.Millisecond {
			t.Fatalf("concurrent exports reached the server %v apart", gap)
		}
		previous = next
	}
	client.SetAccountExportInterval(time.Second)
	// Establish a full interval before testing cancellation of the next request.
	if _, err := client.requestJSON(path+"4", requestOptions{}); err != nil {
		t.Fatal(err)
	}
	<-starts
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := client.requestJSON(path+"5", requestOptions{Context: ctx}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued export ignored cancellation: %v", err)
	}
	if len(starts) != 0 {
		t.Fatal("cancelled export reached the server")
	}
}

func TestRequestJSONRetriesTransientGETFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	response, err := NewHTTPClient(server.Client()).requestJSON(server.URL, requestOptions{})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected two GET attempts, got %d", got)
	}
	if payload, ok := response.Payload.(map[string]any); !ok || payload["ok"] != true {
		t.Fatalf("unexpected response payload: %#v", response.Payload)
	}
}

func TestRequestJSONClassifiesHTTPFailuresWithoutRetryingAuthOrForbidden(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		errorKey string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, errorKey: ErrorAuth},
		{name: "forbidden HTML response", status: http.StatusForbidden, errorKey: ErrorForbidden},
		{name: "missing endpoint", status: http.StatusNotFound, errorKey: ErrorEndpointMissing},
		{name: "unsupported method", status: http.StatusMethodNotAllowed, errorKey: ErrorEndpointMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("<html>upstream rejected request</html>"))
			}))
			defer server.Close()

			_, err := NewHTTPClient(server.Client()).requestJSON(server.URL, requestOptions{})
			requestErr, ok := err.(*RequestError)
			if !ok {
				t.Fatalf("expected RequestError, got %v", err)
			}
			if requestErr.MessageKey != test.errorKey || requestErr.HTTPStatus != test.status {
				t.Fatalf("unexpected error classification: %+v", requestErr)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("expected no retry, got %d requests", got)
			}
		})
	}
}

func TestRequestJSONDoesNotRetryWritesOrLongRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		status     int
		retryAfter string
		errorKey   string
	}{
		{name: "POST service unavailable", method: http.MethodPost, status: http.StatusServiceUnavailable, errorKey: ErrorUnavailable},
		{name: "GET rate limit backoff", method: http.MethodGet, status: http.StatusTooManyRequests, retryAfter: "30", errorKey: ErrorRateLimited},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				http.Error(w, "temporary failure", test.status)
			}))
			defer server.Close()

			_, err := NewHTTPClient(server.Client()).requestJSON(server.URL, requestOptions{Method: test.method})
			requestErr, ok := err.(*RequestError)
			if !ok || requestErr.MessageKey != test.errorKey || requestErr.HTTPStatus != test.status {
				t.Fatalf("unexpected error classification: %#v", err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("expected one request, got %d", got)
			}
		})
	}
}
