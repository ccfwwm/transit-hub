export interface AdminAccount {
  id: string
  platform: string
  baseUrl: string
  identity: string
  displayName: string
  authMethod: string
  current: boolean
  lastUsedAt: string | null
  createdAt: string
  updatedAt: string
}
