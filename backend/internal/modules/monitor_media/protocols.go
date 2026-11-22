package monitor_media

import (
	_ "embed"
	"strings"
)

// Protocol selection is explicit. We never try successive billable endpoints
// to guess how an unfamiliar model works.
var protocolKinds = map[string]string{
	"sub2api_async_image": "image", "openai_image": "image",
	"gemini_image": "image",
	"openai_video": "video", "newapi_video": "video",
	"sub2api_audio": "audio", "sub2api_speech": "audio",
	"openai_tts": "audio", "openai_transcription": "audio", "audio_chat": "audio",
}

func ProtocolPresets() []Capability {
	ids := []string{"openai_image", "gemini_image", "sub2api_async_image", "openai_video", "newapi_video", "sub2api_audio", "sub2api_speech", "openai_tts", "openai_transcription", "audio_chat"}
	out := make([]Capability, 0, len(ids))
	for _, id := range ids {
		out = append(out, Capability{ID: "protocol:" + id, Model: "protocol:" + id, Adapter: id, MediaType: protocolKinds[id], Enabled: true, Status: "configured", Operations: []string{protocolKinds[id]}})
	}
	return out
}

func submitPath(cap Capability, operation string) string {
	switch cap.Adapter {
	case "sub2api_async_image":
		return "/v1/images/generations/async"
	case "sub2api_audio":
		return "/v1/audio/generations"
	case "sub2api_speech", "openai_tts":
		return "/v1/audio/speech"
	case "openai_transcription":
		return "/v1/audio/transcriptions"
	case "newapi_video":
		return "/v1/video/generations"
	case "audio_chat":
		return "/v1/chat/completions"
	}
	if operation == "image" {
		return "/v1/images/generations"
	}
	return "/v1/videos"
}
func queryPath(cap Capability, id string) string {
	switch cap.Adapter {
	case "sub2api_async_image":
		return "/v1/images/tasks/" + id
	case "sub2api_audio", "sub2api_speech":
		return "/v1/tasks/" + id
	case "newapi_video":
		return "/v1/video/generations/" + id
	}
	if cap.MediaType == "audio" {
		return "/v1/audio/generations/" + id
	}
	if cap.MediaType == "image" {
		return "/v1/images/tasks/" + id
	}
	return "/v1/videos/" + id
}
func isProtocol(id string) bool { _, ok := protocolKinds[id]; return ok }

// Fixed synthetic speech contains no user data and avoids treating a valid
// empty transcription of non-speech as a model failure.
//
//go:embed testdata/transcription.wav
var transcriptionWAV []byte

func testWAV() []byte               { return transcriptionWAV }
func protocolName(id string) string { return strings.TrimPrefix(id, "protocol:") }
