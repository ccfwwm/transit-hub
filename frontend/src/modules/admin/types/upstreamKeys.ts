export interface KeySite {
  id: string
  name: string
  platform: 'sub2api' | 'newapi'
  slug: string
  groups: { id: string; name: string; platform?: string | null }[] | null
  error?: string
}
export interface KeyBinding {
  id: string
  account: string
  groups: string[]
  update: boolean
  sharedSince?: string
}
export interface KeyEntry {
  id: string
  siteId: string
  name: string
  groupId: string
  groupName: string
  status: string
  lastUsed?: number | string | null
  used?: number | null
  config: Record<string, unknown>
  bindings: KeyBinding[]
}
export interface KeyInventory {
  sites: KeySite[]
  items: KeyEntry[]
  ownGroups: { id: string; name: string }[]
}
export interface UsagePoint { label: string; amount: number | null; requests: number }
export interface KeyUsage {
  state: 'complete' | 'partial' | 'unavailable'
  unit: string
  total: UsagePoint
  trend: UsagePoint[]
  models: UsagePoint[]
  logs: { time: number; model: string; amount: number | null; ip?: string }[]
}
export interface KeyUsageSummary {
  state: KeyUsage['state']
  unit: string
  today: UsagePoint
  week: UsagePoint
}
export interface BindingPlan {
  id: string
  siteId: string
  siteName: string
  keyName: string
  accountName: string
  limits?: Record<string, number>
  targets: { groupId: string; groupName: string; name: string; number: number; retain: boolean; error?: string }[]
  error?: string
}
