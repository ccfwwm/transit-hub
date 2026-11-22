// This module ports the media protocol presets and artifact checks used by
// Sub2API account monitoring. It has no runtime dependency on that checkout.
package monitor_media

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Options struct {
	Model, Mode, Prompt, Size, Adapter, PresetModel string
	Seconds                                         int
}

func Resolve(o Options) (Capability, Request, error) {
	preset := o.PresetModel
	if preset == "" {
		preset = o.Model
	}
	cap, known := ModelPreset(preset)
	if !known && o.Mode == "image" {
		name := strings.ToLower(o.Model)
		if strings.HasPrefix(name, "gpt-image-") || name == "chatgpt-image-latest" {
			cap = Capability{Model: o.Model, UpstreamModel: o.Model, MediaType: "image", Adapter: "openai_image"}
			known = true
		} else if strings.HasPrefix(name, "gemini-") && (strings.Contains(name, "image") || strings.Contains(name, "nano-banana")) {
			cap = Capability{Model: o.Model, UpstreamModel: o.Model, MediaType: "image", Adapter: "gemini_image"}
			known = true
		}
	}
	if isProtocol(protocolName(preset)) {
		o.Adapter = protocolName(preset)
		known = false
	}
	if !known && o.Adapter == "" {
		return Capability{}, Request{}, fmt.Errorf("未知媒体模型，请选择协议适配类型或精确预设")
	}
	if !known {
		cap = Capability{Model: o.Model, UpstreamModel: o.Model, MediaType: o.Mode, Adapter: o.Adapter}
	}
	if cap.MediaType != o.Mode {
		return cap, Request{}, fmt.Errorf("预设类型与检测类型不一致")
	}
	if o.Adapter != "" {
		cap.Adapter = o.Adapter
	}
	if !isProtocol(cap.Adapter) && !contains([]string{"yunfei_gpt_images", "yunfei_gemini_native", "yunfei_grok", "yunfei_veo_preview", "yunfei_omni", "v6k_video_gateway", "ark_native"}, cap.Adapter) {
		return cap, Request{}, fmt.Errorf("不支持的媒体协议")
	}
	cap.Model = o.Model
	cap.UpstreamModel = o.Model
	if kind, ok := protocolKinds[cap.Adapter]; ok && kind != o.Mode {
		return cap, Request{}, fmt.Errorf("协议与检测类型不一致")
	}
	prompt := strings.TrimSpace(o.Prompt)
	if prompt == "" {
		prompt = "A simple blue circle on a plain white background."
		if o.Mode == "video" {
			prompt = "A fixed camera watching a blue ball slowly roll across a plain white floor."
		}
		if o.Mode == "audio" {
			prompt = "Hello."
		}
	}
	p := map[string]any{"model": o.Model, "prompt": prompt}
	operation := o.Mode
	if cap.Adapter == "openai_tts" || cap.Adapter == "sub2api_speech" {
		p = map[string]any{"model": o.Model, "input": prompt, "voice": "alloy", "response_format": "mp3"}
	}
	if o.Mode == "image" {
		p["n"] = 1
		size := o.Size
		if size == "" && len(cap.Sizes) > 0 {
			sizes := append([]string(nil), cap.Sizes...)
			sort.SliceStable(sizes, func(i, j int) bool { return pixelArea(sizes[i]) < pixelArea(sizes[j]) })
			size = sizes[0]
		}
		if size != "" {
			if len(cap.Sizes) > 0 && !contains(cap.Sizes, size) {
				return cap, Request{}, fmt.Errorf("图片规格不在预设支持范围")
			}
			p["size"] = size
		}
		if len(cap.Qualities) > 0 {
			p["quality"] = cap.Qualities[0]
		}
		if cap.Adapter == "yunfei_gemini_native" || cap.Adapter == "gemini_image" {
			operation = "gemini_image"
			p = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": prompt}}}}, "generationConfig": map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}}
			if o.Size != "" {
				p["generationConfig"].(map[string]any)["imageConfig"] = map[string]any{"imageSize": o.Size}
			}
		}
	}
	seconds := o.Seconds
	if o.Mode == "video" {
		if seconds == 0 {
			for _, v := range cap.Duration.Values {
				if v > 0 && (seconds == 0 || v < seconds) {
					seconds = v
				}
			}
			if cap.Duration.Min != nil {
				seconds = *cap.Duration.Min
			}
		}
		if seconds == 0 {
			return cap, Request{}, fmt.Errorf("自定义视频协议需要指定最短有效时长")
		}
		if (len(cap.Duration.Values) > 0 && !containsInt(cap.Duration.Values, seconds)) || (cap.Duration.Min != nil && seconds < *cap.Duration.Min) || (cap.Duration.Max != nil && seconds > *cap.Duration.Max) {
			return cap, Request{}, fmt.Errorf("视频时长不在预设支持范围")
		}
		p["seconds"] = seconds
		if cap.Adapter == "openai_video" {
			p["seconds"] = strconv.Itoa(seconds)
		}
		if o.Size != "" {
			p["size"] = o.Size
		} else if len(cap.Sizes) > 0 {
			p["size"] = cap.Sizes[0]
		}
		if len(cap.Resolutions) > 0 {
			p["resolution"] = cap.Resolutions[0]
		}
	}
	duration := ""
	if seconds > 0 {
		duration = strconv.Itoa(seconds)
	}
	req := Request{Operation: operation, Model: o.Model, Duration: duration, Parameters: p}
	if cap.Adapter == "openai_transcription" {
		delete(p, "prompt")
		req.Uploads = []Upload{{Field: "file", Name: "monitor.wav", MIME: "audio/wav", Data: testWAV()}}
	}
	return cap, req, nil
}

func containsInt(v []int, n int) bool {
	for _, x := range v {
		if x == n {
			return true
		}
	}
	return false
}
func pixelArea(s string) int {
	var w, h int
	if _, e := fmt.Sscanf(s, "%dx%d", &w, &h); e != nil {
		return 1 << 30
	}
	return w * h
}

func Presets() []Capability {
	var profiles []Capability
	_ = json.Unmarshal(presetJSON, &profiles)
	profiles = append(profiles, omniVideoPresets()...)
	return append(profiles, ProtocolPresets()...)
}
