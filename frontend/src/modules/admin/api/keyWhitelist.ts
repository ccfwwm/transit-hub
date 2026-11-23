import { followJob, operationJSON, type OperationJob } from './operations'

type WhitelistTargets = { connectionIds: string[] } | { ruleIds: string[] }

export async function syncKeyWhitelist(targets: WhitelistTargets) {
  const job = await operationJSON<OperationJob>('/my-sites/key-security/whitelist', {
    method: 'POST',
    body: JSON.stringify(targets),
  })
  return followJob(job)
}
