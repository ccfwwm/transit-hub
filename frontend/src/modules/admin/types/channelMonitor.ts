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
  checkIntervalMinutes: number
  failureThreshold: number
  balanceThreshold: number
  consecutiveFailures: number
  lastMessage: string
  lastLatencyMs: number | null
  lastCheckedAt: string | null
  nextCheckAt: string | null
  recentResults: ChannelMonitorResult[]
  recentTotal: number
  recentSuccess: number
  uptimePercent: number
}

export interface ChannelMonitorSummary {
  stats: ChannelMonitorStats
  groups: ChannelMonitorGroup[]
  channels: ChannelMonitorChannel[]
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
