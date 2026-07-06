package channel_monitor

import "time"

const (
	DefaultCheckIntervalMinutes = 10
	DefaultFailureThreshold     = 3
	DefaultBalanceThreshold     = 1.0
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

type Rule struct {
	ID                   string     `json:"id"`
	UserID               string     `json:"-"`
	AdminAccountID       string     `json:"-"`
	ConnectionID         string     `json:"connectionId"`
	Enabled              bool       `json:"enabled"`
	CheckIntervalMinutes int        `json:"checkIntervalMinutes"`
	FailureThreshold     int        `json:"failureThreshold"`
	BalanceThreshold     float64    `json:"balanceThreshold"`
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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Schedulable *bool  `json:"schedulable"`
}

type SummaryResponse struct {
	Stats    SummaryStats    `json:"stats"`
	Groups   []GroupSummary  `json:"groups"`
	Channels []ChannelStatus `json:"channels"`
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
	RuleID               string     `json:"ruleId"`
	ConnectionID         string     `json:"connectionId"`
	Enabled              bool       `json:"enabled"`
	Supported            bool       `json:"supported"`
	ManualPaused         bool       `json:"manualPaused"`
	Schedulable          *bool      `json:"schedulable"`
	Status               string     `json:"status"`
	SiteID               string     `json:"siteId"`
	SiteName             string     `json:"siteName"`
	SitePlatform         string     `json:"sitePlatform"`
	UpstreamGroupID      string     `json:"upstreamGroupId"`
	UpstreamGroupName    string     `json:"upstreamGroupName"`
	GroupType            string     `json:"groupType"`
	AdminAccountID       string     `json:"adminAccountId"`
	AdminAccountName     string     `json:"adminAccountName"`
	OwnGroups            []string   `json:"ownGroups"`
	Balance              *float64   `json:"balance"`
	CheckIntervalMinutes int        `json:"checkIntervalMinutes"`
	FailureThreshold     int        `json:"failureThreshold"`
	BalanceThreshold     float64    `json:"balanceThreshold"`
	ConsecutiveFailures  int        `json:"consecutiveFailures"`
	LastMessage          string     `json:"lastMessage"`
	LastLatencyMS        *int       `json:"lastLatencyMs"`
	LastCheckedAt        *time.Time `json:"lastCheckedAt"`
	NextCheckAt          *time.Time `json:"nextCheckAt"`
	RecentResults        []Result   `json:"recentResults"`
	RecentTotal          int        `json:"recentTotal"`
	RecentSuccess        int        `json:"recentSuccess"`
	UptimePercent        float64    `json:"uptimePercent"`
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

type BulkSchedulableRequest struct {
	RuleIDs     []string `json:"ruleIds"`
	Schedulable bool     `json:"schedulable"`
}

type BulkRunRequest struct {
	RuleIDs []string `json:"ruleIds"`
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
