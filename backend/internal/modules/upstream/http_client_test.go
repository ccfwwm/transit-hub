package upstream

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

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
