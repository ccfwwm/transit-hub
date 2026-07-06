package httpserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticHandlerDoesNotFallbackMissingAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	rec := httptest.NewRecorder()
	staticHandler(dir).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStaticHandlerFallbackHtmlIsNotCached(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/status-monitor", nil)
	rec := httptest.NewRecorder()
	staticHandler(dir).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("history fallback status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("Cache-Control = %q, want no-cache fallback", got)
	}
}
