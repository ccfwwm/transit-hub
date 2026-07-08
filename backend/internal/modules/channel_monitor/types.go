package channel_monitor

import "time"

const (
	DefaultCheckIntervalMinutes  = 10
	DefaultFailureThreshold      = 3
	DefaultBalanceThreshold      = 1.0
	DefaultOpenAITestModel       = "gpt-5.4"
	DefaultAnthropicTestModel    = "claude-sonnet-4-6"
	DefaultBalanceRefreshMinutes = 5
)

const (
	StatusUnknown       = "unknown"
	StatusHealthy       = "healthy"
	StatusFailed        = "failed"
	StatusAutoPaused    = "auto_paused"
	StatusBalancePaused = "balance_paused"
	StatusManualPaused  = "manual_paused"
	StatusUnsupported   = "unsupported"
)

const (
	RateGateAllowed = "allowed"
	RateGateBlocked = "blocked"
	RateGateMissing = "missing"
	RateGateSkipped = "skipped"
)

type Rule struct {
	ID                   string     `json:"id"`
	UserID               string     `json:"-"`
	AdminAccountID       string     `json:"-"`
	ConnectionID         string     `json:"connectionId"`
	Enabled              bool       `json:"enabled"`
	CheckIntervalMinutes int        `json:"checkIntervalMinutes"`
	FailureThreshold     int        `json:"failureThreshold"`
	BalanceThreshold     float64    `json:"balanceThreshold"`
	DesiredSchedulable   *bool      `json:"desiredSchedulable"`
	ManualPaused         bool       `json:"manualPaused"`
	ConsecutiveFailures  int        `json:"consecutiveFailures"`
	LastStatus           string     `json:"lastStatus"`
	LastMessage          string     `json:"lastMessage"`
	LastLatencyMS        *int       `json:"lastLatencyMs"`
	LastCheckedAt        *time.Time `json:"lastCheckedAt"`
	NextCheckAt          *time.Time `json:"nextCheckAt"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type Result struct {
	ID           string    `json:"id"`
	RuleID       string    `json:"ruleId"`
	ConnectionID string    `json:"connectionId"`
	Status       string    `json:"status"`
	Success      bool      `json:"success"`
	Message      string    `json:"message"`
	LatencyMS    *int      `json:"latencyMs"`
	Model        string    `json:"model"`
	Action       string    `json:"action"`
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

type AccountTestOptions struct {
	ModelID string
	Prompt  string
}

type AccountTestResult struct {
	Success   bool
	Message   string
	LatencyMS int
	Model     string
}

type AdminAccountStatus struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Schedulable    *bool    `json:"schedulable"`
	RateMultiplier *float64 `json:"rateMultiplier"`
	Priority       *int     `json:"priority"`
}

type SummaryResponse struct {
	Stats           SummaryStats    `json:"stats"`
	Groups          []GroupSummary  `json:"groups"`
	Channels        []ChannelStatus `json:"channels"`
	RateRule        RateRuleView    `json:"rateRule"`
	TestModelConfig TestModelConfig `json:"testModelConfig"`
}

type SummaryStats struct {
	Total          int `json:"total"`
	Available      int `json:"available"`
	Failed         int `json:"failed"`
	BalancePaused  int `json:"balancePaused"`
	ManualPaused   int `json:"manualPaused"`
	MonitorPaused  int `json:"monitorPaused"`
	DispatchPaused int `json:"dispatchPaused"`
	Unsupported    int `json:"unsupported"`
}

type GroupSummary struct {
	GroupName      string     `json:"groupName"`
	Platform       string     `json:"platform"`
	Total          int        `json:"total"`
	Available      int        `json:"available"`
	Failed         int        `json:"failed"`
	BalancePaused  int        `json:"balancePaused"`
	ManualPaused   int        `json:"manualPaused"`
	MonitorPaused  int        `json:"monitorPaused"`
	DispatchPaused int        `json:"dispatchPaused"`
	LastCheckedAt  *time.Time `json:"lastCheckedAt"`
}

type ChannelStatus struct {
	RuleID                      string     `json:"ruleId"`
	ConnectionID                string     `json:"connectionId"`
	Enabled                     bool       `json:"enabled"`
	Supported                   bool       `json:"supported"`
	ManualPaused                bool       `json:"manualPaused"`
	Schedulable                 *bool      `json:"schedulable"`
	Status                      string     `json:"status"`
	SiteID                      string     `json:"siteId"`
	SiteName                    string     `json:"siteName"`
	SitePlatform                string     `json:"sitePlatform"`
	UpstreamGroupID             string     `json:"upstreamGroupId"`
	UpstreamGroupName           string     `json:"upstreamGroupName"`
	GroupType                   string     `json:"groupType"`
	AdminAccountID              string     `json:"adminAccountId"`
	AdminAccountName            string     `json:"adminAccountName"`
	OwnGroups                   []string   `json:"ownGroups"`
	Balance                     *float64   `json:"balance"`
	AccountRateMultiplier       *float64   `json:"accountRateMultiplier"`
	AccountPriority             *int       `json:"accountPriority"`
	UpstreamMultiplier          *float64   `json:"upstreamMultiplier"`
	UpstreamEffectiveMultiplier *float64   `json:"upstreamEffectiveMultiplier"`
	OwnGroupMultiplier          *float64   `json:"ownGroupMultiplier"`
	RecommendedPriority         *int       `json:"recommendedPriority"`
	RateGateStatus              string     `json:"rateGateStatus"`
	RateGateMessage             string     `json:"rateGateMessage"`
	CheckIntervalMinutes        int        `json:"checkIntervalMinutes"`
	FailureThreshold            int        `json:"failureThreshold"`
	BalanceThreshold            float64    `json:"balanceThreshold"`
	ConsecutiveFailures         int        `json:"consecutiveFailures"`
	LastMessage                 string     `json:"lastMessage"`
	LastLatencyMS               *int       `json:"lastLatencyMs"`
	LastCheckedAt               *time.Time `json:"lastCheckedAt"`
	NextCheckAt                 *time.Time `json:"nextCheckAt"`
	RecentResults               []Result   `json:"recentResults"`
	RecentTotal                 int        `json:"recentTotal"`
	RecentSuccess               int        `json:"recentSuccess"`
	UptimePercent               float64    `json:"uptimePercent"`
}

type UpdateRuleRequest struct {
	Enabled              *bool    `json:"enabled"`
	CheckIntervalMinutes *int     `json:"checkIntervalMinutes"`
	FailureThreshold     *int     `json:"failureThreshold"`
	BalanceThreshold     *float64 `json:"balanceThreshold"`
}

type BulkUpdateRuleRequest struct {
	RuleIDs              []string `json:"ruleIds"`
	Enabled              *bool    `json:"enabled"`
	CheckIntervalMinutes *int     `json:"checkIntervalMinutes"`
	FailureThreshold     *int     `json:"failureThreshold"`
	BalanceThreshold     *float64 `json:"balanceThreshold"`
}

type SetSchedulableRequest struct {
	Schedulable bool `json:"schedulable"`
}

type SetPriorityRequest struct {
	Priority int `json:"priority"`
}

type BulkSchedulableRequest struct {
	RuleIDs     []string `json:"ruleIds"`
	Schedulable bool     `json:"schedulable"`
}

type BulkRunRequest struct {
	RuleIDs []string `json:"ruleIds"`
}

type TestModelConfig struct {
	UserID                        string    `json:"-"`
	AdminAccountID                string    `json:"-"`
	OpenAIModelID                 string    `json:"openaiModelId"`
	AnthropicModelID              string    `json:"anthropicModelId"`
	BalanceRefreshIntervalMinutes int       `json:"balanceRefreshIntervalMinutes"`
	UpdatedAt                     time.Time `json:"updatedAt"`
}

type UpdateTestModelConfigRequest struct {
	OpenAIModelID                 *string `json:"openaiModelId"`
	AnthropicModelID              *string `json:"anthropicModelId"`
	BalanceRefreshIntervalMinutes *int    `json:"balanceRefreshIntervalMinutes"`
}

type RateRule struct {
	UserID              string     `json:"-"`
	AdminAccountID      string     `json:"-"`
	Enabled             bool       `json:"enabled"`
	AutoApplyOnCheck    bool       `json:"autoApplyOnCheck"`
	UpdatePriority      bool       `json:"updatePriority"`
	StopWhenMissingRate bool       `json:"stopWhenMissingRate"`
	LastAppliedAt       *time.Time `json:"lastAppliedAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type RateRuleView struct {
	Rule       RateRule         `json:"rule"`
	Summary    RateApplySummary `json:"summary"`
	Rows       []RatePlanRow    `json:"rows"`
	LastResult *RateApplyResult `json:"lastResult"`
}

type UpdateRateRuleRequest struct {
	Enabled             *bool `json:"enabled"`
	AutoApplyOnCheck    *bool `json:"autoApplyOnCheck"`
	UpdatePriority      *bool `json:"updatePriority"`
	StopWhenMissingRate *bool `json:"stopWhenMissingRate"`
}

type RateApplySummary struct {
	Total           int `json:"total"`
	Allowed         int `json:"allowed"`
	Blocked         int `json:"blocked"`
	Missing         int `json:"missing"`
	Skipped         int `json:"skipped"`
	WouldEnable     int `json:"wouldEnable"`
	WouldDisable    int `json:"wouldDisable"`
	PriorityChanges int `json:"priorityChanges"`
}

type RatePlanRow struct {
	RuleID                      string              `json:"ruleId"`
	ConnectionID                string              `json:"connectionId"`
	AdminAccountID              string              `json:"adminAccountId"`
	AdminAccountName            string              `json:"adminAccountName"`
	SiteName                    string              `json:"siteName"`
	UpstreamGroupName           string              `json:"upstreamGroupName"`
	OwnGroups                   []string            `json:"ownGroups"`
	GroupDecisions              []RateGroupDecision `json:"groupDecisions"`
	AccountRateMultiplier       *float64            `json:"accountRateMultiplier"`
	AccountPriority             *int                `json:"accountPriority"`
	UpstreamMultiplier          *float64            `json:"upstreamMultiplier"`
	UpstreamEffectiveMultiplier *float64            `json:"upstreamEffectiveMultiplier"`
	OwnGroupMultiplier          *float64            `json:"ownGroupMultiplier"`
	CurrentSchedulable          *bool               `json:"currentSchedulable"`
	SuggestedSchedulable        bool                `json:"suggestedSchedulable"`
	CurrentPriority             *int                `json:"currentPriority"`
	SuggestedPriority           *int                `json:"suggestedPriority"`
	RateGateStatus              string              `json:"rateGateStatus"`
	RateGateMessage             string              `json:"rateGateMessage"`
	Supported                   bool                `json:"supported"`
}

type RateGroupDecision struct {
	GroupName     string   `json:"groupName"`
	OwnMultiplier *float64 `json:"ownMultiplier"`
	Allowed       bool     `json:"allowed"`
	Message       string   `json:"message"`
}

type RateApplyResult struct {
	ID              string        `json:"id"`
	UserID          string        `json:"-"`
	AdminAccountID  string        `json:"-"`
	Action          string        `json:"action"`
	Success         bool          `json:"success"`
	Message         string        `json:"message"`
	Total           int           `json:"total"`
	EnabledCount    int           `json:"enabledCount"`
	DisabledCount   int           `json:"disabledCount"`
	PriorityUpdated int           `json:"priorityUpdated"`
	SkippedCount    int           `json:"skippedCount"`
	Rows            []RatePlanRow `json:"rows"`
	CreatedAt       time.Time     `json:"createdAt"`
}

func DefaultRateRule(userID, adminAccountID string) RateRule {
	now := time.Now()
	return RateRule{
		UserID:              userID,
		AdminAccountID:      adminAccountID,
		Enabled:             false,
		AutoApplyOnCheck:    true,
		UpdatePriority:      true,
		StopWhenMissingRate: true,
		UpdatedAt:           now,
	}
}

func DefaultTestModelConfig(userID, adminAccountID string) TestModelConfig {
	return TestModelConfig{
		UserID:                        userID,
		AdminAccountID:                adminAccountID,
		OpenAIModelID:                 DefaultOpenAITestModel,
		AnthropicModelID:              DefaultAnthropicTestModel,
		BalanceRefreshIntervalMinutes: DefaultBalanceRefreshMinutes,
		UpdatedAt:                     time.Now(),
	}
}

func DefaultRule(userID, adminAccountID, connectionID string) Rule {
	now := time.Now()
	next := now
	return Rule{
		ID:                   connectionID,
		UserID:               userID,
		AdminAccountID:       adminAccountID,
		ConnectionID:         connectionID,
		Enabled:              true,
		CheckIntervalMinutes: DefaultCheckIntervalMinutes,
		FailureThreshold:     DefaultFailureThreshold,
		BalanceThreshold:     DefaultBalanceThreshold,
		LastStatus:           StatusUnknown,
		NextCheckAt:          &next,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}
