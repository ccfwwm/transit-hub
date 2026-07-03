export type NotificationChannel = 'dingtalk' | 'feishu' | 'telegram'

export type TestNotificationChannelPayload = {
  channel: NotificationChannel
  webhook?: string
  secret?: string
  telegramBotToken?: string
  telegramChatId?: string
  telegramProxyUrl?: string
}

export type TestNotificationChannelResponse = {
  success: boolean
  message: string
}

export type NotificationChannelSettings = {
  dingtalk: DingtalkChannelSettings[]
  feishu: WebhookChannelSettings[]
  telegram: TelegramChannelSettings[]
}

export type StrategySettings = {
  enableRefreshInterval: boolean
  refreshInterval: number
  enableBalanceWarning: boolean
  defaultBalanceThreshold: number
  balanceNotifyBotIds: string[]
  balanceTemplate: string
  enableMultiplierAlert: boolean
  multiplierNotifyBotIds: string[]
  multiplierTemplate: string
}

export type DingtalkChannelSettings = {
  id: string
  name: string
  enabled: boolean
  webhook: string
  secret: string
}

export type WebhookChannelSettings = {
  id: string
  name: string
  enabled: boolean
  webhook: string
  secret: string
}

export type TelegramChannelSettings = {
  id: string
  name: string
  enabled: boolean
  botToken: string
  chatId: string
  proxyUrl: string
}
