package upstream

import (
	"context"
	"fmt"
	"net/http"
	"time"

	media "transithub/backend/internal/modules/monitor_media"
)

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
	adapter := media.Adapter{Client: client}
	account := media.Account{BaseURL: o.BaseURL, Key: o.APIKey}
	started := time.Now()
	var result media.UpstreamResult
	if o.TaskID != "" {
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
		if err = media.ValidateMonitorArtifacts(ctx, client, cap, result.Artifacts, account); err != nil {
			out.Message = "媒体产物校验失败：" + err.Error()
			return out, nil
		}
		out.Success = true
		out.Message = "媒体生成完成，产物校验通过"
	case "failed":
		out.Message = "媒体生成失败：" + result.Diagnostic
	case "unknown":
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
