<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CheckSquare2,
  CircleSlash,
  Clock3,
  Gauge,
  Loader2,
  PauseCircle,
  Play,
  Power,
  PowerOff,
  RefreshCw,
  Search,
  Settings2,
  ShieldAlert,
  Square,
  Trash2,
  WalletCards,
} from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import {
  applyChannelMonitorRateRule,
  bulkRunChannelMonitorRules,
  bulkSetChannelMonitorRulesSchedulable,
  bulkUpdateChannelMonitorRules,
  getChannelMonitorSummary,
  previewChannelMonitorRateRule,
  runChannelMonitorRule,
  setChannelMonitorRuleSchedulable,
  setChannelMonitorRulePriority,
  updateChannelMonitorRateRule,
  updateChannelMonitorRule,
  updateChannelMonitorTestModelConfig,
} from '../api/channelMonitor'
import { realDisconnect } from '../api/mySites'
import type { ChannelMonitorChannel, ChannelMonitorRateRule, ChannelMonitorResult, ChannelMonitorStatus, RateGateStatus, UpdateChannelMonitorRuleRequest } from '../types/channelMonitor'
import type { RealDisconnectRequest } from '../types/mySites'

type StatusFilter = 'all' | 'monitor_paused' | 'dispatch_paused' | ChannelMonitorStatus
type RefreshMode = 'initial' | 'manual' | 'silent'
type BulkEditorScope = 'all' | 'selected'
type ActionOptions = {
  actionKey?: string
  clearSelection?: boolean
  refresh?: boolean
}

const { t, locale } = useI18n()

const isLoading = ref(false)
const isRefreshing = ref(false)
const activeActionKeys = ref<string[]>([])
const errorKey = ref('')
const searchQuery = ref('')
const statusFilter = ref<StatusFilter>('all')
const selectedGroup = ref('all')
const selectedRuleIds = ref<string[]>([])
const defaultRateRule = (): ChannelMonitorRateRule => ({
  enabled: false,
  autoApplyOnCheck: true,
  updatePriority: true,
  stopWhenMissingRate: true,
  lastAppliedAt: null,
  updatedAt: '',
})
const summary = ref({
  stats: { total: 0, available: 0, failed: 0, balancePaused: 0, manualPaused: 0, monitorPaused: 0, dispatchPaused: 0, unsupported: 0 },
  groups: [],
  channels: [],
  rateRule: {
    rule: defaultRateRule(),
    summary: { total: 0, allowed: 0, blocked: 0, missing: 0, skipped: 0, wouldEnable: 0, wouldDisable: 0, priorityChanges: 0 },
    rows: [],
    lastResult: null,
  },
  testModelConfig: {
    openaiModelId: 'gpt-5.4',
    anthropicModelId: 'claude-sonnet-4-6',
    updatedAt: '',
  },
} as Awaited<ReturnType<typeof getChannelMonitorSummary>>)
const editingChannel = ref<ChannelMonitorChannel | null>(null)
const isBulkEditorOpen = ref(false)
const bulkEditorScope = ref<BulkEditorScope>('selected')
const isRateRuleEditorOpen = ref(false)
const isTestModelEditorOpen = ref(false)
const disconnectingChannel = ref<ChannelMonitorChannel | null>(null)
const disconnectMode = ref<RealDisconnectRequest['mode']>('unlink')
const disconnectError = ref('')
const editForm = ref({ enabled: true, checkIntervalMinutes: 10, failureThreshold: 3, balanceThreshold: 1 })
const rateRuleForm = ref({ enabled: false, autoApplyOnCheck: true, updatePriority: true, stopWhenMissingRate: true })
const testModelForm = ref({ openaiModelId: 'gpt-5.4', anthropicModelId: 'claude-sonnet-4-6' })
const priorityDrafts = ref<Record<string, number | null>>({})

const syncSummaryState = (next: Awaited<ReturnType<typeof getChannelMonitorSummary>>) => {
  summary.value = next
  selectedRuleIds.value = selectedRuleIds.value.filter(id => next.channels.some(channel => channel.ruleId === id))
  priorityDrafts.value = Object.fromEntries(next.channels.map(channel => [
    channel.ruleId,
    channel.accountPriority ?? channel.recommendedPriority ?? 0,
  ]))
}

const loadSummary = async (mode: RefreshMode = 'manual') => {
  const showInitialLoading = mode === 'initial' || (summary.value.channels.length === 0 && mode !== 'silent')
  if (showInitialLoading) isLoading.value = true
  else if (mode === 'manual') isRefreshing.value = true
  errorKey.value = ''
  try {
    syncSummaryState(await getChannelMonitorSummary())
  } catch (error: any) {
    errorKey.value = error?.message ?? 'admin.channelMonitor.errors.request'
  } finally {
    if (showInitialLoading) isLoading.value = false
    isRefreshing.value = false
  }
}

onMounted(() => {
  void loadSummary('initial')
})

const statCards = computed(() => [
  { key: 'total', value: summary.value.stats.total, icon: Activity, tone: 'text-primary bg-primary/10 border-primary/20' },
  { key: 'available', value: summary.value.stats.available, icon: CheckCircle2, tone: 'text-emerald-600 bg-emerald-500/10 border-emerald-500/20' },
  { key: 'failed', value: summary.value.stats.failed, icon: AlertTriangle, tone: 'text-red-600 bg-red-500/10 border-red-500/20' },
  { key: 'balancePaused', value: summary.value.stats.balancePaused, icon: WalletCards, tone: 'text-amber-600 bg-amber-500/10 border-amber-500/20' },
  { key: 'monitorPaused', value: summary.value.stats.monitorPaused, icon: PauseCircle, tone: 'text-sky-600 bg-sky-500/10 border-sky-500/20' },
  { key: 'dispatchPaused', value: summary.value.stats.dispatchPaused, icon: PowerOff, tone: 'text-zinc-600 bg-zinc-500/10 border-zinc-500/20' },
])

const groupOptions = computed(() => ['all', ...Array.from(new Set(summary.value.groups.map(group => group.groupName)))])

const filteredChannels = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return summary.value.channels.filter(channel => {
    const statusMatch =
      statusFilter.value === 'all' ||
      (statusFilter.value === 'monitor_paused' && !channel.enabled) ||
      (statusFilter.value === 'dispatch_paused' && channel.schedulable === false) ||
      channel.status === statusFilter.value
    const groupMatch = selectedGroup.value === 'all' || channel.ownGroups.includes(selectedGroup.value)
    const searchMatch = !query ||
      channel.siteName.toLowerCase().includes(query) ||
      channel.upstreamGroupName.toLowerCase().includes(query) ||
      channel.adminAccountName.toLowerCase().includes(query) ||
      channel.adminAccountId.toLowerCase().includes(query) ||
      channel.ownGroups.some(group => group.toLowerCase().includes(query))
    return statusMatch && groupMatch && searchMatch
  })
})

const visibleRuleIds = computed(() => filteredChannels.value.map(channel => channel.ruleId))
const selectedCount = computed(() => selectedRuleIds.value.length)
const allVisibleSelected = computed(() => visibleRuleIds.value.length > 0 && visibleRuleIds.value.every(id => selectedRuleIds.value.includes(id)))
const selectedChannels = computed(() => summary.value.channels.filter(channel => selectedRuleIds.value.includes(channel.ruleId)))
const allRuleIds = computed(() => summary.value.channels.map(channel => channel.ruleId))
const bulkEditorRuleIds = computed(() => bulkEditorScope.value === 'all' ? allRuleIds.value : selectedRuleIds.value)
const bulkEditorCount = computed(() => bulkEditorRuleIds.value.length)
const isActionLoading = computed(() => activeActionKeys.value.length > 0)
const isBulkActionLoading = computed(() => activeActionKeys.value.some(key => key.startsWith('bulk:') || key.startsWith('editor:') || key.startsWith('rate-rule:') || key.startsWith('test-model:')))

const statusLabel = (status: StatusFilter): string => t(`admin.channelMonitor.status.${status}`)

const statusClass = (status: ChannelMonitorStatus): string => {
  switch (status) {
    case 'healthy':
      return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-600 dark:text-emerald-300'
    case 'failed':
    case 'auto_paused':
      return 'border-red-500/20 bg-red-500/10 text-red-600 dark:text-red-300'
    case 'balance_paused':
      return 'border-amber-500/20 bg-amber-500/10 text-amber-600 dark:text-amber-300'
    case 'manual_paused':
      return 'border-sky-500/20 bg-sky-500/10 text-sky-600 dark:text-sky-300'
    case 'unsupported':
      return 'border-border/60 bg-surface-elevated text-muted-foreground'
    default:
      return 'border-primary/20 bg-primary/10 text-primary'
  }
}

const statusIcon = (status: ChannelMonitorStatus) => {
  if (status === 'healthy') return CheckCircle2
  if (status === 'balance_paused') return WalletCards
  if (status === 'manual_paused') return PauseCircle
  if (status === 'unsupported') return CircleSlash
  if (status === 'failed' || status === 'auto_paused') return ShieldAlert
  return Activity
}

const formatDateTime = (value: string | null): string => {
  if (!value) return t('admin.channelMonitor.common.never')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('admin.channelMonitor.common.never')
  return new Intl.DateTimeFormat(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

const formatRelativeShort = (value: string | null): string => {
  if (!value) return t('admin.channelMonitor.common.never')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('admin.channelMonitor.common.never')
  const diffMs = date.getTime() - Date.now()
  const absSeconds = Math.max(0, Math.round(Math.abs(diffMs) / 1000))
  if (absSeconds < 60) return diffMs >= 0 ? t('admin.channelMonitor.timeline.inSeconds', { value: absSeconds }) : t('admin.channelMonitor.timeline.secondsAgo', { value: absSeconds })
  const minutes = Math.round(absSeconds / 60)
  if (minutes < 60) return diffMs >= 0 ? t('admin.channelMonitor.timeline.inMinutes', { value: minutes }) : t('admin.channelMonitor.timeline.minutesAgo', { value: minutes })
  const hours = Math.round(minutes / 60)
  if (hours < 24) return diffMs >= 0 ? t('admin.channelMonitor.timeline.inHours', { value: hours }) : t('admin.channelMonitor.timeline.hoursAgo', { value: hours })
  const days = Math.round(hours / 24)
  return diffMs >= 0 ? t('admin.channelMonitor.timeline.inDays', { value: days }) : t('admin.channelMonitor.timeline.daysAgo', { value: days })
}

const formatMoney = (value: number | null): string => {
  if (value === null || !Number.isFinite(value)) return t('admin.channelMonitor.common.unknown')
  return value.toFixed(2)
}

const formatLatency = (value: number | null): string => {
  if (value === null || !Number.isFinite(value)) return t('admin.channelMonitor.common.unknown')
  return `${value} ms`
}

const formatMultiplier = (value: number | null): string => {
  if (value === null || !Number.isFinite(value)) return t('admin.channelMonitor.common.unknown')
  return `${value.toFixed(4).replace(/0+$/, '').replace(/\.$/, '')}x`
}

const rateGateLabel = (status: RateGateStatus): string => t(`admin.channelMonitor.rateRule.status.${status || 'missing'}`)

const rateGateClass = (status: RateGateStatus): string => {
  switch (status) {
    case 'allowed':
      return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
    case 'blocked':
      return 'border-red-500/20 bg-red-500/10 text-red-700 dark:text-red-300'
    case 'missing':
      return 'border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300'
    default:
      return 'border-border/60 bg-surface-elevated text-muted-foreground'
  }
}

const groupRowClass = (groupName: string, platform: string): string => {
  const selected = selectedGroup.value === groupName
  const normalizedPlatform = platform.toLowerCase()
  if (normalizedPlatform.includes('anthropic')) {
    return selected
      ? 'border-l-4 border-l-violet-500 bg-violet-500/15 text-violet-950 ring-1 ring-inset ring-violet-500/30 dark:text-violet-100'
      : 'border-l-4 border-l-violet-500/40 hover:bg-violet-500/10'
  }
  if (normalizedPlatform.includes('openai')) {
    return selected
      ? 'border-l-4 border-l-blue-500 bg-blue-500/15 text-blue-950 ring-1 ring-inset ring-blue-500/30 dark:text-blue-100'
      : 'border-l-4 border-l-blue-500/40 hover:bg-blue-500/10'
  }
  if (normalizedPlatform.includes('gemini')) {
    return selected
      ? 'border-l-4 border-l-emerald-500 bg-emerald-500/15 text-emerald-950 ring-1 ring-inset ring-emerald-500/30 dark:text-emerald-100'
      : 'border-l-4 border-l-emerald-500/40 hover:bg-emerald-500/10'
  }
  return selected
    ? 'border-l-4 border-l-primary bg-primary/15 text-foreground ring-1 ring-inset ring-primary/30'
    : 'border-l-4 border-l-transparent hover:bg-surface-elevated/70'
}

const groupPlatformBadgeClass = (platform: string): string => {
  const normalizedPlatform = platform.toLowerCase()
  if (normalizedPlatform.includes('anthropic')) return 'border-violet-500/20 bg-violet-500/10 text-violet-700 dark:text-violet-300'
  if (normalizedPlatform.includes('openai')) return 'border-blue-500/20 bg-blue-500/10 text-blue-700 dark:text-blue-300'
  if (normalizedPlatform.includes('gemini')) return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
  return 'border-border/50 bg-surface-elevated text-muted-foreground'
}

const isActionActive = (key: string): boolean => activeActionKeys.value.includes(key)

const channelActionKey = (channel: ChannelMonitorChannel, action: string): string => `channel:${channel.ruleId}:${action}`

const isChannelBusy = (channel: ChannelMonitorChannel): boolean => activeActionKeys.value.some(key => key.startsWith(`channel:${channel.ruleId}:`))

const runAction = async (action: () => Promise<unknown>, options: ActionOptions = {}) => {
  const actionKey = options.actionKey ?? 'global'
  if (isActionActive(actionKey)) return
  activeActionKeys.value = [...activeActionKeys.value, actionKey]
  errorKey.value = ''
  try {
    await action()
    if (options.clearSelection) selectedRuleIds.value = []
    if (options.refresh !== false) await loadSummary('silent')
  } catch (error: any) {
    errorKey.value = error?.message ?? 'admin.channelMonitor.errors.request'
  } finally {
    activeActionKeys.value = activeActionKeys.value.filter(key => key !== actionKey)
  }
}

const toggleSelect = (ruleId: string) => {
  if (selectedRuleIds.value.includes(ruleId)) {
    selectedRuleIds.value = selectedRuleIds.value.filter(id => id !== ruleId)
  } else {
    selectedRuleIds.value = [...selectedRuleIds.value, ruleId]
  }
}

const toggleSelectAllVisible = () => {
  if (allVisibleSelected.value) {
    const visible = new Set(visibleRuleIds.value)
    selectedRuleIds.value = selectedRuleIds.value.filter(id => !visible.has(id))
  } else {
    selectedRuleIds.value = Array.from(new Set([...selectedRuleIds.value, ...visibleRuleIds.value]))
  }
}

const openEditor = (channel: ChannelMonitorChannel) => {
  editingChannel.value = channel
  isBulkEditorOpen.value = false
  editForm.value = {
    enabled: channel.enabled,
    checkIntervalMinutes: channel.checkIntervalMinutes,
    failureThreshold: channel.failureThreshold,
    balanceThreshold: channel.balanceThreshold,
  }
}

const openBulkEditor = (scope: BulkEditorScope = 'selected') => {
  bulkEditorScope.value = scope
  const first = (scope === 'all' ? summary.value.channels : selectedChannels.value)[0]
  editingChannel.value = null
  isBulkEditorOpen.value = true
  editForm.value = {
    enabled: first?.enabled ?? true,
    checkIntervalMinutes: first?.checkIntervalMinutes ?? 10,
    failureThreshold: first?.failureThreshold ?? 3,
    balanceThreshold: first?.balanceThreshold ?? 1,
  }
}

const closeEditor = () => {
  editingChannel.value = null
  isBulkEditorOpen.value = false
  bulkEditorScope.value = 'selected'
}

const openRateRuleEditor = () => {
  const rule = summary.value.rateRule.rule
  rateRuleForm.value = {
    enabled: rule.enabled,
    autoApplyOnCheck: rule.autoApplyOnCheck,
    updatePriority: rule.updatePriority,
    stopWhenMissingRate: rule.stopWhenMissingRate,
  }
  isRateRuleEditorOpen.value = true
}

const closeRateRuleEditor = () => {
  isRateRuleEditorOpen.value = false
}

const openTestModelEditor = () => {
  testModelForm.value = {
    openaiModelId: summary.value.testModelConfig.openaiModelId || 'gpt-5.4',
    anthropicModelId: summary.value.testModelConfig.anthropicModelId || 'claude-sonnet-4-6',
  }
  isTestModelEditorOpen.value = true
}

const closeTestModelEditor = () => {
  isTestModelEditorOpen.value = false
}

const saveTestModelConfig = async () => {
  await runAction(async () => {
    const config = await updateChannelMonitorTestModelConfig({
      openaiModelId: testModelForm.value.openaiModelId,
      anthropicModelId: testModelForm.value.anthropicModelId,
    })
    summary.value.testModelConfig = config
  }, { actionKey: 'test-model:save', refresh: false })
  closeTestModelEditor()
}

const saveRateRule = async (applyAfterSave = false) => {
  await runAction(async () => {
    await updateChannelMonitorRateRule({ ...rateRuleForm.value })
    if (applyAfterSave) await applyChannelMonitorRateRule()
  }, { actionKey: applyAfterSave ? 'rate-rule:save-apply' : 'rate-rule:save' })
  closeRateRuleEditor()
}

const saveEditor = async () => {
  const payload: UpdateChannelMonitorRuleRequest = {
    enabled: editForm.value.enabled,
    checkIntervalMinutes: Number(editForm.value.checkIntervalMinutes),
    failureThreshold: Number(editForm.value.failureThreshold),
    balanceThreshold: Number(editForm.value.balanceThreshold),
  }
  if (editingChannel.value) {
    await runAction(() => updateChannelMonitorRule(editingChannel.value!.ruleId, payload), { actionKey: `editor:${editingChannel.value.ruleId}` })
  } else if (isBulkEditorOpen.value) {
    const ruleIds = bulkEditorRuleIds.value
    if (ruleIds.length === 0) return
    await runAction(() => bulkUpdateChannelMonitorRules({ ...payload, ruleIds }), {
      actionKey: `bulk:edit:${bulkEditorScope.value}`,
      clearSelection: bulkEditorScope.value === 'selected',
    })
  }
  closeEditor()
}

const setSelectedMonitoring = (enabled: boolean) =>
  runAction(() => bulkUpdateChannelMonitorRules({ ruleIds: selectedRuleIds.value, enabled }), { actionKey: `bulk:monitor:${enabled}`, clearSelection: true })

const setSelectedSchedulable = (schedulable: boolean) =>
  runAction(() => bulkSetChannelMonitorRulesSchedulable(selectedRuleIds.value, schedulable), { actionKey: `bulk:dispatch:${schedulable}`, clearSelection: true })

const runSelected = () =>
  runAction(() => bulkRunChannelMonitorRules(selectedRuleIds.value), { actionKey: 'bulk:run' })

const previewRateRule = () =>
  runAction(async () => {
    const view = await previewChannelMonitorRateRule()
    summary.value.rateRule = view
  }, { actionKey: 'rate-rule:preview', refresh: false })

const applyRateRule = () =>
  runAction(() => applyChannelMonitorRateRule(), { actionKey: 'rate-rule:apply' })

const toggleChannelMonitoring = (channel: ChannelMonitorChannel) =>
  runAction(() => updateChannelMonitorRule(channel.ruleId, { enabled: !channel.enabled }), { actionKey: channelActionKey(channel, 'monitor') })

const toggleChannelSchedulable = (channel: ChannelMonitorChannel) =>
  runAction(() => setChannelMonitorRuleSchedulable(channel.ruleId, channel.schedulable === false), { actionKey: channelActionKey(channel, 'dispatch') })

const setChannelPriority = (channel: ChannelMonitorChannel) => {
  const priority = Number(priorityDrafts.value[channel.ruleId])
  if (!Number.isFinite(priority)) return
  return runAction(() => setChannelMonitorRulePriority(channel.ruleId, Math.round(priority)), { actionKey: channelActionKey(channel, 'priority') })
}

const openDisconnect = (channel: ChannelMonitorChannel) => {
  disconnectingChannel.value = channel
  disconnectMode.value = 'unlink'
  disconnectError.value = ''
}

const closeDisconnect = () => {
  disconnectingChannel.value = null
  disconnectMode.value = 'unlink'
  disconnectError.value = ''
}

const confirmDisconnect = async () => {
  if (!disconnectingChannel.value) return
  const channel = disconnectingChannel.value
  const actionKey = channelActionKey(channel, 'disconnect')
  if (isActionActive(actionKey)) return
  activeActionKeys.value = [...activeActionKeys.value, actionKey]
  disconnectError.value = ''
  try {
    await realDisconnect({ connectionId: channel.connectionId, mode: disconnectMode.value })
    selectedRuleIds.value = selectedRuleIds.value.filter(id => id !== channel.ruleId)
    closeDisconnect()
    await loadSummary('silent')
  } catch {
    disconnectError.value = t('admin.channelMonitor.disconnect.failed')
  } finally {
    activeActionKeys.value = activeActionKeys.value.filter(key => key !== actionKey)
  }
}

const selectGroup = (groupName: string) => {
  selectedGroup.value = selectedGroup.value === groupName ? 'all' : groupName
}

const timelineItems = (channel: ChannelMonitorChannel): Array<ChannelMonitorResult | null> => {
  const recentResults = Array.isArray(channel.recentResults) ? channel.recentResults : []
  const ordered = [...recentResults].reverse().slice(-60)
  return [...Array(Math.max(0, 60 - ordered.length)).fill(null), ...ordered]
}

const timelineClass = (result: ChannelMonitorResult | null): string => {
  if (!result) return 'bg-border/60'
  if (result.success) return 'bg-emerald-500'
  if (result.status === 'balance_paused') return 'bg-amber-500'
  if (result.status === 'auto_paused' || result.status === 'failed') return 'bg-red-500'
  return 'bg-muted-foreground/60'
}

const timelineTitle = (result: ChannelMonitorResult | null): string => {
  if (!result) return t('admin.channelMonitor.timeline.empty')
  return `${formatDateTime(result.createdAt)} · ${statusLabel(result.status)} · ${formatLatency(result.latencyMs)} · ${result.message || '-'}`
}

const timelineNextLabel = (channel: ChannelMonitorChannel): string => {
  if (!channel.enabled) return t('admin.channelMonitor.timeline.monitorOff')
  if (!channel.supported) return t('admin.channelMonitor.status.unsupported')
  return t('admin.channelMonitor.timeline.nextRefresh', { value: formatRelativeShort(channel.nextCheckAt) })
}

const latestStatusLine = (channel: ChannelMonitorChannel): string => (
  `${formatDateTime(channel.lastCheckedAt)} / ${formatLatency(channel.lastLatencyMs)}`
)

const monitorButtonClass = (channel: ChannelMonitorChannel): string => (
  channel.enabled
    ? '!border-amber-500/30 !bg-amber-500/10 !text-amber-700 hover:!bg-amber-500/15 dark:!text-amber-300'
    : '!border-emerald-500/30 !bg-emerald-500/10 !text-emerald-700 hover:!bg-emerald-500/15 dark:!text-emerald-300'
)

const dispatchButtonClass = (channel: ChannelMonitorChannel): string => (
  channel.schedulable === false
    ? '!border-emerald-500/30 !bg-emerald-500/10 !text-emerald-700 hover:!bg-emerald-500/15 dark:!text-emerald-300'
    : '!border-red-500/30 !bg-red-500/10 !text-red-700 hover:!bg-red-500/15 dark:!text-red-300'
)
</script>

<template>
  <div class="h-[calc(100vh-8rem)] flex flex-col gap-4">
    <div class="grid grid-cols-2 gap-3 lg:grid-cols-3 2xl:grid-cols-6">
      <div v-for="card in statCards" :key="card.key" class="rounded-lg border border-border/50 bg-surface p-3">
        <div class="flex items-center justify-between gap-3">
          <div>
            <p class="text-xs font-medium text-muted-foreground">{{ t(`admin.channelMonitor.stats.${card.key}`) }}</p>
            <p class="mt-1 text-2xl font-semibold text-foreground">{{ card.value }}</p>
          </div>
          <div :class="['flex h-9 w-9 items-center justify-center rounded-lg border', card.tone]">
            <component :is="card.icon" class="h-4 w-4" />
          </div>
        </div>
      </div>
    </div>

    <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
      <div class="flex flex-1 flex-col gap-3 md:flex-row">
        <div class="relative w-full md:max-w-sm">
          <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            v-model="searchQuery"
            class="h-10 w-full rounded-xl border border-border/50 bg-surface pl-10 pr-4 text-sm outline-none transition focus:border-primary focus:ring-1 focus:ring-primary"
            :placeholder="t('admin.channelMonitor.filters.search')"
          />
        </div>
        <select v-model="selectedGroup" class="h-10 rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none transition focus:border-primary focus:ring-1 focus:ring-primary">
          <option v-for="group in groupOptions" :key="group" :value="group">
            {{ group === 'all' ? t('admin.channelMonitor.filters.allGroups') : group }}
          </option>
        </select>
        <select v-model="statusFilter" class="h-10 rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none transition focus:border-primary focus:ring-1 focus:ring-primary">
          <option value="all">{{ t('admin.channelMonitor.filters.allStatus') }}</option>
          <option value="healthy">{{ statusLabel('healthy') }}</option>
          <option value="failed">{{ statusLabel('failed') }}</option>
          <option value="auto_paused">{{ statusLabel('auto_paused') }}</option>
          <option value="balance_paused">{{ statusLabel('balance_paused') }}</option>
          <option value="monitor_paused">{{ statusLabel('monitor_paused') }}</option>
          <option value="dispatch_paused">{{ statusLabel('dispatch_paused') }}</option>
          <option value="unsupported">{{ statusLabel('unsupported') }}</option>
        </select>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Button type="button" variant="secondary" class="h-10 gap-2 rounded-xl !border-violet-500/30 !bg-violet-500/10 !text-violet-700 hover:!bg-violet-500/15 dark:!text-violet-300" :disabled="isLoading || isBulkActionLoading" @click="openTestModelEditor">
          <Settings2 class="h-4 w-4" />
          <span>{{ t('admin.channelMonitor.testModel.configure') }}</span>
          <span class="hidden max-w-[260px] truncate font-mono text-[11px] opacity-80 2xl:inline">
            {{ summary.testModelConfig.openaiModelId }} / {{ summary.testModelConfig.anthropicModelId }}
          </span>
        </Button>
        <Button type="button" variant="secondary" class="h-10 gap-2 rounded-xl" :disabled="isLoading || isBulkActionLoading || allRuleIds.length === 0" @click="openBulkEditor('all')">
          <Settings2 class="h-4 w-4" />
          {{ t('admin.channelMonitor.bulk.editAllRules') }}
        </Button>
        <Button type="button" variant="secondary" class="h-10 gap-2 rounded-xl" :disabled="isLoading || isRefreshing" @click="loadSummary('manual')">
          <RefreshCw :class="['h-4 w-4', (isLoading || isRefreshing) ? 'animate-spin' : '']" />
          {{ t('admin.channelMonitor.actions.refresh') }}
        </Button>
      </div>
    </div>

    <div v-if="selectedCount > 0" class="flex flex-wrap items-center gap-2 rounded-lg border border-border/50 bg-surface px-4 py-3">
      <span class="mr-2 text-sm font-medium text-foreground">{{ t('admin.channelMonitor.bulk.selected', { count: selectedCount }) }}</span>
      <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-blue-500/30 !bg-blue-500/10 !text-blue-700 hover:!bg-blue-500/15 dark:!text-blue-300" :disabled="isBulkActionLoading" @click="runSelected">
        <RefreshCw :class="['h-3.5 w-3.5', isActionActive('bulk:run') ? 'animate-spin' : '']" />
        {{ t('admin.channelMonitor.bulk.run') }}
      </Button>
      <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-emerald-500/30 !bg-emerald-500/10 !text-emerald-700 hover:!bg-emerald-500/15 dark:!text-emerald-300" :disabled="isBulkActionLoading" @click="setSelectedMonitoring(true)">
        <Play class="h-3.5 w-3.5" />
        {{ t('admin.channelMonitor.bulk.enableMonitor') }}
      </Button>
      <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-amber-500/30 !bg-amber-500/10 !text-amber-700 hover:!bg-amber-500/15 dark:!text-amber-300" :disabled="isBulkActionLoading" @click="setSelectedMonitoring(false)">
        <PauseCircle class="h-3.5 w-3.5" />
        {{ t('admin.channelMonitor.bulk.disableMonitor') }}
      </Button>
      <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-emerald-500/30 !bg-emerald-500/10 !text-emerald-700 hover:!bg-emerald-500/15 dark:!text-emerald-300" :disabled="isBulkActionLoading" @click="setSelectedSchedulable(true)">
        <Power class="h-3.5 w-3.5" />
        {{ t('admin.channelMonitor.bulk.enableDispatch') }}
      </Button>
      <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-red-500/30 !bg-red-500/10 !text-red-700 hover:!bg-red-500/15 dark:!text-red-300" :disabled="isBulkActionLoading" @click="setSelectedSchedulable(false)">
        <PowerOff class="h-3.5 w-3.5" />
        {{ t('admin.channelMonitor.bulk.disableDispatch') }}
      </Button>
      <Button type="button" variant="ghost" size="sm" class="gap-1.5" :disabled="isBulkActionLoading" @click="openBulkEditor('selected')">
        <Settings2 class="h-3.5 w-3.5" />
        {{ t('admin.channelMonitor.bulk.editSelectedRules') }}
      </Button>
    </div>

    <div v-if="errorKey" class="rounded-xl border border-warning/20 bg-warning/10 p-3 text-sm text-warning">
      {{ t(errorKey) }}
    </div>

    <section class="rounded-lg border border-border/50 bg-surface px-5 py-4">
      <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-sm font-semibold text-foreground">{{ t('admin.channelMonitor.rateRule.title') }}</h2>
            <span :class="['rounded-md border px-2 py-0.5 text-[11px] font-medium', summary.rateRule.rule.enabled ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : 'border-zinc-500/20 bg-zinc-500/10 text-zinc-600']">
              {{ summary.rateRule.rule.enabled ? t('admin.channelMonitor.rateRule.enabled') : t('admin.channelMonitor.rateRule.disabled') }}
            </span>
            <span v-if="summary.rateRule.rule.updatePriority" class="rounded-md border border-blue-500/20 bg-blue-500/10 px-2 py-0.5 text-[11px] font-medium text-blue-700 dark:text-blue-300">
              {{ t('admin.channelMonitor.rateRule.priorityOn') }}
            </span>
          </div>
          <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.channelMonitor.rateRule.subtitle') }}</p>
        </div>
        <div class="grid grid-cols-2 gap-2 text-xs sm:grid-cols-4 xl:min-w-[420px]">
          <div class="rounded-md bg-surface-elevated px-3 py-2">
            <div class="text-muted-foreground">{{ t('admin.channelMonitor.rateRule.allowed') }}</div>
            <div class="mt-1 font-mono text-foreground">{{ summary.rateRule.summary.allowed }}</div>
          </div>
          <div class="rounded-md bg-surface-elevated px-3 py-2">
            <div class="text-muted-foreground">{{ t('admin.channelMonitor.rateRule.blocked') }}</div>
            <div class="mt-1 font-mono text-red-600 dark:text-red-300">{{ summary.rateRule.summary.blocked }}</div>
          </div>
          <div class="rounded-md bg-surface-elevated px-3 py-2">
            <div class="text-muted-foreground">{{ t('admin.channelMonitor.rateRule.dispatchChanges') }}</div>
            <div class="mt-1 font-mono text-foreground">{{ summary.rateRule.summary.wouldEnable }}/{{ summary.rateRule.summary.wouldDisable }}</div>
          </div>
          <div class="rounded-md bg-surface-elevated px-3 py-2">
            <div class="text-muted-foreground">{{ t('admin.channelMonitor.rateRule.priorityChanges') }}</div>
            <div class="mt-1 font-mono text-foreground">{{ summary.rateRule.summary.priorityChanges }}</div>
          </div>
        </div>
        <div class="flex flex-wrap justify-end gap-2">
          <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-blue-500/30 !bg-blue-500/10 !text-blue-700 hover:!bg-blue-500/15 dark:!text-blue-300" :disabled="isBulkActionLoading" @click="previewRateRule">
            <RefreshCw :class="['h-3.5 w-3.5', isActionActive('rate-rule:preview') ? 'animate-spin' : '']" />
            {{ t('admin.channelMonitor.rateRule.preview') }}
          </Button>
          <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-emerald-500/30 !bg-emerald-500/10 !text-emerald-700 hover:!bg-emerald-500/15 dark:!text-emerald-300" :disabled="isBulkActionLoading || !summary.rateRule.rule.enabled" @click="applyRateRule">
            <Play class="h-3.5 w-3.5" />
            {{ t('admin.channelMonitor.rateRule.apply') }}
          </Button>
          <Button type="button" variant="secondary" size="sm" class="gap-1.5" :disabled="isBulkActionLoading" @click="openRateRuleEditor">
            <Settings2 class="h-3.5 w-3.5" />
            {{ t('admin.channelMonitor.rateRule.configure') }}
          </Button>
        </div>
      </div>
      <div v-if="summary.rateRule.lastResult" class="mt-3 text-xs text-muted-foreground">
        {{ t('admin.channelMonitor.rateRule.lastApplied', { time: formatDateTime(summary.rateRule.lastResult.createdAt), enabled: summary.rateRule.lastResult.enabledCount, disabled: summary.rateRule.lastResult.disabledCount, priority: summary.rateRule.lastResult.priorityUpdated }) }}
      </div>
    </section>

    <div class="grid min-h-0 flex-1 grid-cols-1 gap-3 xl:grid-cols-[minmax(260px,340px)_1fr]">
      <section class="min-h-0 overflow-hidden rounded-lg border border-border/50 bg-surface">
        <div class="border-b border-border/50 px-4 py-3">
          <h2 class="text-sm font-semibold text-foreground">{{ t('admin.channelMonitor.groups.title') }}</h2>
          <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.channelMonitor.groups.subtitle') }}</p>
        </div>
        <div class="max-h-full overflow-auto">
          <table class="w-full text-left text-xs">
            <thead class="sticky top-0 bg-surface-elevated text-xs text-muted-foreground">
              <tr>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.groups.columns.group') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.groups.columns.available') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.groups.columns.paused') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.groups.columns.last') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border/40">
              <tr
                v-for="group in summary.groups"
                :key="`${group.groupName}-${group.platform}`"
                :class="[
                  'cursor-pointer transition-colors',
                  groupRowClass(group.groupName, group.platform)
                ]"
                @click="selectGroup(group.groupName)"
              >
                <td class="px-3 py-2">
                  <div class="font-medium text-foreground">{{ group.groupName }}</div>
                  <span :class="['mt-1 inline-flex rounded-md border px-1.5 py-0.5 text-[11px] font-medium', groupPlatformBadgeClass(group.platform)]">
                    {{ group.platform || t('admin.channelMonitor.common.unknown') }}
                  </span>
                </td>
                <td class="px-3 py-2 font-mono text-foreground">{{ group.available }}/{{ group.total }}</td>
                <td class="px-3 py-2 text-xs text-muted-foreground">{{ group.monitorPaused }}/{{ group.dispatchPaused }}</td>
                <td class="px-3 py-2 text-xs text-muted-foreground">{{ formatDateTime(group.lastCheckedAt) }}</td>
              </tr>
              <tr v-if="!isLoading && summary.groups.length === 0">
                <td colspan="4" class="px-4 py-10 text-center text-sm text-muted-foreground">{{ t('admin.channelMonitor.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="min-h-0 overflow-hidden rounded-lg border border-border/50 bg-surface">
        <div class="flex items-center justify-between gap-3 border-b border-border/50 px-4 py-3">
          <div>
            <h2 class="text-sm font-semibold text-foreground">{{ t('admin.channelMonitor.channels.title') }}</h2>
            <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.channelMonitor.channels.subtitle', { count: filteredChannels.length }) }}</p>
          </div>
          <button type="button" class="inline-flex items-center gap-2 rounded-lg px-2 py-1 text-xs text-muted-foreground hover:bg-surface-elevated hover:text-foreground" @click="toggleSelectAllVisible">
            <CheckSquare2 v-if="allVisibleSelected" class="h-4 w-4 text-primary" />
            <Square v-else class="h-4 w-4" />
            {{ t('admin.channelMonitor.actions.selectAll') }}
          </button>
        </div>

        <div v-if="isLoading" class="flex h-64 items-center justify-center text-muted-foreground">
          <Loader2 class="mr-2 h-5 w-5 animate-spin" />
          {{ t('admin.channelMonitor.loading') }}
        </div>

        <div v-else class="max-h-full overflow-auto">
          <table class="w-full min-w-[1860px] table-fixed text-left text-xs">
            <colgroup>
              <col style="width: 2%;" />
              <col style="width: 36%;" />
              <col style="width: 5%;" />
              <col style="width: 5%;" />
              <col style="width: 7%;" />
              <col style="width: 5%;" />
              <col style="width: 7%;" />
              <col style="width: 6%;" />
              <col style="width: 5%;" />
              <col style="width: 10.5%;" />
              <col style="width: 11.5%;" />
            </colgroup>
            <thead class="sticky top-0 bg-surface-elevated text-xs text-muted-foreground">
              <tr>
                <th class="w-9 px-3 py-2 font-medium"></th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.channels.columns.channel') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.rateRule.upstreamRate') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.rateRule.ownRate') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.rateRule.priority') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.rateRule.accountRate') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.channels.columns.group') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.channels.columns.status') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.channels.columns.balance') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('admin.channelMonitor.channels.columns.last') }}</th>
                <th class="px-3 py-2 text-right font-medium">{{ t('admin.channelMonitor.channels.columns.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border/40">
              <tr v-for="channel in filteredChannels" :key="channel.connectionId" class="hover:bg-surface-elevated/60">
                <td class="px-3 py-3 align-top">
                  <button type="button" class="mt-1 text-muted-foreground hover:text-primary" @click="toggleSelect(channel.ruleId)">
                    <CheckSquare2 v-if="selectedRuleIds.includes(channel.ruleId)" class="h-4 w-4 text-primary" />
                    <Square v-else class="h-4 w-4" />
                  </button>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="flex items-start gap-3">
                    <div class="min-w-[300px] flex-1">
                      <div class="font-medium text-foreground">{{ channel.adminAccountName || channel.adminAccountId }}</div>
                      <div class="mt-1 text-xs text-muted-foreground">{{ channel.siteName }} · {{ channel.upstreamGroupName }}</div>
                      <div class="mt-1.5 flex flex-wrap gap-1">
                        <span :class="['rounded-md border px-2 py-0.5 text-[11px] font-medium', channel.enabled ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-600' : 'border-sky-500/20 bg-sky-500/10 text-sky-600']">
                          {{ channel.enabled ? t('admin.channelMonitor.flags.monitorOn') : t('admin.channelMonitor.flags.monitorOff') }}
                        </span>
                        <span :class="['rounded-md border px-2 py-0.5 text-[11px] font-medium', channel.schedulable === false ? 'border-zinc-500/20 bg-zinc-500/10 text-zinc-600' : 'border-emerald-500/20 bg-emerald-500/10 text-emerald-600']">
                          {{ channel.schedulable === false ? t('admin.channelMonitor.flags.dispatchOff') : t('admin.channelMonitor.flags.dispatchOn') }}
                        </span>
                        <span :class="['rounded-md border px-2 py-0.5 text-[11px] font-medium', rateGateClass(channel.rateGateStatus)]">
                          {{ rateGateLabel(channel.rateGateStatus) }}
                        </span>
                      </div>
                      <div v-if="channel.rateGateMessage" class="mt-1 max-w-[390px] truncate text-xs" :class="channel.rateGateStatus === 'blocked' ? 'text-red-600 dark:text-red-300' : 'text-muted-foreground'" :title="channel.rateGateMessage">
                        {{ channel.rateGateMessage }}
                      </div>
                    </div>

                    <div class="w-[360px] shrink-0 rounded-lg border border-border/50 bg-background/60 p-2 shadow-sm">
                      <div class="flex items-center justify-between gap-3 text-xs font-medium text-muted-foreground">
                        <span>{{ t('admin.channelMonitor.timeline.window') }}</span>
                        <span class="truncate">{{ timelineNextLabel(channel) }}</span>
                      </div>
                      <div class="mt-1.5 grid h-5 grid-cols-[repeat(60,minmax(2px,1fr))] gap-0.5">
                        <span
                          v-for="(result, index) in timelineItems(channel)"
                          :key="`${channel.ruleId}-${index}-${result?.id ?? 'empty'}`"
                          :class="['h-5 rounded-[2px]', timelineClass(result)]"
                          :title="timelineTitle(result)"
                        />
                      </div>
                      <div class="mt-1 flex justify-between text-[10px] font-medium uppercase text-muted-foreground/70">
                        <span>{{ t('admin.channelMonitor.timeline.past') }}</span>
                        <span>{{ t('admin.channelMonitor.timeline.now') }}</span>
                      </div>
                      <div class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                        <span class="font-mono text-foreground">{{ channel.recentTotal ? `${channel.uptimePercent.toFixed(0)}%` : '-' }}</span>
                        <span>{{ t('admin.channelMonitor.timeline.successCount', { success: channel.recentSuccess, total: channel.recentTotal }) }}</span>
                        <span>{{ latestStatusLine(channel) }}</span>
                      </div>
                      <div v-if="channel.lastMessage" class="mt-1 max-w-[340px] truncate text-xs" :class="channel.status === 'healthy' ? 'text-muted-foreground' : 'text-red-600 dark:text-red-300'" :title="channel.lastMessage">
                        {{ channel.lastMessage }}
                      </div>
                    </div>
                  </div>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="font-mono text-xs font-semibold text-foreground">{{ formatMultiplier(channel.upstreamEffectiveMultiplier) }}</div>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="font-mono text-xs font-semibold text-foreground">{{ formatMultiplier(channel.ownGroupMultiplier) }}</div>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="font-mono text-xs font-semibold text-foreground">{{ channel.accountPriority ?? '-' }} → {{ channel.recommendedPriority ?? '-' }}</div>
                  <div class="mt-1 flex items-center gap-1">
                    <input
                      v-model.number="priorityDrafts[channel.ruleId]"
                      type="number"
                      min="0"
                      max="999"
                      class="h-7 w-14 rounded-md border border-border/50 bg-surface px-2 text-xs font-mono text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                      :disabled="isChannelBusy(channel) || !channel.supported"
                      :aria-label="t('admin.channelMonitor.rateRule.manualPriority')"
                    />
                    <Button
                      type="button"
                      variant="secondary"
                      size="sm"
                      class="h-7 px-2 text-xs !border-amber-500/30 !bg-amber-500/10 !text-amber-700 hover:!bg-amber-500/15 dark:!text-amber-300"
                      :disabled="isChannelBusy(channel) || !channel.supported"
                      @click="setChannelPriority(channel)"
                    >
                      <Loader2 v-if="isActionActive(channelActionKey(channel, 'priority'))" class="h-3 w-3 animate-spin" />
                      <span v-else>{{ t('admin.channelMonitor.rateRule.setPriority') }}</span>
                    </Button>
                  </div>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="font-mono text-xs font-semibold text-foreground">{{ formatMultiplier(channel.accountRateMultiplier) }}</div>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="flex max-w-[140px] flex-wrap gap-1">
                    <span v-for="group in channel.ownGroups" :key="group" class="rounded-md border border-border/50 bg-surface-elevated px-2 py-0.5 text-xs font-medium text-muted-foreground">
                      {{ group }}
                    </span>
                  </div>
                  <div class="mt-1 text-xs text-muted-foreground">{{ channel.groupType || t('admin.channelMonitor.common.unknown') }}</div>
                </td>
                <td class="px-3 py-3 align-top">
                  <span :class="['inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-semibold', statusClass(channel.status)]">
                    <component :is="statusIcon(channel.status)" class="h-3.5 w-3.5" />
                    {{ statusLabel(channel.status) }}
                  </span>
                  <div v-if="channel.consecutiveFailures > 0" class="mt-1 text-xs text-muted-foreground">
                    {{ t('admin.channelMonitor.channels.failures', { count: channel.consecutiveFailures }) }}
                  </div>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="font-mono text-foreground">{{ formatMoney(channel.balance) }}</div>
                  <div class="text-xs text-muted-foreground">{{ t('admin.channelMonitor.channels.threshold', { value: channel.balanceThreshold }) }}</div>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Clock3 class="h-3.5 w-3.5" />
                    {{ formatDateTime(channel.lastCheckedAt) }}
                  </div>
                  <div class="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Gauge class="h-3.5 w-3.5" />
                    {{ formatLatency(channel.lastLatencyMs) }}
                  </div>
                  <div class="mt-1 text-xs text-muted-foreground">{{ t('admin.channelMonitor.channels.next', { value: timelineNextLabel(channel) }) }}</div>
                  <div class="mt-1 max-w-[200px] truncate text-xs" :class="channel.status === 'healthy' ? 'text-muted-foreground' : 'text-red-600 dark:text-red-300'" :title="channel.lastMessage">{{ channel.lastMessage || '-' }}</div>
                </td>
                <td class="px-3 py-3 align-top">
                  <div class="flex max-w-[210px] flex-wrap justify-end gap-1">
                    <Button type="button" variant="secondary" size="sm" class="h-8 gap-1 !border-blue-500/30 !bg-blue-500/10 px-2 text-xs !text-blue-700 hover:!bg-blue-500/15 dark:!text-blue-300" :disabled="isChannelBusy(channel) || !channel.supported" :title="t('admin.channelMonitor.actions.run')" @click="runAction(() => runChannelMonitorRule(channel.ruleId), { actionKey: channelActionKey(channel, 'run') })">
                      <RefreshCw :class="['h-3.5 w-3.5', isActionActive(channelActionKey(channel, 'run')) ? 'animate-spin' : '']" />
                      {{ t('admin.channelMonitor.actions.runShort') }}
                    </Button>
                    <Button type="button" variant="secondary" size="sm" :class="['h-8 gap-1 px-2 text-xs', monitorButtonClass(channel)]" :disabled="isChannelBusy(channel)" :title="channel.enabled ? t('admin.channelMonitor.actions.disableMonitor') : t('admin.channelMonitor.actions.enableMonitor')" @click="toggleChannelMonitoring(channel)">
                      <Loader2 v-if="isActionActive(channelActionKey(channel, 'monitor'))" class="h-3.5 w-3.5 animate-spin" />
                      <Play v-else-if="!channel.enabled" class="h-3.5 w-3.5" />
                      <PauseCircle v-else class="h-3.5 w-3.5" />
                      {{ channel.enabled ? t('admin.channelMonitor.actions.disableMonitorShort') : t('admin.channelMonitor.actions.enableMonitorShort') }}
                    </Button>
                    <Button type="button" variant="secondary" size="sm" :class="['h-8 gap-1 px-2 text-xs', dispatchButtonClass(channel)]" :disabled="isChannelBusy(channel) || !channel.supported" :title="channel.schedulable === false ? t('admin.channelMonitor.actions.enableDispatch') : t('admin.channelMonitor.actions.disableDispatch')" @click="toggleChannelSchedulable(channel)">
                      <Loader2 v-if="isActionActive(channelActionKey(channel, 'dispatch'))" class="h-3.5 w-3.5 animate-spin" />
                      <Power v-else-if="channel.schedulable === false" class="h-3.5 w-3.5" />
                      <PowerOff v-else class="h-3.5 w-3.5" />
                      {{ channel.schedulable === false ? t('admin.channelMonitor.actions.enableDispatchShort') : t('admin.channelMonitor.actions.disableDispatchShort') }}
                    </Button>
                    <Button type="button" variant="ghost" size="sm" class="h-8 px-2 text-xs" :disabled="isChannelBusy(channel)" @click="openEditor(channel)">
                      <Settings2 class="h-3.5 w-3.5" />
                    </Button>
                    <Button type="button" variant="secondary" size="sm" class="h-8 gap-1 !border-red-500/30 !bg-red-500/10 px-2 text-xs !text-red-700 hover:!bg-red-500/15 dark:!text-red-300" :disabled="isChannelBusy(channel)" :title="t('admin.channelMonitor.disconnect.action')" @click="openDisconnect(channel)">
                      <Trash2 class="h-3.5 w-3.5" />
                      {{ t('admin.channelMonitor.disconnect.actionShort') }}
                    </Button>
                  </div>
                </td>
              </tr>
              <tr v-if="filteredChannels.length === 0">
                <td colspan="11" class="px-5 py-16 text-center text-sm text-muted-foreground">{{ t('admin.channelMonitor.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>

    <div v-if="editingChannel || isBulkEditorOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div class="w-full max-w-md rounded-xl border border-border/50 bg-card p-6 shadow-xl">
        <h2 class="text-lg font-semibold text-foreground">
          {{ editingChannel ? t('admin.channelMonitor.editor.title') : t(bulkEditorScope === 'all' ? 'admin.channelMonitor.editor.allTitle' : 'admin.channelMonitor.editor.bulkTitle', { count: bulkEditorCount }) }}
        </h2>
        <p v-if="isBulkEditorOpen" class="mt-1 text-xs text-muted-foreground">
          {{ t(bulkEditorScope === 'all' ? 'admin.channelMonitor.editor.allDescription' : 'admin.channelMonitor.editor.bulkDescription', { count: bulkEditorCount }) }}
        </p>
        <div class="mt-5 space-y-4">
          <label class="flex items-center justify-between gap-4">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.editor.enabled') }}</span>
            <input v-model="editForm.enabled" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.editor.interval') }}</span>
            <input v-model.number="editForm.checkIntervalMinutes" type="number" min="1" max="1440" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.editor.failureThreshold') }}</span>
            <input v-model.number="editForm.failureThreshold" type="number" min="1" max="100" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.editor.balanceThreshold') }}</span>
            <input v-model.number="editForm.balanceThreshold" type="number" min="0" step="0.01" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
          </label>
        </div>
        <div class="mt-6 flex justify-end gap-2">
          <Button variant="secondary" :disabled="isActionLoading" @click="closeEditor">{{ t('admin.channelMonitor.actions.cancel') }}</Button>
          <Button class="gap-2" :disabled="isActionLoading" @click="saveEditor">
            <Loader2 v-if="isActionLoading" class="h-4 w-4 animate-spin" />
            {{ t('admin.channelMonitor.actions.save') }}
          </Button>
        </div>
      </div>
    </div>

    <div v-if="isTestModelEditorOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div class="w-full max-w-lg rounded-xl border border-border/50 bg-card p-6 shadow-xl">
        <h2 class="text-lg font-semibold text-foreground">{{ t('admin.channelMonitor.testModel.title') }}</h2>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('admin.channelMonitor.testModel.description') }}</p>
        <div class="mt-5 grid gap-4">
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.testModel.openai') }}</span>
            <input v-model.trim="testModelForm.openaiModelId" type="text" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 font-mono text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" placeholder="gpt-5.4" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.testModel.anthropic') }}</span>
            <input v-model.trim="testModelForm.anthropicModelId" type="text" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 font-mono text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" placeholder="claude-sonnet-4-6" />
          </label>
          <div class="rounded-lg border border-border/50 bg-surface-elevated px-3 py-2 text-xs text-muted-foreground">
            {{ t('admin.channelMonitor.testModel.current', { openai: summary.testModelConfig.openaiModelId, anthropic: summary.testModelConfig.anthropicModelId }) }}
          </div>
        </div>
        <div class="mt-6 flex justify-end gap-2">
          <Button variant="secondary" :disabled="isActionActive('test-model:save')" @click="closeTestModelEditor">{{ t('admin.channelMonitor.actions.cancel') }}</Button>
          <Button class="gap-2" :disabled="isActionActive('test-model:save')" @click="saveTestModelConfig">
            <Loader2 v-if="isActionActive('test-model:save')" class="h-4 w-4 animate-spin" />
            {{ t('admin.channelMonitor.actions.save') }}
          </Button>
        </div>
      </div>
    </div>

    <div v-if="disconnectingChannel" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div class="w-full max-w-lg rounded-xl border border-border/50 bg-card shadow-xl">
        <div class="border-b border-border/50 px-6 py-5">
          <h2 class="text-lg font-semibold text-foreground">{{ t('admin.channelMonitor.disconnect.title') }}</h2>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ t('admin.channelMonitor.disconnect.description', { channel: disconnectingChannel.adminAccountName || disconnectingChannel.adminAccountId }) }}
          </p>
        </div>
        <div v-if="disconnectError" class="mx-6 mt-5 rounded-xl border border-warning/20 bg-warning/10 p-3 text-sm text-warning">
          {{ disconnectError }}
        </div>
        <div class="space-y-3 px-6 py-5">
          <label
            :class="[
              'flex cursor-pointer gap-3 rounded-xl border p-4 transition',
              disconnectMode === 'unlink' ? 'border-primary/40 bg-primary/10' : 'border-border/50 bg-surface hover:bg-surface-elevated'
            ]"
          >
            <input v-model="disconnectMode" type="radio" value="unlink" class="mt-1 h-4 w-4 border-border text-primary focus:ring-primary" />
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.disconnect.unlinkOnly') }}</span>
              <span class="mt-1 block text-xs text-muted-foreground">{{ t('admin.channelMonitor.disconnect.unlinkOnlyHint') }}</span>
            </span>
          </label>
          <label
            :class="[
              'flex cursor-pointer gap-3 rounded-xl border p-4 transition',
              disconnectMode === 'full' ? 'border-red-500/40 bg-red-500/10' : 'border-border/50 bg-surface hover:bg-surface-elevated'
            ]"
          >
            <input v-model="disconnectMode" type="radio" value="full" class="mt-1 h-4 w-4 border-border text-red-600 focus:ring-red-500" />
            <span>
              <span class="block text-sm font-medium text-red-600 dark:text-red-400">{{ t('admin.channelMonitor.disconnect.deleteAll') }}</span>
              <span class="mt-1 block text-xs text-red-500/80">{{ t('admin.channelMonitor.disconnect.deleteAllHint') }}</span>
            </span>
          </label>
        </div>
        <div class="flex justify-end gap-2 border-t border-border/50 px-6 py-4">
          <Button variant="secondary" :disabled="isActionActive(channelActionKey(disconnectingChannel, 'disconnect'))" @click="closeDisconnect">{{ t('admin.channelMonitor.actions.cancel') }}</Button>
          <Button :variant="disconnectMode === 'full' ? 'destructive' : 'default'" class="gap-2" :disabled="isActionActive(channelActionKey(disconnectingChannel, 'disconnect'))" @click="confirmDisconnect">
            <Loader2 v-if="isActionActive(channelActionKey(disconnectingChannel, 'disconnect'))" class="h-4 w-4 animate-spin" />
            {{ t('admin.channelMonitor.disconnect.confirm') }}
          </Button>
        </div>
      </div>
    </div>

    <div v-if="isRateRuleEditorOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div class="w-full max-w-lg rounded-xl border border-border/50 bg-card p-6 shadow-xl">
        <h2 class="text-lg font-semibold text-foreground">{{ t('admin.channelMonitor.rateRule.configureTitle') }}</h2>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('admin.channelMonitor.rateRule.configureDescription') }}</p>
        <div class="mt-5 space-y-4">
          <label class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.rateRule.form.enabled') }}</span>
              <span class="block text-xs text-muted-foreground">{{ t('admin.channelMonitor.rateRule.form.enabledHelp') }}</span>
            </span>
            <input v-model="rateRuleForm.enabled" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
          <label class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.rateRule.form.autoApplyOnCheck') }}</span>
              <span class="block text-xs text-muted-foreground">{{ t('admin.channelMonitor.rateRule.form.autoApplyOnCheckHelp') }}</span>
            </span>
            <input v-model="rateRuleForm.autoApplyOnCheck" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
          <label class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.rateRule.form.updatePriority') }}</span>
              <span class="block text-xs text-muted-foreground">{{ t('admin.channelMonitor.rateRule.form.updatePriorityHelp') }}</span>
            </span>
            <input v-model="rateRuleForm.updatePriority" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
          <label class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.rateRule.form.stopWhenMissingRate') }}</span>
              <span class="block text-xs text-muted-foreground">{{ t('admin.channelMonitor.rateRule.form.stopWhenMissingRateHelp') }}</span>
            </span>
            <input v-model="rateRuleForm.stopWhenMissingRate" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
        </div>
        <div class="mt-6 flex flex-wrap justify-end gap-2">
          <Button variant="secondary" :disabled="isActionLoading" @click="closeRateRuleEditor">{{ t('admin.channelMonitor.actions.cancel') }}</Button>
          <Button variant="secondary" class="gap-2" :disabled="isActionLoading" @click="saveRateRule(false)">
            <Loader2 v-if="isActionLoading" class="h-4 w-4 animate-spin" />
            {{ t('admin.channelMonitor.actions.save') }}
          </Button>
          <Button class="gap-2" :disabled="isActionLoading" @click="saveRateRule(true)">
            <Loader2 v-if="isActionLoading" class="h-4 w-4 animate-spin" />
            {{ t('admin.channelMonitor.rateRule.saveAndApply') }}
          </Button>
        </div>
      </div>
    </div>
  </div>
</template>
