export type ChannelMonitorStatus =
  | 'unknown'
  | 'healthy'
  | 'failed'
  | 'auto_paused'
  | 'balance_paused'
  | 'manual_paused'
  | 'unsupported'

export interface ChannelMonitorStats {
  total: number
  available: number
  failed: number
  balancePaused: number
  manualPaused: number
  monitorPaused: number
  dispatchPaused: number
  unsupported: number
}

export interface ChannelMonitorGroup {
  groupName: string
  platform: string
  total: number
  available: number
  failed: number
  balancePaused: number
  manualPaused: number
  monitorPaused: number
  dispatchPaused: number
  lastCheckedAt: string | null
}

export interface ChannelMonitorChannel {
  ruleId: string
  connectionId: string
  enabled: boolean
  supported: boolean
  manualPaused: boolean
  schedulable: boolean | null
  status: ChannelMonitorStatus
  siteId: string
  siteName: string
  sitePlatform: string
  upstreamGroupId: string
  upstreamGroupName: string
  groupType: string
  adminAccountId: string
  adminAccountName: string
  ownGroups: string[]
  balance: number | null
  accountRateMultiplier: number | null
  accountPriority: number | null
  upstreamMultiplier: number | null
  upstreamEffectiveMultiplier: number | null
  ownGroupMultiplier: number | null
  recommendedPriority: number | null
  rateGateStatus: RateGateStatus
  rateGateMessage: string
  checkIntervalMinutes: number
  failureThreshold: number
  balanceThreshold: number
  consecutiveFailures: number
  lastMessage: string
  lastLatencyMs: number | null
  lastCheckedAt: string | null
  nextCheckAt: string | null
  recentResults?: ChannelMonitorResult[] | null
  recentTotal: number
  recentSuccess: number
  uptimePercent: number
}

export interface ChannelMonitorSummary {
  stats: ChannelMonitorStats
  groups: ChannelMonitorGroup[]
  channels: ChannelMonitorChannel[]
  rateRule: ChannelMonitorRateRuleView
  testModelConfig: ChannelMonitorTestModelConfig
}

export interface UpdateChannelMonitorRuleRequest {
  enabled?: boolean
  checkIntervalMinutes?: number
  failureThreshold?: number
  balanceThreshold?: number
}

export interface BulkUpdateChannelMonitorRuleRequest extends UpdateChannelMonitorRuleRequest {
  ruleIds: string[]
}

export interface ChannelMonitorResult {
  id: string
  ruleId: string
  connectionId: string
  status: ChannelMonitorStatus
  success: boolean
  message: string
  latencyMs: number | null
  model: string
  action: string
  startedAt: string
  finishedAt: string
  createdAt: string
}

export type RateGateStatus = 'allowed' | 'blocked' | 'missing' | 'skipped' | ''

export interface ChannelMonitorRateRule {
  enabled: boolean
  autoApplyOnCheck: boolean
  updatePriority: boolean
  stopWhenMissingRate: boolean
  lastAppliedAt: string | null
  updatedAt: string
}

export interface ChannelMonitorRateRuleView {
  rule: ChannelMonitorRateRule
  summary: ChannelMonitorRateSummary
  rows: ChannelMonitorRateRow[]
  lastResult: ChannelMonitorRateApplyResult | null
}

export interface ChannelMonitorRateSummary {
  total: number
  allowed: number
  blocked: number
  missing: number
  skipped: number
  wouldEnable: number
  wouldDisable: number
  priorityChanges: number
}

export interface ChannelMonitorRateDecision {
  groupName: string
  ownMultiplier: number | null
  allowed: boolean
  message: string
}

export interface ChannelMonitorRateRow {
  ruleId: string
  connectionId: string
  adminAccountId: string
  adminAccountName: string
  siteName: string
  upstreamGroupName: string
  ownGroups: string[]
  groupDecisions: ChannelMonitorRateDecision[]
  accountRateMultiplier: number | null
  accountPriority: number | null
  upstreamMultiplier: number | null
  upstreamEffectiveMultiplier: number | null
  ownGroupMultiplier: number | null
  currentSchedulable: boolean | null
  suggestedSchedulable: boolean
  currentPriority: number | null
  suggestedPriority: number | null
  rateGateStatus: RateGateStatus
  rateGateMessage: string
  supported: boolean
}

export interface ChannelMonitorRateApplyResult {
  id: string
  action: string
  success: boolean
  message: string
  total: number
  enabledCount: number
  disabledCount: number
  priorityUpdated: number
  skippedCount: number
  rows: ChannelMonitorRateRow[]
  createdAt: string
}

export interface UpdateChannelMonitorRateRuleRequest {
  enabled?: boolean
  autoApplyOnCheck?: boolean
  updatePriority?: boolean
  stopWhenMissingRate?: boolean
}

export interface ChannelMonitorTestModelConfig {
  openaiModelId: string
  anthropicModelId: string
  balanceRefreshIntervalMinutes: number
  updatedAt: string
}

export interface UpdateChannelMonitorTestModelConfigRequest {
  openaiModelId?: string
  anthropicModelId?: string
  balanceRefreshIntervalMinutes?: number
}
