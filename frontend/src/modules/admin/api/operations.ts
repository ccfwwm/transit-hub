import { ref } from 'vue'
import { getAccessToken, handleAuthExpired } from '@/modules/auth/api/auth'

export interface OperationItem { id: string; status: string; result: any; error: string }
export interface OperationJob { id: string; kind: string; status: string; total: number; completed: number; failed: number; items: OperationItem[]; createdAt?: string }
export const operationJobs = ref<OperationJob[]>([])
const polling = new Map<string, Promise<OperationJob>>()
let workspaceRevision = 0
export function resetOperationWorkspace() {
  workspaceRevision++
  operationJobs.value = []
}
const base = (import.meta.env.VITE_API_BASE_URL ?? '/api').replace(/\/$/, '')
export async function operationJSON<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(base + path, { ...options, headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getAccessToken()}`, ...options.headers } })
  if (response.status === 401) handleAuthExpired()
  const payload = await response.json()
  if (!response.ok) throw new Error(payload.message || '操作失败')
  return payload as T
}
function track(job: OperationJob) {
  const at = operationJobs.value.findIndex(item => item.id === job.id)
  if (at >= 0) operationJobs.value[at] = job
  else operationJobs.value = [job, ...operationJobs.value].sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || '')).slice(0, 12)
}
export function followJob(job: OperationJob): Promise<OperationJob> {
  const existing = polling.get(job.id)
  if (existing) return existing
  track(job)
  const token = getAccessToken()
  const revision = workspaceRevision
  const promise = (async () => {
    while (job.status === 'running') {
      const waiting = job.items.some(item => item.status === 'waiting')
      await new Promise(resolve => setTimeout(resolve, waiting ? 5000 : 1000))
      if (getAccessToken() !== token) throw new Error('登录状态已变化')
      job = await operationJSON<OperationJob>(`/channel-monitor/operations/${encodeURIComponent(job.id)}`)
      if (getAccessToken() === token && workspaceRevision === revision) track(job)
    }
    return job
  })().finally(() => polling.delete(job.id))
  polling.set(job.id, promise)
  return promise
}
export async function submitOperation(body: Record<string, unknown>): Promise<OperationJob> {
  const job = await operationJSON<OperationJob>('/channel-monitor/operations', { method: 'POST', body: JSON.stringify(body) })
  return followJob(job)
}
export async function checkedOperation<T = unknown>(body: Record<string, unknown>): Promise<T> {
  const job = await submitOperation(body)
  if (job.failed) throw new Error(job.items.find(item => item.status === 'failed')?.error || `${job.failed} 项失败，可在任务进度中重试`)
  return (job.items.length === 1 ? job.items[0]?.result : job.items.map(item => item.result)) as T
}
export async function retryJob(id: string) {
  return followJob(await operationJSON<OperationJob>(`/channel-monitor/operations/${encodeURIComponent(id)}/retry`, { method: 'POST' }))
}
