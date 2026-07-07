package my_sites

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"transithub/backend/internal/modules/upstream"
	"transithub/backend/internal/shared/httpjson"
)

func TestWriteErrorPassesThroughUpstreamAuthErrors(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeError(recorder, &upstream.RequestError{MessageKey: upstream.ErrorAuth, Platform: upstream.PlatformNewAPI})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected upstream auth error to be a bad request, got %d", recorder.Code)
	}
	var payload httpjson.ErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Message != upstream.ErrorAuth {
		t.Fatalf("expected upstream auth message, got %q", payload.Message)
	}
}
