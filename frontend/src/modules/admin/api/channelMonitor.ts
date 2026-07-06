import type {
  ChannelMonitorResult,
  ChannelMonitorSummary,
  UpdateChannelMonitorRuleRequest,
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

export const getChannelMonitorSummary = async (): Promise<ChannelMonitorSummary> =>
  requestJson<ChannelMonitorSummary>('/channel-monitor/summary')

export const runChannelMonitorRule = async (ruleId: string): Promise<ChannelMonitorResult> =>
  requestJson<ChannelMonitorResult>(`/channel-monitor/rules/${encodeURIComponent(ruleId)}/run`, { method: 'POST' })

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
