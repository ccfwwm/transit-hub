package monitor_media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
)

type Adapter struct{ Client *http.Client }

func (a *Adapter) Submit(ctx context.Context, cap Capability, req Request, account Account) (UpstreamResult, error) {
	p := map[string]any{}
	for k, v := range req.Parameters {
		p[k] = v
	}
	path := submitPath(cap, req.Operation)
	switch req.Operation {
	case "image":
		path = submitPath(cap, req.Operation)
	case "image_edit":
		path = "/v1/images/edits"
	case "gemini_image":
		path = "/v1beta/models/" + url.PathEscape(cap.UpstreamModel) + ":generateContent"
		delete(p, "model")
	case "audio", "chat_music":
		if !isProtocol(cap.Adapter) || cap.Adapter == "audio_chat" {
			path = "/v1/chat/completions"
			if p["messages"] == nil {
				p["messages"] = []any{map[string]any{"role": "user", "content": p["prompt"]}}
			}
			delete(p, "prompt")
			p["stream"] = false
		}
	}
	for from, to := range cap.FieldMapping {
		if to != "" && !strings.ContainsAny(to, ":.[") && p[from] != nil && from != to {
			p[to] = p[from]
			delete(p, from)
		}
	}
	if cap.Adapter == "ark_native" {
		path = arkTaskPath(account.BaseURL)
		content := []any{}
		if meta, ok := p["metadata"].(map[string]any); ok {
			if native, ok := meta["content"].([]any); ok {
				content = append(content, native...)
			}
		}
		if prompt := stringValue(p["prompt"]); prompt != "" {
			content = append([]any{map[string]any{"type": "text", "text": prompt}}, content...)
		}
		if len(content) == 0 {
			return UpstreamResult{}, fmt.Errorf("ark requires content")
		}
		if meta, ok := p["metadata"].(map[string]any); !ok || meta["content"] == nil {
			for _, kind := range []string{"image", "video", "audio"} {
				for _, ref := range req.MaterialRefs[kind] {
					raw, _, err := refString(ref)
					if err != nil {
						return UpstreamResult{}, err
					}
					content = append(content, map[string]any{"type": kind + "_url", kind + "_url": map[string]any{"url": raw}})
				}
			}
		}
		native := map[string]any{"model": cap.UpstreamModel, "content": content}
		for _, key := range []string{"resolution", "ratio", "generate_audio", "seed"} {
			if p[key] != nil {
				native[key] = p[key]
			}
		}
		if req.Duration != "" {
			seconds, err := requestSeconds(req.Duration)
			if err != nil {
				return UpstreamResult{}, err
			}
			native["duration"] = seconds
		}
		p = native
	}
	var body io.Reader
	contentType := "application/json"
	if len(req.Uploads) > 0 {
		buf := &bytes.Buffer{}
		writer := multipart.NewWriter(buf)
		for key, value := range p {
			var text string
			if s, ok := value.(string); ok {
				text = s
			} else {
				raw, e := json.Marshal(value)
				if e != nil {
					return UpstreamResult{}, e
				}
				text = string(raw)
			}
			if e := writer.WriteField(key, text); e != nil {
				return UpstreamResult{}, e
			}
		}
		for _, u := range req.Uploads {
			header := textproto.MIMEHeader{}
			field := u.Field
			if cap.Adapter == "yunfei_gpt_images" && len(req.Uploads) > 1 && field == "image" {
				field = "image[]"
			}
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, u.Name))
			header.Set("Content-Type", u.MIME)
			part, e := writer.CreatePart(header)
			if e != nil {
				return UpstreamResult{}, e
			}
			if _, e = part.Write(u.Data); e != nil {
				return UpstreamResult{}, e
			}
		}
		if e := writer.Close(); e != nil {
			return UpstreamResult{}, e
		}
		contentType = writer.FormDataContentType()
		body = buf
	} else {
		raw, e := json.Marshal(p)
		if e != nil {
			return UpstreamResult{}, e
		}
		body = bytes.NewReader(raw)
	}
	return a.call(ctx, account, cap, http.MethodPost, path, body, contentType, true)
}
func (a *Adapter) Poll(ctx context.Context, t StoredTask, account Account) (UpstreamResult, error) {
	path := queryPath(t.Snapshot.Capability, url.PathEscape(t.UpstreamID))
	if t.Snapshot.Capability.Adapter == "ark_native" {
		path = arkTaskPath(account.BaseURL) + "/" + url.PathEscape(t.UpstreamID)
	}
	return a.call(ctx, account, t.Snapshot.Capability, http.MethodGet, path, nil, "", false)
}

// Recover only queries a known managed-media idempotency key. It never sends
// another generation request after an ambiguous submission.
func (a *Adapter) Recover(ctx context.Context, cap Capability, operation string, account Account) (UpstreamResult, error) {
	if cap.Adapter == "sub2api_speech" {
		operation = "speech"
	}
	path := "/v1/media/tasks/recover?" + url.Values{"operation": []string{operation}, "idempotency_key": []string{account.IdempotencyKey}}.Encode()
	return a.call(ctx, account, cap, http.MethodGet, path, nil, "", false)
}

func arkTaskPath(base string) string {
	u, err := url.Parse(base)
	if err == nil && (strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/api/v3") || strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v3")) {
		return "/contents/generations/tasks"
	}
	return "/api/v3/contents/generations/tasks"
}
func (a *Adapter) call(ctx context.Context, account Account, cap Capability, method, path string, body io.Reader, mime string, submission bool) (UpstreamResult, error) {
	target, e := originURL(account, path)
	if e != nil {
		return UpstreamResult{}, e
	}
	req, e := http.NewRequestWithContext(ctx, method, target, body)
	if e != nil {
		return UpstreamResult{}, e
	}
	req.Header.Set("Authorization", "Bearer "+account.Key)
	if cap.Adapter == "gemini_image" || cap.Adapter == "yunfei_gemini_native" {
		req.Header.Set("x-goog-api-key", account.Key)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TransitHub-monitor/1")
	if submission && account.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", account.IdempotencyKey)
	}
	if mime != "" {
		req.Header.Set("Content-Type", mime)
	}
	response, e := a.Client.Do(req)
	if e != nil {
		if submission && cap.MediaType == "image" {
			return UpstreamResult{Status: "failed", Diagnostic: "图片请求超时或连接中断，未取得图片结果"}, nil
		}
		if submission {
			return UpstreamResult{Status: "unknown", Diagnostic: "Submission transport failed; do not resubmit"}, nil
		}
		return UpstreamResult{}, fmt.Errorf("upstream query unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	raw, e := io.ReadAll(io.LimitReader(response.Body, (256<<20)+1))
	if e != nil || len(raw) > 256<<20 {
		if submission && cap.MediaType == "image" {
			return UpstreamResult{Status: "failed", Diagnostic: "图片响应中断或超过大小限制，未取得完整图片"}, nil
		}
		if submission {
			return UpstreamResult{Status: "unknown", Diagnostic: "Submission response incomplete; reconcile acceptance"}, nil
		}
		return UpstreamResult{}, fmt.Errorf("upstream query response incomplete")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if !submission {
			return UpstreamResult{}, fmt.Errorf("upstream query HTTP %d", response.StatusCode)
		}
		if cap.MediaType != "image" && (response.StatusCode >= 500 || response.StatusCode == 408 || response.StatusCode == 429) {
			return UpstreamResult{Status: "unknown", Diagnostic: fmt.Sprintf("Upstream HTTP %d; acceptance not confirmed", response.StatusCode)}, nil
		}
		return UpstreamResult{Status: "failed", Response: raw, Diagnostic: safeDiagnostic(fmt.Sprintf("HTTP %d", response.StatusCode), raw, account.Key)}, nil
	}
	if cap.MediaType == "image" && strings.HasPrefix(response.Header.Get("Content-Type"), "image/") {
		mime := strings.Split(response.Header.Get("Content-Type"), ";")[0]
		return UpstreamResult{Status: "completed", Artifacts: []Artifact{{Type: "image", MIME: mime, Source: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)}}}, nil
	}
	if strings.HasPrefix(response.Header.Get("Content-Type"), "audio/") {
		return UpstreamResult{Status: "completed", Artifacts: []Artifact{{Type: "audio", MIME: response.Header.Get("Content-Type"), Source: "data:" + response.Header.Get("Content-Type") + ";base64," + base64.StdEncoding.EncodeToString(raw)}}}, nil
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if e = decoder.Decode(&object); e != nil {
		if cap.MediaType == "image" {
			return UpstreamResult{Status: "failed", Diagnostic: "上游未返回可解析的图片结果"}, nil
		}
		return UpstreamResult{Status: "unknown", Diagnostic: "Non-JSON successful response; acceptance requires reconciliation"}, nil
	}
	result := parseResult(object, cap)
	result.Response = raw
	if cap.MediaType == "image" && result.Status == "unknown" {
		result.Status = "failed"
		result.Diagnostic = "上游未返回图片或有效生成任务"
	}
	if result.Status == "failed" && result.Diagnostic == "" {
		result.Diagnostic = safeDiagnostic("上游生成失败", raw, account.Key)
	}
	if result.Status == "unknown" {
		result.Diagnostic = "Unrecognized successful response; do not resubmit"
	}
	return result, nil
}
func parseResult(p map[string]any, cap Capability) UpstreamResult {
	if p["error"] != nil {
		return UpstreamResult{Status: "failed"}
	}
	if cap.Adapter == "newapi_suno" && stringValue(p["code"]) == "success" {
		if id, ok := p["data"].(string); ok && strings.TrimSpace(id) != "" {
			return UpstreamResult{ID: id, Status: "in_progress", Accepted: true}
		}
	}
	for _, field := range []string{"result", "output"} {
		if nested, ok := p[field].(map[string]any); ok {
			child := parseResult(nested, cap)
			if child.Status != "unknown" {
				if child.ID == "" {
					child.ID = stringValue(p["id"])
					if child.ID == "" {
						child.ID = stringValue(p["task_id"])
					}
				}
				if strings.EqualFold(stringValue(p["status"]), "failed") {
					child.Status = "failed"
				}
				return child
			}
		}
	}

	if inner, ok := p["data"].(map[string]any); ok && (inner["id"] != nil || inner["task_id"] != nil || inner["status"] != nil || inner["state"] != nil || inner["task_status"] != nil) {
		child := parseResult(inner, cap)
		if child.ID == "" {
			child.ID = stringValue(p["task_id"])
		}
		if child.ID == "" {
			child.ID = stringValue(p["id"])
		}
		return child
	}
	result := UpstreamResult{ID: stringValue(p["task_id"]), Status: "unknown", Quantities: map[string][]string{}}
	if result.ID == "" {
		result.ID = stringValue(p["id"])
	}
	if result.ID == "" {
		result.ID = stringValue(p["request_id"])
	}
	status := strings.ToLower(stringValue(p["status"]))
	if status == "" {
		status = strings.ToLower(stringValue(p["state"]))
	}
	if status == "" {
		status = strings.ToLower(stringValue(p["task_status"]))
	}
	switch status {
	case "queued", "queueing", "pending", "submitting", "submitted", "accepted", "not_start":
		result.Status = "in_progress"
	case "running", "processing", "in_progress":
		result.Status = "in_progress"
	case "succeeded", "success", "completed", "complete", "finished":
		result.Status = "completed"
	case "failed", "failure", "error", "cancelled", "canceled":
		result.Status = "failed"
	}
	if cap.Adapter == "openai_transcription" && strings.TrimSpace(stringValue(p["text"])) != "" {
		result.Status = "completed"
		result.Response, _ = json.Marshal(p)
		return result
	}
	seen := map[string]bool{}
	add := func(raw string, mime string) {
		if raw == "" || seen[raw] {
			return
		}
		if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "data:") {
			return
		}
		seen[raw] = true
		result.Artifacts = append(result.Artifacts, Artifact{Key: fmt.Sprintf("%s%d", cap.MediaType, len(result.Artifacts)+1), Type: cap.MediaType, MIME: mime, Source: raw})
	}
	for _, field := range []string{"url", "download_url", cap.MediaType + "_url", "content_url", "result_url"} {
		add(stringValue(p[field]), "")
	}
	if value := stringValue(p["b64_json"]); value != "" {
		add("data:image/png;base64,"+value, "image/png")
	}
	for _, field := range []string{"data", "images", "results", "videos", "audio", "audios", "artifacts", "content"} {
		if item, ok := p[field].(map[string]any); ok {
			if value := stringValue(item["b64_json"]); value != "" {
				add("data:image/png;base64,"+value, "image/png")
			}
			for _, key := range []string{"url", "download_url", "content_url", cap.MediaType + "_url"} {
				add(stringValue(item[key]), stringValue(item["mime_type"]))
			}
		}
		if list, ok := p[field].([]any); ok {
			for _, v := range list {
				switch item := v.(type) {
				case string:
					add(item, "")
				case map[string]any:
					if value := stringValue(item["b64_json"]); value != "" {
						mime := "image/png"
						if cap.MediaType == "audio" {
							mime = "audio/mpeg"
						}
						add("data:"+mime+";base64,"+value, mime)
					}
					for _, key := range []string{"url", "download_url", "content_url", cap.MediaType + "_url"} {
						add(stringValue(item[key]), stringValue(item["mime_type"]))
					}
				}
			}
		}
	}
	if choices, ok := p["choices"].([]any); ok {
		for _, choice := range choices {
			if obj, ok := choice.(map[string]any); ok {
				if message, ok := obj["message"].(map[string]any); ok {
					links := regexp.MustCompile(`https://[^\s)<>"\]]+`).FindAllString(stringValue(message["content"]), -1)
					for _, link := range links {
						add(link, "")
					}
				}
			}
		}
	}
	if candidates, ok := p["candidates"].([]any); ok {
		for _, candidate := range candidates {
			if obj, ok := candidate.(map[string]any); ok {
				content, _ := obj["content"].(map[string]any)
				parts, _ := content["parts"].([]any)
				for _, part := range parts {
					if object, ok := part.(map[string]any); ok {
						inline := object["inlineData"]
						if inline == nil {
							inline = object["inline_data"]
						}
						if v, ok := inline.(map[string]any); ok {
							mime := stringValue(v["mimeType"])
							if mime == "" {
								mime = stringValue(v["mime_type"])
							}
							add("data:"+mime+";base64,"+stringValue(v["data"]), mime)
						}
						for _, link := range regexp.MustCompile(`https://[^\s)<>"\]]+`).FindAllString(stringValue(object["text"]), -1) {
							add(link, "")
						}
					}
				}
			}
		}
	}
	if len(result.Artifacts) > 0 && result.Status == "unknown" {
		result.Status = "completed"
	}
	if result.Status == "completed" && len(result.Artifacts) == 0 && cap.MediaType == "video" && result.ID != "" && cap.Adapter != "ark_native" {
		add("/v1/videos/"+url.PathEscape(result.ID)+"/content", "video/mp4")
	}
	if result.ID != "" && result.Status == "unknown" && (cap.MediaType != "image" || p["task_id"] != nil || cap.Adapter == "sub2api_async_image") {
		result.Status = "in_progress"
	}
	if (result.Status == "in_progress" || result.Status == "queued") && result.ID == "" {
		result.Status = "unknown"
	}
	result.Accepted = result.ID != "" || len(result.Artifacts) > 0
	// Quantity names and JSON paths are administrator-configured, adapter-whitelisted.
	for key, path := range cap.UsageFields {
		if !strings.HasPrefix(key, "provider_tokens.") && key != "provider_duration" {
			continue
		}
		var value any = p
		for _, segment := range strings.Split(path, ".") {
			obj, ok := value.(map[string]any)
			if !ok {
				value = nil
				break
			}
			value = obj[segment]
		}
		if value != nil {
			if _, e := number(stringValue(value)); e == nil {
				result.Quantities[key] = []string{stringValue(value)}
			}
		}
	}
	return result
}

func safeDiagnostic(prefix string, raw []byte, key string) string {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return prefix
	}
	message := stringValue(payload["message"])
	if detail, ok := payload["error"].(map[string]any); ok {
		message = stringValue(detail["message"])
	} else if value, ok := payload["error"].(string); ok {
		message = value
	}
	if key != "" {
		message = strings.ReplaceAll(message, key, "[redacted]")
	}
	message = regexp.MustCompile(`(?i)(?:sk-|bearer\s+)[A-Za-z0-9_.-]{8,}`).ReplaceAllString(message, "[redacted]")
	runes := []rune(strings.Join(strings.Fields(message), " "))
	if len(runes) > 240 {
		runes = runes[:240]
	}
	if len(runes) > 0 {
		return prefix + "：" + string(runes)
	}
	return prefix
}
func decodeArtifact(source string) ([]byte, string, error) {
	pos := strings.Index(source, ",")
	if pos < 0 || !strings.Contains(source[:pos], ";base64") {
		return nil, "", fmt.Errorf("invalid inline artifact")
	}
	raw, e := base64.StdEncoding.DecodeString(source[pos+1:])
	return raw, strings.TrimSuffix(strings.TrimPrefix(source[:pos], "data:"), ";base64"), e
}
