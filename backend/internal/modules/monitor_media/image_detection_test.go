package monitor_media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGPTImageFamiliesAndGeminiUseImageProtocols(t *testing.T) {
	for _, model := range []string{"gpt-image-2.5", "gpt-image-2.5-flare", "gpt-image-2.6-sunburst", "gemini-2.5-flash-image", "gemini-nano-banana-2.1", "gemini-3.1-flash-image-preview"} {
		t.Run(model, func(t *testing.T) {
			cap, req, e := Resolve(Options{Model: model, Mode: "image"})
			if e != nil {
				t.Fatal(e)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p map[string]any
				json.NewDecoder(r.Body).Decode(&p)
				if strings.HasPrefix(model, "gemini-") {
					if r.URL.Path != "/prefix/v1beta/models/"+model+":generateContent" || r.Header.Get("x-goog-api-key") != "fixture" || p["contents"] == nil || p["generationConfig"] == nil {
						t.Error("Gemini native protocol mismatch", r.URL.Path, p)
					}
					w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inline_data":{"mime_type":"image/png","data":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}}]}}]}`))
				} else {
					if r.URL.Path != "/prefix/v1/images/generations" || p["model"] != model || p["n"] != float64(1) {
						t.Error("GPT image protocol mismatch", r.URL.Path, p)
					}
					w.Write([]byte(`{"data":{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}}`))
				}
			}))
			defer server.Close()
			adapter := Adapter{Client: server.Client()}
			account := Account{BaseURL: server.URL + "/prefix/v1", Key: "fixture"}
			result, e := adapter.Submit(context.Background(), cap, req, account)
			if e != nil || result.Status != "completed" || len(result.Artifacts) != 1 {
				t.Fatal(result, e)
			}
			if e = ValidateMonitorArtifacts(context.Background(), server.Client(), cap, result.Artifacts, account); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestImageMissingOutputIsFailureAndTaskIDUsesImagePoll(t *testing.T) {
	for _, raw := range []string{`{"request_id":"trace-only"}`, `{"candidates":[{"finishReason":"SAFETY","content":{"parts":[{"text":"blocked"}]}}]}`, `{"error":{"message":"bad key sk-secretsecret"}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) }))
		cap, req, _ := Resolve(Options{Model: "gpt-image-2.5", Mode: "image"})
		a := Adapter{Client: server.Client()}
		result, e := a.Submit(context.Background(), cap, req, Account{BaseURL: server.URL, Key: "sk-secretsecret"})
		server.Close()
		if e != nil || result.Status != "failed" || strings.Contains(result.Diagnostic, "sk-secretsecret") {
			t.Fatal(result, e)
		}
	}
	cap, _, _ := Resolve(Options{Model: "gpt-image-2.5", Mode: "image"})
	if queryPath(cap, "original") != "/v1/images/tasks/original" {
		t.Fatal("image task used video polling route")
	}
}
