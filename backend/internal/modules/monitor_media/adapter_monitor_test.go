package monitor_media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestImageSubmitUsesImageProtocolAndDecodableArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Method != "POST" {
			t.Error("incorrect image protocol", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["n"] != float64(1) {
			t.Error("image must generate exactly one", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}}})
	}))
	defer server.Close()
	cap, req, err := Resolve(Options{Model: "gpt-image-2", Mode: "image"})
	if err != nil {
		t.Fatal(err)
	}
	a := Adapter{Client: server.Client()}
	account := Account{BaseURL: server.URL, Key: "fixture"}
	result, err := a.Submit(context.Background(), cap, req, account)
	if err != nil || result.Status != "completed" {
		t.Fatal(result, err)
	}
	if err = ValidateMonitorArtifacts(context.Background(), server.Client(), cap, result.Artifacts, account); err != nil {
		t.Fatal(err)
	}
}

func TestVideoSubmitPollAndFFprobeValidation(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe unavailable")
	}
	file := filepath.Join(t.TempDir(), "tiny.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=blue:s=16x16:d=1", "-c:v", "mpeg4", "-y", file).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s %v", out, err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos":
			posts.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"id": "original", "status": "queued"})
		case "/v1/videos/original":
			json.NewEncoder(w).Encode(map[string]any{"id": "original", "status": "completed", "video_url": "/artifact"})
		case "/artifact":
			w.Header().Set("Content-Type", "video/mp4")
			w.Write(raw)
		default:
			t.Error("wrong protocol", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cap, req, err := Resolve(Options{Model: "custom-video", Mode: "video", Adapter: "v6k_video_gateway", Seconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	a := Adapter{Client: server.Client()}
	account := Account{BaseURL: server.URL, Key: "fixture"}
	result, err := a.Submit(context.Background(), cap, req, account)
	if err != nil || result.Status != "in_progress" || result.ID != "original" {
		t.Fatal(result, err)
	}
	result, err = a.Poll(context.Background(), StoredTask{ID: NewTaskID(), UpstreamID: result.ID, Snapshot: Snapshot{Capability: cap}}, account)
	if err != nil || result.Status != "completed" || posts.Load() != 1 {
		t.Fatal(result, err)
	}
	if err = ValidateMonitorArtifacts(context.Background(), server.Client(), cap, result.Artifacts, account); err != nil {
		t.Fatal(err)
	}
	raw = []byte("not a video")
	if err = ValidateMonitorArtifacts(context.Background(), server.Client(), cap, result.Artifacts, account); err == nil {
		t.Fatal("invalid video accepted")
	}
}

func TestSubmissionServerFailureRequiresReconciliation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer server.Close()
	cap, req, err := Resolve(Options{Model: "gpt-image-2", Mode: "image"})
	if err != nil {
		t.Fatal(err)
	}
	a := Adapter{Client: server.Client()}
	result, err := a.Submit(context.Background(), cap, req, Account{BaseURL: server.URL})
	if err != nil || result.Status != "unknown" {
		t.Fatal("ambiguous submission became retryable", result, err)
	}
}
