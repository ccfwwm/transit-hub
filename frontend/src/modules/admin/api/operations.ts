import { ref } from 'vue'
import { getAccessToken, handleAuthExpired } from '@/modules/auth/api/auth'

export interface OperationItem { id: string; status: string; result: any; error: string }
export interface OperationJob { id: string; kind: string; status: string; total: number; completed: number; failed: number; items: OperationItem[] }
export const operationJobs = ref<OperationJob[]>([])
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
  else operationJobs.value = [job, ...operationJobs.value].slice(0, 12)
}
export async function followJob(job: OperationJob): Promise<OperationJob> {
  track(job)
  while (job.status === 'running') {
    await new Promise(resolve => setTimeout(resolve, 1000))
    job = await operationJSON<OperationJob>(`/channel-monitor/operations/${encodeURIComponent(job.id)}`)
    track(job)
  }
  return job
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
