package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeAPIKeyUsesOpenAIResponsesGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-monitor" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "gpt-monitor" {
			t.Fatalf("model = %#v", body["model"])
		}
		if _, ok := body["input"]; !ok {
			t.Fatalf("responses payload missing input: %#v", body)
		}
		if _, ok := body["messages"]; ok {
			t.Fatalf("responses payload unexpectedly contains messages: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"content":[{"type":"output_text","text":"OK"}]}]}`))
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	result, err := service.ProbeAPIKey(context.Background(), APIKeyProbeOptions{
		BaseURL: server.URL, APIKey: "sk-monitor", Platform: "openai", ModelID: "gpt-monitor",
	})
	if err != nil || !result.Success || result.Model != "gpt-monitor" {
		t.Fatalf("ProbeAPIKey = %+v, %v", result, err)
	}
}

func TestProbeAPIKeyFallsBackToChatCompletionsWhenResponsesUnsupported(t *testing.T) {
	paths := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v1/responses":
			w.WriteHeader(http.StatusMethodNotAllowed)
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if _, ok := body["messages"]; !ok {
				t.Fatalf("chat completions payload missing messages: %#v", body)
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	result, err := service.ProbeAPIKey(context.Background(), APIKeyProbeOptions{
		BaseURL: server.URL + "/v1", APIKey: "sk-monitor", Platform: "openai", ModelID: "gpt-monitor",
	})
	if err != nil || !result.Success {
		t.Fatalf("ProbeAPIKey = %+v, %v", result, err)
	}
	if len(paths) != 2 || paths[0] != "/v1/responses" || paths[1] != "/v1/chat/completions" {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestProbeAPIKeyKeepsGrokOnChatCompletions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	result, err := service.ProbeAPIKey(context.Background(), APIKeyProbeOptions{
		BaseURL: server.URL, APIKey: "xai-monitor", Platform: "grok", ModelID: "grok-4.5",
	})
	if err != nil || !result.Success {
		t.Fatalf("ProbeAPIKey = %+v, %v", result, err)
	}
}

func TestProbeAPIKeyUsesAnthropicGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant-monitor" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("anthropic-version = %q", got)
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"OK"}]}`))
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	result, err := service.ProbeAPIKey(context.Background(), APIKeyProbeOptions{
		BaseURL: server.URL + "/v1", APIKey: "sk-ant-monitor", Platform: "claude", ModelID: "claude-monitor",
	})
	if err != nil || !result.Success {
		t.Fatalf("ProbeAPIKey = %+v, %v", result, err)
	}
}

func TestProbeAPIKeyReturnsSanitizedUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model is unavailable"}}`))
	}))
	defer server.Close()

	service := NewPlatformService(NewHTTPClient(server.Client()))
	result, err := service.ProbeAPIKey(context.Background(), APIKeyProbeOptions{
		BaseURL: server.URL, APIKey: "secret-must-not-leak", Platform: "grok", ModelID: "grok-monitor",
	})
	if err != nil || result.Success {
		t.Fatalf("ProbeAPIKey = %+v, %v", result, err)
	}
	if result.Message != "上游 Key 直连检测失败（HTTP 400）：model is unavailable" {
		t.Fatalf("message = %q", result.Message)
	}
}
