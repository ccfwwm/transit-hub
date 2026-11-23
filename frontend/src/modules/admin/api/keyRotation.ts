import { followJob, operationJSON, type OperationJob } from './operations'

type RotationTargets = { connectionIds: string[] } | { ruleIds: string[] }

export function previewKeyRotation(targets: RotationTargets) {
  return operationJSON<{ keys: number; skipped: number; version: string }>('/my-sites/key-security/rotate', {
    method: 'POST',
    body: JSON.stringify({ ...targets, preview: true }),
  })
}

export async function rotateKeys(targets: RotationTargets, version: string) {
  return followJob(await operationJSON<OperationJob>('/my-sites/key-security/rotate', {
    method: 'POST',
    body: JSON.stringify({ ...targets, version }),
  }))
}

export function keyJobMessage(job: OperationJob) {
  return job.items.filter(item => item.error || item.result?.skipped || item.result?.namingWarning)
    .map(item => `${item.id} · ${item.error || item.result.message}`).join('\n')
}
