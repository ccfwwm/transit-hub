package monitor_media

import (
	"encoding/json"
	"fmt"
	"time"
)

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string                    { return e.Code + ": " + e.Message }
func fail(status int, code, message string) error { return &Error{status, code, message} }

type Duration struct {
	Mode      string           `json:"mode"`
	Values    []int            `json:"values"`
	Min       *int             `json:"min"`
	Max       *int             `json:"max"`
	Default   *int             `json:"default"`
	Required  bool             `json:"required"`
	Scenarios []map[string]any `json:"scenarios"`
}
type Material struct {
	Formats      []string `json:"formats"`
	MaxCount     *int     `json:"max_count"`
	MaxFileBytes *int64   `json:"max_file_bytes"`
	MaxDuration  *string  `json:"max_duration_seconds"`
}
type Capability struct {
	ID               string              `json:"profile_id"`
	Version          string              `json:"version"`
	Model            string              `json:"model"`
	UpstreamModel    string              `json:"upstream_model"`
	Provider         string              `json:"provider"`
	Adapter          string              `json:"adapter"`
	MediaType        string              `json:"media_type"`
	Enabled          bool                `json:"enabled"`
	Status           string              `json:"status"`
	ValidationStatus string              `json:"validation_status"`
	Source           string              `json:"source"`
	Scenarios        []string            `json:"scenarios"`
	Operations       []string            `json:"operations"`
	Formats          []string            `json:"formats"`
	Duration         Duration            `json:"duration"`
	Resolutions      []string            `json:"resolutions"`
	Sizes            []string            `json:"sizes,omitempty"`
	AspectRatios     []string            `json:"aspect_ratios"`
	Qualities        []string            `json:"qualities"`
	ResolutionPolicy string              `json:"resolution_policy"`
	Materials        map[string]Material `json:"materials"`
	FieldMapping     map[string]string   `json:"field_mapping"`
	Constraints      []map[string]any    `json:"constraints"`
	Unverified       []string            `json:"unverified"`
	Delivery         struct {
		PollSeconds int   `json:"poll_seconds"`
		Head        *bool `json:"head"`
		Range       *bool `json:"range"`
		Refresh     *bool `json:"refresh"`
	} `json:"delivery"`
	Limits struct {
		MaxRequestBytes    *int64 `json:"max_request_bytes"`
		MaxConcurrentTasks *int   `json:"max_concurrent_tasks"`
		ObservationSeconds *int   `json:"observation_seconds"`
	} `json:"limits"`
	// Bindings reference existing account credentials. Secrets never enter snapshots.
	AccountIDs           []int64           `json:"account_ids"`
	GroupIDs             []int64           `json:"group_ids"`
	ChannelID            int64             `json:"channel_id,omitempty"`
	PriceTemplates       []string          `json:"price_templates,omitempty"`
	CostTemplates        []string          `json:"cost_templates,omitempty"`
	MusicRateOverride    *string           `json:"music_rate_override,omitempty"`
	VerificationEvidence string            `json:"verification_evidence,omitempty"`
	UsageFields          map[string]string `json:"usage_fields,omitempty"`
	ArtifactHosts        []string          `json:"artifact_hosts,omitempty"`
}
type Rounding struct {
	Mode    string `json:"mode"`
	Step    string `json:"step"`
	Minimum string `json:"minimum"`
	Scope   string `json:"scope"`
}
type Tier struct {
	UpTo  *string `json:"up_to"`
	Price *string `json:"unit_price"`
}
type Charge struct {
	Name            string   `json:"name"`
	Unit            string   `json:"unit"`
	Price           *string  `json:"unit_price"`
	QuantitySource  string   `json:"quantity_source"`
	Fallback        *string  `json:"fallback_source"`
	UsageField      *string  `json:"usage_field"`
	Trigger         string   `json:"trigger"`
	Refundable      bool     `json:"refundable"`
	Rounding        Rounding `json:"rounding"`
	Method          string   `json:"method"`
	PackageQuantity *string  `json:"package_quantity"`
	Tiers           []Tier   `json:"tiers"`
}
type Rule struct {
	Priority int            `json:"priority"`
	When     map[string]any `json:"when"`
	Charges  []Charge       `json:"charges"`
}
type Request struct {
	MaterialRefs map[string][]any    `json:"material_refs,omitempty"`
	Uploads      []Upload            `json:"uploads,omitempty"`
	Operation    string              `json:"operation"`
	Parameters   map[string]any      `json:"request"`
	Model        string              `json:"model"`
	Duration     string              `json:"duration"`
	Conditions   map[string]any      `json:"conditions"`
	Quantities   map[string][]string `json:"quantities"`
	InputImages  int                 `json:"input_images"`
}
type Upload struct {
	Field string `json:"field"`
	Name  string `json:"name"`
	MIME  string `json:"mime"`
	Data  []byte `json:"data"`
}
type Artifact struct {
	Key        string  `json:"key"`
	Type       string  `json:"type"`
	MIME       string  `json:"mime_type"`
	Size       *int64  `json:"size_bytes"`
	Duration   *string `json:"duration_seconds"`
	ContentURL string  `json:"content_url"`
	Available  bool    `json:"available"`
	// Upstream locations are internal only, never rendered in the task response.
	Source string `json:"-"`
}
type UpstreamResult struct {
	ID, Status, Diagnostic string
	Response               json.RawMessage
	Artifacts              []Artifact
	Quantities             map[string][]string
	Accepted               bool
}
type Account struct {
	CostMultiplier         string
	ID                     int64
	BaseURL, Key, Platform string
	Allowed                bool
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

type Snapshot struct{ Capability Capability }
type StoredTask struct {
	ID, UpstreamID string
	Snapshot       Snapshot
}

func NewTaskID() string { return fmt.Sprintf("mt_%d", time.Now().UnixNano()) }
