package upstream

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefreshSession_KeepsAccessTokenWhenRefreshTokenRejected(t *testing.T) {
	var refreshCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshCalled = true
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]any{"error": "invalid refresh token"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	expired := time.Now().Add(-time.Hour).UnixMilli()
	session := Session{
		Platform:     PlatformSub2API,
		BaseURL:      server.URL,
		AccessToken:  "still-valid-access-token",
		RefreshToken: "expired-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    &expired,
	}
	service := NewPlatformService(NewHTTPClient(server.Client()))

	refreshed, err := service.RefreshSession(session)
	if err != nil {
		t.Fatalf("expected refresh failure to keep existing access token, got err: %v", err)
	}
	if !refreshCalled {
		t.Fatal("expected refresh endpoint to be called for expired session")
	}
	if refreshed.AccessToken != session.AccessToken {
		t.Fatalf("expected access token to be preserved, got %q", refreshed.AccessToken)
	}
	if refreshed.RefreshToken != "" {
		t.Fatalf("expected rejected refresh token to be cleared, got %q", refreshed.RefreshToken)
	}
	if refreshed.ExpiresAt != nil {
		t.Fatalf("expected stale expiry to be cleared, got %v", *refreshed.ExpiresAt)
	}
}
