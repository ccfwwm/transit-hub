import { checkedOperation } from "./operations"
import type {
  BulkUpdateChannelMonitorRuleRequest,
  ChannelMonitorRateApplyResult,
  ChannelMonitorRateRule,
  ChannelMonitorRateRuleView,
  ChannelMonitorResult,
  ChannelMonitorSummary,
  ChannelMonitorTestModelConfig,
  UpdateChannelMonitorRateRuleRequest,
  UpdateChannelMonitorRuleRequest,
  UpdateChannelMonitorTestModelConfigRequest,
} from '../types/channelMonitor'
import {
  authUnauthorizedErrorKey,
  getAccessToken,
  handleAuthExpired,
  isUnauthorizedApiResponse,
} from '@/modules/auth/api/auth'

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? '/api'
const endpoint = (path: string): string => `${apiBaseUrl.replace(/\/$/, '')}${path}`

const authHeaders = (): HeadersInit => {
  const token = getAccessToken()
  if (!token) return {}
  return { Authorization: `Bearer ${token}` }
}

type AdminErrorPayload = {
  message?: string
}

const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> => {
  let response: Response
  try {
    response = await fetch(endpoint(path), {
      ...options,
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        ...authHeaders(),
        ...(options.headers ?? {}),
      },
    })
  } catch {
    throw new Error('admin.channelMonitor.errors.network')
  }

  const text = await response.text()
  const payload = text ? JSON.parse(text) as T & AdminErrorPayload : ({} as T & AdminErrorPayload)
  if (!response.ok) {
    if (isUnauthorizedApiResponse(response.status, payload)) {
      handleAuthExpired()
      throw new Error(authUnauthorizedErrorKey)
    }
    throw new Error(payload.message ?? 'admin.channelMonitor.errors.request')
  }
  return payload
}

export const getChannelMonitorSummary = async (groupId?: string, options: {light?: boolean, refresh?: boolean, signal?: AbortSignal} = {}): Promise<ChannelMonitorSummary> => {
  const query = new URLSearchParams()
  if (groupId) query.set('groupId', groupId)
  if (options.light) query.set('light', 'true')
  if (options.refresh) query.set('refresh', 'true')
  return requestJson<ChannelMonitorSummary>(`/channel-monitor/summary?${query}`, {signal: options.signal})
}

export interface MonitorPage { snapshotVersion:number;updatedAt?:string;channels: ChannelMonitorSummary['channels'], total: number, ruleIds: string[], page: number, pageSize: number}
export const getChannelMonitorPage = (query: Record<string, string>, signal?: AbortSignal): Promise<MonitorPage> =>
  requestJson(`/channel-monitor/channels?${new URLSearchParams(query)}`, {signal})

export interface MediaPreset {model: string, adapter: string, media_type: string, sizes?: string[], duration: {values: number[], min?: number}, resolutions: string[]}
export const getMonitorMediaPresets = (): Promise<MediaPreset[]> => requestJson('/channel-monitor/media-presets')
export const resetMonitorMediaTask = (key: string): Promise<void> => requestJson('/channel-monitor/media-tasks/reset', {method: 'POST', body: JSON.stringify({key})})
export const getMonitorTargetHistory = (id: string, groupId: string): Promise<ChannelMonitorResult[]> =>
  requestJson(`/channel-monitor/rules/${encodeURIComponent(id)}/history?groupId=${encodeURIComponent(groupId)}`)

export const runChannelMonitorRule = async (ruleId: string, groupId?: string): Promise<ChannelMonitorResult> =>
  checkedOperation<ChannelMonitorResult>({kind: 'run', ids: [ruleId], groupId})

export const pauseChannelMonitorRule = async (ruleId: string): Promise<void> => {
  await requestJson(`/channel-monitor/rules/${encodeURIComponent(ruleId)}/pause`, { method: 'POST' })
}

export const resumeChannelMonitorRule = async (ruleId: string): Promise<ChannelMonitorResult> =>
  requestJson<ChannelMonitorResult>(`/channel-monitor/rules/${encodeURIComponent(ruleId)}/resume`, { method: 'POST' })

export const updateChannelMonitorRule = async (ruleId: string, request: UpdateChannelMonitorRuleRequest): Promise<void> => {
  await requestJson(`/channel-monitor/rules/${encodeURIComponent(ruleId)}`, {
    method: 'PATCH',
    body: JSON.stringify(request),
  })
}

export const setChannelMonitorRuleSchedulable = async (ruleId: string, schedulable: boolean): Promise<void> => { await checkedOperation({kind:'schedulable',ids:[ruleId],schedulable}) }
export const setChannelMonitorRulePriority = async (ruleId: string, priority: number, groupId?: string): Promise<void> => { await checkedOperation({kind:'priority',ids:[ruleId],priority,groupId}) }
export const syncAndTakeOverChannelMonitorRule = async (ruleId: string): Promise<void> => { await checkedOperation({kind:'takeover',ids:[ruleId]}) }
export const bulkUpdateChannelMonitorRules = async (request: BulkUpdateChannelMonitorRuleRequest): Promise<void> => { await checkedOperation({kind:'update',ids:request.ruleIds,settings:request}) }
export const bulkSetChannelMonitorRulesSchedulable = async (ruleIds: string[], schedulable: boolean): Promise<void> => { await checkedOperation({kind:'schedulable',ids:ruleIds,schedulable}) }
export const bulkRunChannelMonitorRules = async (ruleIds: string[], groupId?: string): Promise<ChannelMonitorResult[]> => { const result=await checkedOperation<ChannelMonitorResult | ChannelMonitorResult[]>({kind:'run',ids:ruleIds,groupId}); return Array.isArray(result)?result:[result] }

export const getChannelMonitorRateRule = async (): Promise<ChannelMonitorRateRuleView> =>
  requestJson<ChannelMonitorRateRuleView>('/channel-monitor/rate-rule')

export const updateChannelMonitorRateRule = async (request: UpdateChannelMonitorRateRuleRequest): Promise<ChannelMonitorRateRule> =>
  requestJson<ChannelMonitorRateRule>('/channel-monitor/rate-rule', {
    method: 'PATCH',
    body: JSON.stringify(request),
  })

export const previewChannelMonitorRateRule = async (): Promise<ChannelMonitorRateRuleView> =>
  checkedOperation<ChannelMonitorRateRuleView>({kind:'rate_preview'})

export const applyChannelMonitorRateRule = async (groupId?: string, planVersion?: string): Promise<ChannelMonitorRateApplyResult> =>
  checkedOperation<ChannelMonitorRateApplyResult>({kind:'rate_apply',groupId,planVersion})

export const updateChannelMonitorPriorityGroupRule = async (groupId: string, request: { enabled?: boolean, autoApplyOnRateChange?: boolean }): Promise<void> => {
  await requestJson(`/channel-monitor/priority-groups/${encodeURIComponent(groupId)}`, {
    method: 'PATCH',
    body: JSON.stringify(request),
  })
}

export const updateChannelMonitorTestModelConfig = async (request: UpdateChannelMonitorTestModelConfigRequest): Promise<ChannelMonitorTestModelConfig> =>
  requestJson<ChannelMonitorTestModelConfig>('/channel-monitor/test-model-config', {
    method: 'PATCH',
    body: JSON.stringify(request),
  })

export const bulkSyncAndTakeOverChannelMonitorRules = async (ruleIds: string[]): Promise<void> => { await checkedOperation({kind:'takeover',ids:ruleIds}) }
export const getMonitorPageHistory = (query: Record<string,string>, signal?: AbortSignal): Promise<{snapshotVersion:number,histories:Record<string,ChannelMonitorResult[]>}> => requestJson(`/channel-monitor/page-history?${new URLSearchParams(query)}`,{signal})

export const getMonitorStats = (groupId?:string,signal?:AbortSignal):Promise<Pick<ChannelMonitorSummary,'stats'|'updatedAt'|'snapshotVersion'>> => requestJson(`/channel-monitor/stats?${new URLSearchParams({groupId:groupId||''})}`,{signal})
export const getMonitorGroups = (groupId?:string,signal?:AbortSignal):Promise<Pick<ChannelMonitorSummary,'groups'|'updatedAt'|'snapshotVersion'>> => requestJson(`/channel-monitor/groups?${new URLSearchParams({groupId:groupId||''})}`,{signal})
