package upstream

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	media "transithub/backend/internal/modules/monitor_media"
)

// FetchAPIKeyModelIDs reads the model directory exposed to this exact Key.
// A directory entry is not health evidence and never triggers a generation.
func (s *PlatformService) FetchAPIKeyModelIDs(ctx context.Context, o APIKeyProbeOptions) ([]string, error) {
	r, err := s.httpClient.requestJSON(apiProbeURL(o.BaseURL, "/v1/models"), requestOptions{Context: ctx, AccessToken: o.APIKey, TokenType: "Bearer", InsecureSkipTLS: o.InsecureSkipTLS})
	if err != nil {
		return nil, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, item := range dataArray(r.Payload) {
		if m, ok := item.(map[string]any); ok {
			if id := safeString(m, "id"); id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *PlatformService) probeMedia(ctx context.Context, o APIKeyProbeOptions) (APIKeyProbeResult, error) {
	cap, req, err := media.Resolve(media.Options{Model: o.ModelID, Mode: o.Mode, Prompt: o.Prompt, Size: o.Size, Seconds: o.Seconds, Adapter: o.Adapter, PresetModel: o.PresetModel})
	if err != nil {
		return APIKeyProbeResult{}, err
	}
	client := media.SafeClient(false)
	// A configured origin can be a local integration gateway. Production
	// artifact downloads still use the public-only client and drop credentials
	// when redirected away from the configured origin.
	if o.InsecureSkipTLS {
		client = s.httpClient.requestClient(true)
	}
	if o.Mode == "image" {
		copy := *client
		client = &copy
		client.Timeout = 8 * time.Minute
		if transport, ok := client.Transport.(*http.Transport); ok {
			transport = transport.Clone()
			transport.ResponseHeaderTimeout = 8 * time.Minute
			client.Transport = transport
		}
	}
	adapter := media.Adapter{Client: client}
	account := media.Account{BaseURL: o.BaseURL, Key: o.APIKey, IdempotencyKey: o.IdempotencyKey}
	started := time.Now()
	var result media.UpstreamResult
	if o.Recover {
		result, err = adapter.Recover(ctx, cap, req.Operation, account)
	} else if o.TaskID != "" {
		result, err = adapter.Poll(ctx, media.StoredTask{ID: media.NewTaskID(), UpstreamID: o.TaskID, Snapshot: media.Snapshot{Capability: cap}}, account)
	} else {
		result, err = adapter.Submit(ctx, cap, req, account)
	}
	out := APIKeyProbeResult{Model: o.ModelID, TaskID: result.ID, LatencyMS: int(time.Since(started).Milliseconds())}
	if out.TaskID == "" {
		out.TaskID = o.TaskID
	}
	if err != nil {
		return out, err
	}
	switch result.Status {
	case "completed":
		if cap.Adapter == "openai_transcription" {
			out.Success = true
			out.Message = "音频转写完成，返回有效文本"
			break
		}
		artifacts := result.Artifacts
		if cap.Adapter == "newapi_suno" && len(artifacts) > 1 {
			artifacts = artifacts[:1]
		}
		if err = media.ValidateMonitorArtifacts(ctx, client, cap, artifacts, account); err != nil {
			out.Message = "媒体产物校验失败：" + err.Error()
			return out, nil
		}
		out.Success = true
		out.Message = "媒体生成完成，产物校验通过"
	case "failed":
		out.Inconclusive = strings.Contains(result.Diagnostic, "HTTP 400") || strings.Contains(result.Diagnostic, "HTTP 401") || strings.Contains(result.Diagnostic, "HTTP 403") || strings.Contains(result.Diagnostic, "HTTP 404") || strings.Contains(result.Diagnostic, "HTTP 422")
		out.Message = "媒体生成失败：" + result.Diagnostic
	case "unknown":
		if o.Mode == "image" {
			out.Message = "图片检测失败：未收到有效图片结果"
			break
		}
		out.Uncertain = true
		out.Message = "受理结果不明确，已阻止自动重发；请确认上游任务后显式重置"
	default:
		out.Pending = true
		out.Message = "媒体生成中"
	}
	return out, nil
}

func (s *PlatformService) probeConnection(ctx context.Context, o APIKeyProbeOptions) (APIKeyProbeResult, error) {
	endpoint := apiProbeURL(o.BaseURL, "/v1/models")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return APIKeyProbeResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	response, err := s.httpClient.requestClient(o.InsecureSkipTLS).Do(req)
	if err != nil {
		return APIKeyProbeResult{}, err
	}
	defer response.Body.Close()
	return APIKeyProbeResult{Success: response.StatusCode >= 200 && response.StatusCode < 300, Model: o.ModelID, Message: fmt.Sprintf("连接检查 HTTP %d；未执行生成", response.StatusCode)}, nil
}
