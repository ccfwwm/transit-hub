package monitor_media

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExactGeminiOmniVideoUsesOpenAIAsyncProtocol(t *testing.T) {
	cap, req, e := Resolve(Options{Model: "gemini-omni-1.2-flash-v", Mode: "video", Seconds: 4})
	if e != nil {
		t.Fatal(e)
	}
	if cap.Adapter != "openai_video" || submitPath(cap, req.Operation) != "/v1/videos" || queryPath(cap, "task") != "/v1/videos/task" || req.Parameters["seconds"] != "4" || req.Parameters["size"] != "1280x720" {
		t.Fatalf("wrong exact Omni request: %+v %+v", cap, req)
	}
	if _, _, e = Resolve(Options{Model: "gemini-omni-unknown", Mode: "video", Seconds: 4}); e == nil {
		t.Fatal("guessed unknown billable protocol")
	}
	result := parseResult(map[string]any{"id": "task-id", "status": "queued"}, cap)
	if result.Status != "in_progress" || result.ID != "task-id" || len(result.Artifacts) != 0 {
		t.Fatal("accepted task treated as success")
	}
	result = parseResult(map[string]any{"id": "task-id", "status": "completed"}, cap)
	if result.Status != "completed" || len(result.Artifacts) != 1 || result.Artifacts[0].Source != "/v1/videos/task-id/content" {
		t.Fatalf("missing binary content endpoint: %+v", result)
	}
}

func TestAsyncImageAndAudioProtocols(t *testing.T) {
	for _, tc := range []struct{ adapter, mode, submit, poll string }{
		{"sub2api_async_image", "image", "/v1/images/generations/async", "/v1/images/tasks/task"},
		{"sub2api_audio", "audio", "/v1/audio/generations", "/v1/tasks/task"},
		{"newapi_video", "video", "/v1/video/generations", "/v1/video/generations/task"},
	} {
		t.Run(tc.adapter, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					if r.URL.Path != tc.submit {
						t.Error(r.URL.Path)
					}
					if r.Header.Get("Idempotency-Key") != "once" {
						t.Error("missing idempotency key")
					}
					json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"task_id": "task", "status": "queued"}})
				} else {
					if r.URL.Path != tc.poll {
						t.Error(r.URL.Path)
					}
					json.NewEncoder(w).Encode(map[string]any{"id": "task", "status": "processing"})
				}
			}))
			defer server.Close()
			cap, req, e := Resolve(Options{Model: "custom", Mode: tc.mode, Adapter: tc.adapter, Seconds: 4})
			if e != nil {
				t.Fatal(e)
			}
			adapter := Adapter{Client: server.Client()}
			account := Account{BaseURL: server.URL, Key: "key", IdempotencyKey: "once"}
			r, e := adapter.Submit(context.Background(), cap, req, account)
			if e != nil || r.Status != "in_progress" || r.ID != "task" {
				t.Fatalf("submit %+v %v", r, e)
			}
			r, e = adapter.Poll(context.Background(), StoredTask{UpstreamID: r.ID, Snapshot: Snapshot{Capability: cap}}, account)
			if e != nil || r.Status != "in_progress" {
				t.Fatal(r, e)
			}
		})
	}
}
func TestSpeechBinaryAndTranscriptionMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/audio/speech" {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("binary-audio"))
			return
		}
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			t.Error(e)
		}
		file, header, e := r.FormFile("file")
		if e != nil {
			t.Error(e)
		} else {
			defer file.Close()
			content, err := io.ReadAll(file)
			if err != nil || header.Size < 16000 || header.Size > 160000 || len(content) < 44 || string(content[:4]) != "RIFF" || string(content[8:12]) != "WAVE" {
				t.Error("expected a short PCM speech fixture", header.Size, err)
			}
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "hello"})
	}))
	defer server.Close()
	adapter := Adapter{Client: server.Client()}
	for _, protocol := range []string{"openai_tts", "openai_transcription"} {
		cap, req, e := Resolve(Options{Model: "audio", Mode: "audio", Adapter: protocol})
		if e != nil {
			t.Fatal(e)
		}
		out, e := adapter.Submit(context.Background(), cap, req, Account{BaseURL: server.URL, Key: "key"})
		if e != nil || out.Status != "completed" {
			t.Fatal(out, e)
		}
		if protocol == "openai_tts" && (len(out.Artifacts) != 1 || !strings.HasPrefix(out.Artifacts[0].Source, "data:audio/mpeg;base64,")) {
			t.Fatal("binary audio discarded")
		}
	}
}
