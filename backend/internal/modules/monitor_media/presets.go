package monitor_media

import (
	_ "embed"
	"encoding/json"
)

//go:embed presets.json
var presetJSON []byte

// ModelPreset returns an independent copy: account overrides must never mutate
// the shared catalog. A preset describes a protocol, not a verification result.
func ModelPreset(model string) (Capability, bool) {
	var profiles []Capability
	_ = json.Unmarshal(presetJSON, &profiles)
	for _, c := range profiles {
		if c.Model == model {
			return CapabilityDefaults(c), true
		}
	}
	return Capability{}, false
}

func CapabilityDefaults(c Capability) Capability {
	if c.UpstreamModel == "" {
		c.UpstreamModel = c.Model
	}
	if c.ID == "" {
		c.ID = "manual:" + c.Model
	}
	if c.Version == "" {
		c.Version = "1"
	}
	if c.Status == "" {
		c.Status = "enabled"
	}
	if c.ValidationStatus == "" {
		c.ValidationStatus = "待渠道验证"
	}
	if c.Limits.MaxRequestBytes == nil {
		v := int64(32 << 20)
		c.Limits.MaxRequestBytes = &v
	}
	if c.Limits.MaxConcurrentTasks == nil {
		v := 2
		c.Limits.MaxConcurrentTasks = &v
	}
	if c.Limits.ObservationSeconds == nil {
		v := 3600
		c.Limits.ObservationSeconds = &v
	}
	if c.Delivery.PollSeconds == 0 {
		c.Delivery.PollSeconds = 10
	}
	if c.Materials == nil {
		c.Materials = map[string]Material{}
	}
	// Image-edit profiles created from an exact model mapping used to omit
	// material bounds entirely. Give the GPT multipart protocol bounded defaults;
	// an explicit zero remains an administrator's disabled-reference setting.
	if c.Adapter == "yunfei_gpt_images" && contains(c.Operations, "image_edit") {
		materials := make(map[string]Material, len(c.Materials)+1)
		for kind, material := range c.Materials {
			materials[kind] = material
		}
		c.Materials = materials
		m := c.Materials["image"]
		if m.MaxCount == nil {
			v := 2
			m.MaxCount = &v
		}
		if len(m.Formats) == 0 {
			m.Formats = []string{"file_png", "file_jpeg", "file_webp"}
		}
		c.Materials["image"] = m
	}
	for kind, m := range c.Materials {
		if m.MaxCount != nil && *m.MaxCount > 0 {
			if m.MaxFileBytes == nil {
				v := int64(32 << 20)
				m.MaxFileBytes = &v
			}
			if kind != "image" && m.MaxDuration == nil {
				v := "30"
				m.MaxDuration = &v
			}
			c.Materials[kind] = m
		}
	}
	// Resolution labels are distinct from the pixel-size parameter.
	if c.Adapter == "yunfei_gpt_images" && len(c.Sizes) == 0 {
		c.Sizes = []string{"1024x1024", "1536x1024", "1024x1536"}
	}
	return c
}

// RepairManualPlaceholder only replaces the old empty default, preserving
// credentials, enabled flags and every explicitly configured capability.
func RepairManualPlaceholder(c Capability) Capability {
	if c.Provider == "manual" && c.Adapter == "v6k_video_gateway" && c.Duration.Mode == "unknown" && len(c.Duration.Values) == 0 && c.Duration.Min == nil && c.Duration.Max == nil && c.Duration.Default == nil && !c.Duration.Required && len(c.Resolutions) == 0 && len(c.Sizes) == 0 && len(c.Qualities) == 0 && len(c.Materials) == 0 {
		if preset, ok := ModelPreset(c.UpstreamModel); ok {
			preset.Model, preset.UpstreamModel, preset.ID, preset.Enabled = c.Model, c.UpstreamModel, c.ID, c.Enabled
			preset.ValidationStatus, preset.VerificationEvidence = c.ValidationStatus, c.VerificationEvidence
			preset.Limits, preset.Delivery = c.Limits, c.Delivery
			if len(c.AspectRatios) > 0 {
				preset.AspectRatios = c.AspectRatios
			}
			if len(c.ArtifactHosts) > 0 {
				preset.ArtifactHosts = c.ArtifactHosts
			}
			if len(c.UsageFields) > 0 {
				preset.UsageFields = c.UsageFields
			}
			if len(c.FieldMapping) > 0 {
				preset.FieldMapping = c.FieldMapping
			}
			if len(c.Constraints) > 0 {
				preset.Constraints = c.Constraints
			}
			if len(c.Scenarios) > 0 {
				preset.Scenarios = c.Scenarios
			}
			if c.Source != "" {
				preset.Source = c.Source
			}
			c = preset
		}
	}
	return CapabilityDefaults(c)
}
