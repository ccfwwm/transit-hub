<script setup lang="ts">
import PrimaryModelPicker from '../components/PrimaryModelPicker.vue'
import GroupModelStatus from '../components/GroupModelStatus.vue'
import ModelTestDialog from '../components/ModelTestDialog.vue'
import MonitorBindingSync from '../components/MonitorBindingSync.vue'
import OperationProgress from '../components/OperationProgress.vue'
import StatusSourceControl from '../components/StatusSourceControl.vue'
import { keyJobMessage, previewKeyRotation, rotateKeys } from '../api/keyRotation'
import { syncKeyWhitelist } from '../api/keyWhitelist'
import GroupKeyPolicy from '../components/GroupKeyPolicy.vue'
import KeyPolicyEditor from '../components/KeyPolicyEditor.vue'
import ChannelKeyActions from '../components/ChannelKeyActions.vue'
import { submitOperation, checkedOperation, operationJSON, followJob, type OperationJob } from '../api/operations'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
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
  previewChannelMonitorRateRule,
  bulkSyncAndTakeOverChannelMonitorRules,
  bulkSetChannelMonitorRulesSchedulable,
  bulkUpdateChannelMonitorRules,
  getChannelMonitorSummary,
  getChannelMonitorPage,
  getMonitorPageHistory,
  getMonitorStats,
  getMonitorGroups,
  getMonitorMediaPresets,
  resetMonitorMediaTask,
  getMonitorTargetHistory,
  setChannelMonitorRuleSchedulable,
  setChannelMonitorRulePriority,
  syncAndTakeOverChannelMonitorRule,
  updateChannelMonitorRateRule,
  updateChannelMonitorPriorityGroupRule,
  updateChannelMonitorRule,
  updateChannelMonitorTestModelConfig,
} from '../api/channelMonitor'
import { getMySiteMappingOptions, listRealConnections, realDisconnect, repairRealConnectionAccount, updateRealConnectionGroups } from '../api/mySites'
import type { ChannelMonitorChannel, ChannelMonitorGroup, ChannelMonitorGroupModelConfig, ChannelMonitorRateRule, ChannelMonitorResult, ChannelMonitorStatus, RateGateStatus, UpdateChannelMonitorRuleRequest } from '../types/channelMonitor'
import type { MySiteMappingOwnGroupOption, RealDisconnectRequest } from '../types/mySites'

type MediaPreset = Awaited<ReturnType<typeof getMonitorMediaPresets>>[number]
type StatusFilter = 'all' | 'monitor_paused' | 'dispatch_paused' | ChannelMonitorStatus
type GroupStatusFilter = 'all' | 'available' | 'unavailable'
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
const testingChannel = ref<ChannelMonitorChannel | null>(null)
const errorKey = ref('')
const syncingHealth=ref(false)
const healthSyncMessage=ref('')
async function syncCurrentHealth(){
 if(syncingHealth.value)return
 const sites=[...new Set(summary.value.channels.filter(c=>!selectedRuleIds.value.length||selectedRuleIds.value.includes(c.ruleId)).map(c=>c.siteId))]
 if(!sites.length)return
 syncingHealth.value=true;healthSyncMessage.value=''
 let done=0,failed=0
 const pending=[...sites]
 await Promise.all(Array.from({length:Math.min(4,pending.length)},async()=>{
   while(pending.length){const site=pending.shift()!;try{const job=await followJob(await operationJSON<OperationJob>(`/upstream-sites/${encodeURIComponent(site)}/model-health/refresh`,{method:'POST'}));if(job.failed)failed++;else done++}catch{failed++}healthSyncMessage.value=`已同步 ${done}/${sites.length} 个上游${failed?`，${failed} 个失败`:''}`}
 }))
 await checkedOperation({kind:'refresh'}).catch(()=>{})
 syncingHealth.value=false
 healthSyncVersion.value++
}
const healthSyncVersion=ref(0)

const searchQuery = ref('')
const statusFilter = ref<StatusFilter>('all')
const dispatchFilter = ref('all')
const groupStatusFilter = ref<GroupStatusFilter>('all')
const selectedGroup = ref('all')
const page = ref(1)
const pageSize = ref(25)
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
    priorityGroups: [],
    lastResult: null,
  },
  testModelConfig: {
    openaiModelId: 'gpt-5.6-sol',
    anthropicModelId: 'claude-sonnet-5',
    grokModelId: 'grok-4.5',
    groupModels: [],
    balanceRefreshIntervalMinutes: 5,
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
const groupEditingChannel = ref<ChannelMonitorChannel | null>(null)
const groupEditorOwnGroups = ref<MySiteMappingOwnGroupOption[]>([])
const groupEditorOwnGroupIds = ref<string[]>([])
const groupEditorStaleCount = ref(0)
const groupEditorLoading = ref(false)
const groupEditorError = ref('')
const editForm = ref({ enabled: true, checkIntervalMinutes: 2, failureThreshold: 2, balanceThreshold: 1, upstreamMultiplierOverride: null as number | null, allowWhenUpstreamRateGteOwn: false, useDefaultTestModel: true, testModelId: '' })
const rateRuleForm = ref({ enabled: false, autoApplyOnCheck: true, updatePriority: true, stopWhenMissingRate: true })
const testModelForm = ref({ openaiModelId: 'gpt-5.6-sol', anthropicModelId: 'claude-sonnet-5', grokModelId: 'grok-4.5', groupModels: [] as { groupId: string, groupName: string, modelId: string, mode?: 'text' | 'image' | 'video' | 'audio' | 'connection', prompt?: string, size?: string, seconds?: number, adapter?: string, presetModel?: string, intervalMinutes?: number }[], balanceRefreshIntervalMinutes: 5 })
const priorityDrafts = ref<Record<string, number | null>>({})
let summaryRequestID = 0
let pageRequestID = 0
let summaryAbort: AbortController | undefined
let pageAbort: AbortController | undefined
let searchTimer: ReturnType<typeof setTimeout> | undefined
let mediaTimer: ReturnType<typeof setInterval> | undefined
const debouncedSearch = ref('')
const channelsLoading = ref(true)
const channelsReading = ref(false)
const lastPageVersion = ref(0)
const lastPageQuery = ref('')
let summaryReading = false
const channelTotal = ref(0)
const filteredRuleIds = ref<string[]>([])
const mediaPresets = ref<MediaPreset[]>([])

const pageQuery = (): Record<string,string> => ({groupId: selectedGroup.value === 'all' ? '' : selectedGroup.value, groupStatus: groupStatusFilter.value, status: statusFilter.value, dispatch:dispatchFilter.value, q: debouncedSearch.value, page: String(page.value), pageSize: String(pageSize.value)})
const loadChannels = async (poll=false) => {
  if(poll&&channelsReading.value)return
  channelsReading.value=true
  const query=pageQuery()
  const queryKey=JSON.stringify(query)
  const requestID = ++pageRequestID
  pageAbort?.abort()
  const controller = new AbortController()
  pageAbort = controller
  try {
    const result = await getChannelMonitorPage(query, controller.signal)
    if (requestID !== pageRequestID) return
    if(lastPageVersion.value===result.snapshotVersion&&lastPageQuery.value===queryKey)return
    lastPageVersion.value=result.snapshotVersion||0;lastPageQuery.value=queryKey
    channelTotal.value = result.total
    filteredRuleIds.value = result.ruleIds
    selectedRuleIds.value = selectedRuleIds.value.filter(id => result.ruleIds.includes(id))
    summary.value.channels = result.channels
    if(result.channels.some(ch=>!ch.modelSummary))void getMonitorPageHistory(query,controller.signal).then(history=>{
      if(requestID!==pageRequestID||history.snapshotVersion!==result.snapshotVersion)return
      summary.value.channels=summary.value.channels.map(ch=>({...ch,recentResults:history.histories[ch.ruleId+'|'+ch.detectionGroupId]||[]}))
    }).catch(()=>{})
    for (const channel of result.channels) priorityDrafts.value[channel.ruleId] = channel.accountPriority ?? channel.recommendedPriority ?? 0
  } catch (error: any) {
    if (!controller.signal.aborted && requestID === pageRequestID) errorKey.value = error?.message ?? 'admin.channelMonitor.errors.request'
  }finally{if(requestID===pageRequestID){channelsLoading.value=false;channelsReading.value=false}}
}

const loadSummary = async (mode: RefreshMode = 'manual') => {
  if(mode==='silent'&&summaryReading)return
  summaryReading=true
  const requestID = ++summaryRequestID
  summaryAbort?.abort()
  const controller = new AbortController()
  summaryAbort = controller
  if (mode === 'initial') isLoading.value = true
  else if (mode === 'manual') isRefreshing.value = true
  errorKey.value = ''
  try {
    const group=selectedGroup.value==='all'?undefined:selectedGroup.value
    await Promise.allSettled([
      getMonitorStats(group,controller.signal).then(next=>{if(requestID===summaryRequestID)summary.value={...summary.value,...next}}),
      getMonitorGroups(group,controller.signal).then(next=>{if(requestID===summaryRequestID)summary.value={...summary.value,...next}}),
      getChannelMonitorSummary(group,{light:true,signal:controller.signal}).then(next=>{
        if(requestID!==summaryRequestID)return
        summary.value={...next,stats:summary.value.stats,groups:summary.value.groups,channels:summary.value.channels}
      })
    ])
  } catch (error: any) {
    if (!controller.signal.aborted && requestID === summaryRequestID) errorKey.value = error?.message ?? 'admin.channelMonitor.errors.request'
  } finally {
    if (requestID === summaryRequestID) {isLoading.value = false; isRefreshing.value = false;summaryReading=false}
  }
}

const refreshChannel = async (_ruleId: string) => {
  const next = await getChannelMonitorSummary(selectedGroupID.value || undefined, {light: true, refresh: true})
  summary.value = {...next, channels: summary.value.channels}
  // Read the bounded current page after cache invalidation so sort order,
  // filtered totals and selection IDs remain consistent after a row changes.
  await loadChannels()
}

onMounted(() => {
  void Promise.allSettled([loadSummary('initial'),loadChannels()])
  mediaTimer = setInterval(() => {
    if (!document.hidden) void Promise.allSettled([loadSummary('silent'),loadChannels(true)])
  }, 10000)
})
onUnmounted(() => {summaryAbort?.abort(); pageAbort?.abort(); clearTimeout(searchTimer); clearInterval(mediaTimer)})

const statCards = computed(() => [
  { key: 'total', value: summary.value.stats.total, icon: Activity, tone: 'text-primary bg-primary/10 border-primary/20' },
  { key: 'available', value: summary.value.stats.available, icon: CheckCircle2, tone: 'text-emerald-600 bg-emerald-500/10 border-emerald-500/20' },
  { key: 'failed', value: summary.value.stats.failed, icon: AlertTriangle, tone: 'text-red-600 bg-red-500/10 border-red-500/20' },
  { key: 'balancePaused', value: summary.value.stats.balancePaused, icon: WalletCards, tone: 'text-amber-600 bg-amber-500/10 border-amber-500/20' },
  { key: 'monitorPaused', value: summary.value.stats.monitorPaused, icon: PauseCircle, tone: 'text-sky-600 bg-sky-500/10 border-sky-500/20' },
  { key: 'dispatchPaused', value: summary.value.stats.dispatchPaused, icon: PowerOff, tone: 'text-zinc-600 bg-zinc-500/10 border-zinc-500/20' },
])

const visibleGroups = computed(() => summary.value.groups.filter(group => groupStatusFilter.value === 'all' || (groupStatusFilter.value === 'available' ? group.available > 0 : group.available === 0)))
const groupOptions = computed(() => [{ id: 'all', name: t('admin.channelMonitor.groups.all') }, ...visibleGroups.value.map(group => ({ id: group.groupId, name: group.groupName }))])
const selectedGroupID = computed(() => selectedGroup.value === 'all' ? '' : selectedGroup.value)
const selectedGroupName = computed(() => summary.value.groups.find(group => group.groupId === selectedGroupID.value)?.groupName ?? '')
const selectedPriorityGroupRule = computed(() => summary.value.rateRule.priorityGroups.find(rule => rule.groupId === selectedGroupID.value) ?? null)
const selectedGroupSharesAccounts = computed(() => !!selectedGroupID.value && summary.value.channels.some(channel => channel.ownGroupIds.includes(selectedGroupID.value) && channel.ownGroupIds.length > 1))
watch(selectedGroup, () => { void loadSummary('silent') })

const filteredChannels = computed(() => summary.value.channels)
const pageCount = computed(() => Math.max(1, Math.ceil(channelTotal.value / pageSize.value)))
const pagedChannels = computed(() => summary.value.channels)
const visibleRuleIds = computed(() => pagedChannels.value.map(channel => channel.ruleId))
const selectedCount = computed(() => selectedRuleIds.value.length)
const allVisibleSelected = computed(() => visibleRuleIds.value.length > 0 && visibleRuleIds.value.every(id => selectedRuleIds.value.includes(id)))
const selectedChannels = computed(() => summary.value.channels.filter(channel => selectedRuleIds.value.includes(channel.ruleId)))
const allRuleIds = computed(() => filteredRuleIds.value)
const bulkEditorRuleIds = computed(() => bulkEditorScope.value === 'all' ? allRuleIds.value : selectedRuleIds.value)
const bulkEditorCount = computed(() => bulkEditorRuleIds.value.length)
const isActionLoading = computed(() => activeActionKeys.value.length > 0)
const isBulkActionLoading = computed(() => activeActionKeys.value.some(key => key.startsWith('bulk:') || key.startsWith('editor:') || key.startsWith('rate-rule:') || key.startsWith('test-model:')))
watch(searchQuery, value => {clearTimeout(searchTimer); searchTimer = setTimeout(() => {debouncedSearch.value = value}, 250)})
watch([debouncedSearch, statusFilter, dispatchFilter, groupStatusFilter, selectedGroup, pageSize], () => {if (page.value !== 1) page.value = 1; else void loadChannels()})
watch(page, () => {void loadChannels()})
watch(groupStatusFilter, () => {if (selectedGroup.value !== 'all' && !visibleGroups.value.some(group => group.groupId === selectedGroup.value)) selectedGroup.value = 'all'})
watch(pageCount, value => { if (page.value > value) page.value = value })

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

const groupRowClass = (groupId: string, platform: string): string => {
  const selected = selectedGroupID.value === groupId
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
    if (options.refresh !== false) {const match = actionKey.match(/^channel:(.+):[^:]+$/); if (match?.[1]) await refreshChannel(match[1]); else await Promise.allSettled([loadSummary('silent'),loadChannels()])}
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
    upstreamMultiplierOverride: channel.upstreamMultiplierOverride,
    allowWhenUpstreamRateGteOwn: channel.allowWhenUpstreamRateGteOwn,
    useDefaultTestModel: !channel.testModelId,
    testModelId: channel.testModelId || channel.effectiveTestModelId,
  }
}

const openBulkEditor = (scope: BulkEditorScope = 'selected') => {
  bulkEditorScope.value = scope
  const first = (scope === 'all' ? summary.value.channels : selectedChannels.value)[0]
  editingChannel.value = null
  isBulkEditorOpen.value = true
  editForm.value = {
    enabled: first?.enabled ?? true,
    checkIntervalMinutes: first?.checkIntervalMinutes ?? 2,
    failureThreshold: first?.failureThreshold ?? 2,
    balanceThreshold: first?.balanceThreshold ?? 1,
    upstreamMultiplierOverride: null,
    allowWhenUpstreamRateGteOwn: false,
    useDefaultTestModel: true,
    testModelId: '',
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
  void getMonitorMediaPresets().then(value => {mediaPresets.value = value}).catch(() => {})
  testModelForm.value = {
    openaiModelId: summary.value.testModelConfig.openaiModelId || 'gpt-5.6-sol',
    anthropicModelId: summary.value.testModelConfig.anthropicModelId || 'claude-sonnet-5',
    grokModelId: summary.value.testModelConfig.grokModelId || 'grok-4.5',
    groupModels: summary.value.testModelConfig.groupModels?.map(model => ({ ...model })) ?? [],
    balanceRefreshIntervalMinutes: summary.value.testModelConfig.balanceRefreshIntervalMinutes || 5,
  }
  isTestModelEditorOpen.value = true
}

const resetMedia = (channel: ChannelMonitorChannel) => {
  if (!channel.mediaTaskKey || !window.confirm(t('admin.channelMonitor.testModel.resetConfirm'))) return
  void runAction(() => resetMonitorMediaTask(channel.mediaTaskKey!), {actionKey: channelActionKey(channel, 'reset')})
}
const closeTestModelEditor = () => {
  isTestModelEditorOpen.value = false
}

const configurableGroups = computed(() => summary.value.groups
  .filter((group: ChannelMonitorGroup) => Boolean(group.groupId))
  .sort((a: ChannelMonitorGroup, b: ChannelMonitorGroup) => Number(b.groupId === selectedGroupID.value) - Number(a.groupId === selectedGroupID.value) || a.groupName.localeCompare(b.groupName)))

const groupModelConfig = (groupID: string) => testModelForm.value.groupModels.find(model => model.groupId === groupID)
const groupModelValue = (groupID: string): string => groupModelConfig(groupID)?.modelId ?? ''

const updateGroupModel = (group: ChannelMonitorGroup, value: string) => {
  const modelID = value.trim()
  const index = testModelForm.value.groupModels.findIndex(model => model.groupId === group.groupId)
  if (!modelID) {
    if (index >= 0) testModelForm.value.groupModels.splice(index, 1)
    return
  }
  const existing = index >= 0 ? testModelForm.value.groupModels[index] : undefined
  const config: ChannelMonitorGroupModelConfig = { ...existing, groupId: group.groupId, groupName: group.groupName, modelId: modelID }
  if (index >= 0) testModelForm.value.groupModels[index] = config
  else testModelForm.value.groupModels.push(config)
}

const handleGroupModelInput = (group: ChannelMonitorGroup, event: Event) => {
  const target = event.target as HTMLInputElement | null
  updateGroupModel(group, target?.value ?? '')
}

const updateGroupModelField = (group: ChannelMonitorGroup, field: 'mode' | 'prompt' | 'size' | 'seconds' | 'adapter' | 'presetModel', value: string) => {
  const current = groupModelConfig(group.groupId)
  if (!current?.modelId) return
  const next = { ...current, groupName: group.groupName } as ChannelMonitorGroupModelConfig
  if (field === 'seconds') next.seconds = Math.max(0, Number(value) || 0)
  else next[field] = value as never
  const index = testModelForm.value.groupModels.findIndex(model => model.groupId === group.groupId)
  if (index >= 0) testModelForm.value.groupModels[index] = next
}

const saveTestModelConfig = async () => {
  let saved = false
  await runAction(async () => {
    const currentGroupIDs = new Set(configurableGroups.value.map(group => group.groupId))
    const config = await updateChannelMonitorTestModelConfig({
      openaiModelId: testModelForm.value.openaiModelId,
      anthropicModelId: testModelForm.value.anthropicModelId,
      grokModelId: testModelForm.value.grokModelId,
      groupModels: testModelForm.value.groupModels
        .filter(model => currentGroupIDs.has(model.groupId) && model.modelId.trim())
        .map(model => ({ ...model, modelId: model.modelId.trim() })),
      balanceRefreshIntervalMinutes: Number(testModelForm.value.balanceRefreshIntervalMinutes),
    })
    summary.value.testModelConfig = config
    saved = true
  }, { actionKey: 'test-model:save', refresh: false })
  if (saved) {closeTestModelEditor(); await Promise.allSettled([loadSummary('silent'),loadChannels()])}
}

const saveRateRule = async (applyAfterSave = false) => {
  await runAction(async () => {
    if (applyAfterSave && !selectedGroupID.value) throw new Error('admin.channelMonitor.errors.selectPriorityGroup')
    await updateChannelMonitorRateRule({ ...rateRuleForm.value })
    if (applyAfterSave) await applySelectedGroupPriority()
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
    const customModel = editForm.value.testModelId.trim()
    if (!editForm.value.useDefaultTestModel && !customModel) {
      errorKey.value = 'admin.channelMonitor.editor.modelRequired'
      return
    }
    const rate = editForm.value.upstreamMultiplierOverride
    const resetRate = rate == null || String(rate).trim() === ''
    if (!resetRate && (!Number.isFinite(Number(rate)) || Number(rate) < 0)) {
      errorKey.value = 'admin.channelMonitor.errors.invalidMultiplier'
      return
    }
    payload.upstreamMultiplierOverride = resetRate ? undefined : Number(rate)
    payload.resetUpstreamMultiplierOverride = resetRate
    payload.allowWhenUpstreamRateGteOwn = editForm.value.allowWhenUpstreamRateGteOwn
    payload.testModelId = editForm.value.useDefaultTestModel ? '' : customModel
    await runAction(async () => { await updateChannelMonitorRule(editingChannel.value!.ruleId, payload); closeEditor() }, { actionKey: `editor:${editingChannel.value.ruleId}` })
  } else if (isBulkEditorOpen.value) {
    const ruleIds = bulkEditorRuleIds.value
    if (ruleIds.length === 0) return
    await runAction(() => bulkUpdateChannelMonitorRules({ ...payload, ruleIds }), {
      actionKey: `bulk:edit:${bulkEditorScope.value}`,
      clearSelection: bulkEditorScope.value === 'selected',
    })
    if (!errorKey.value) closeEditor()
  }
}

const setSelectedMonitoring = (enabled: boolean) =>
  runAction(() => bulkUpdateChannelMonitorRules({ ruleIds: selectedRuleIds.value, enabled }), { actionKey: `bulk:monitor:${enabled}`, clearSelection: true })

const setSelectedSchedulable = (schedulable: boolean) =>
  runAction(() => bulkSetChannelMonitorRulesSchedulable(selectedRuleIds.value, schedulable), { actionKey: `bulk:dispatch:${schedulable}`, clearSelection: true })

const rotationFilterReady = computed(() =>
  searchQuery.value === debouncedSearch.value && lastPageQuery.value === JSON.stringify(pageQuery()))
const rotatingKeys = computed(() => isActionActive('bulk:rotate-keys'))
const rotationMessage = ref('')
const whitelistMessage = ref('')
async function syncMonitorWhitelist(scope: 'filtered' | 'selected') {
  if (!rotationFilterReady.value || isBulkActionLoading.value) return
  const ruleIds = [...new Set(scope === 'selected' ? selectedRuleIds.value : filteredRuleIds.value)]
  if (!ruleIds.length) return
  whitelistMessage.value = '正在同步白名单…'
  await runAction(async () => {
    try {
      const job = await syncKeyWhitelist({ ruleIds })
      const skipped = job.items.filter(item => item.result?.skipped).length
      whitelistMessage.value = `已同步 ${job.total - job.failed - skipped} 个 Key，${job.failed} 个失败${skipped ? `，${skipped} 个跳过` : ''}`
      window.dispatchEvent(new Event('transithub:key-policy'))
      await loadChannels()
      const problems = keyJobMessage(job)
      if (problems) throw new Error(problems)
    } catch (error) {
      if (whitelistMessage.value === '正在同步白名单…') whitelistMessage.value = ''
      throw error
    }
  }, { actionKey: 'bulk:whitelist', refresh: false })
}
async function rotateMonitorKeys(scope: 'filtered' | 'selected') {
  if (!rotationFilterReady.value || rotatingKeys.value) return
  const ruleIds = [...new Set(scope === 'selected' ? selectedRuleIds.value : filteredRuleIds.value)]
  if (!ruleIds.length) return
  const filterAtStart = lastPageQuery.value
  await runAction(async () => {
    const targets = { ruleIds }
    rotationMessage.value = ''
    const preview = await previewKeyRotation(targets)
    if (!preview.keys) throw new Error('所选记录没有可轮换的绑定 Key')
    if (lastPageQuery.value !== filterAtStart || !rotationFilterReady.value) return
    const skipped = preview.skipped ? `\n将跳过 ${preview.skipped} 条缺少绑定凭据的记录。` : ''
    if (!window.confirm(`轮换 ${preview.keys} 个上游 Key？${skipped}\n共用同一 Key 的全部转发账号会一起切换，回读验证后撤销旧 Key。`)) return
    rotationMessage.value = '轮换任务进行中…'
    try {
      const job = await rotateKeys(targets, preview.version)
      const taskSkipped = job.items.filter(item => item.result?.skipped).length
      rotationMessage.value = `已轮换 ${job.total - job.failed - taskSkipped} 个 Key，${job.failed} 个失败${preview.skipped + taskSkipped ? `，${preview.skipped + taskSkipped} 条跳过` : ''}`
      await Promise.allSettled([loadSummary('silent'), loadChannels()])
      const problems = keyJobMessage(job)
      if (problems) throw new Error(problems)
    } catch (err) {
      if (rotationMessage.value === '轮换任务进行中…') rotationMessage.value = ''
      throw err
    }
  }, { actionKey: 'bulk:rotate-keys', refresh: false })
}

const syncableRuleIds = computed(() => summary.value.channels
  .filter(channel => channel.supported && channel.adminAccountId.trim() !== '')
  .map(channel => channel.ruleId))
const oneClickSyncRuleIds = () => {
  // The server validates every filtered result, including channels on pages
  // that are not currently rendered. Only constrain an explicit selection to
  // the rows whose support state is known on this page.
  if (selectedRuleIds.value.length === 0) return syncableRuleIds.value
  const syncable = new Set(syncableRuleIds.value)
  return selectedRuleIds.value.filter(ruleId => syncable.has(ruleId) || !summary.value.channels.some(channel => channel.ruleId === ruleId))
}

const runOneClickSync = () => {
  const ruleIds = oneClickSyncRuleIds()
  if (ruleIds.length === 0) return
  runAction(() => bulkSyncAndTakeOverChannelMonitorRules(ruleIds), { actionKey: 'bulk:one-click-sync' })
}

const previewRateRule = () =>
  runAction(async () => {
    summary.value.rateRule = await previewChannelMonitorRateRule()
  }, { actionKey: 'rate-rule:preview', refresh: false })

const applySelectedGroupPriority = async () => {
  if (!selectedGroupID.value) throw new Error('admin.channelMonitor.errors.selectPriorityGroup')
  const plan=await previewChannelMonitorRateRule()
  summary.value.rateRule=plan
  const result = await applyChannelMonitorRateRule(selectedGroupID.value,plan.planVersion)
  await loadSummary('silent')
  if (!result.success) throw new Error(result.message || 'admin.channelMonitor.errors.request')
}

const applyRateRule = () =>
  runAction(applySelectedGroupPriority, { actionKey: 'rate-rule:apply', refresh: false })

const toggleSelectedGroupPriority = (enabled: boolean) => {
  if (!selectedGroupID.value) return
  runAction(() => updateChannelMonitorPriorityGroupRule(selectedGroupID.value, { enabled }), { actionKey: 'rate-rule:group' })
}

const toggleSelectedGroupAutoPriority = (enabled: boolean) => {
  if (!selectedGroupID.value) return
  runAction(() => updateChannelMonitorPriorityGroupRule(selectedGroupID.value, { autoApplyOnRateChange: enabled }), { actionKey: 'rate-rule:group-auto' })
}

const toggleChannelMonitoring = (channel: ChannelMonitorChannel) =>
  runAction(() => updateChannelMonitorRule(channel.ruleId, { enabled: !channel.enabled }), { actionKey: channelActionKey(channel, 'monitor') })

const toggleChannelSchedulable = (channel: ChannelMonitorChannel) =>
  runAction(() => setChannelMonitorRuleSchedulable(channel.ruleId, channel.schedulable === false), { actionKey: channelActionKey(channel, 'dispatch') })

const setChannelPriority = (channel: ChannelMonitorChannel) => {
  const priority = Number(priorityDrafts.value[channel.ruleId])
  if (!Number.isFinite(priority)) return
  return runAction(() => setChannelMonitorRulePriority(channel.ruleId, Math.round(priority), selectedGroupID.value || undefined), { actionKey: channelActionKey(channel, 'priority') })
}

const syncChannelStatus = async (channel:ChannelMonitorChannel) => {
 const job=await followJob(await operationJSON<OperationJob>(`/upstream-sites/${encodeURIComponent(channel.siteId)}/model-health/refresh`,{method:'POST'}))
 if(job.failed)throw new Error(job.items.find(i=>i.error)?.error||'状态同步失败')
 healthSyncVersion.value++
}

const syncAndTakeOverChannel = (channel: ChannelMonitorChannel) =>
  runAction(async () => {
    if (channel.repairAvailable) await repairRealConnectionAccount(channel.connectionId)
    await syncAndTakeOverChannelMonitorRule(channel.ruleId)
  }, { actionKey: channelActionKey(channel, 'takeover') })

const openDisconnect = (channel: ChannelMonitorChannel) => {
  disconnectingChannel.value = channel
  disconnectMode.value = 'full'
  disconnectError.value = ''
}

const groupEditorSearch = ref('')
const visibleEditorGroups = computed(()=>groupEditorOwnGroups.value.filter(g=>!groupEditorSearch.value||g.groupName.toLowerCase().includes(groupEditorSearch.value.toLowerCase())))
const groupEditorTestModel = ref('')
const groupEditorAllowHigherRate = ref(false)
const groupEditorActionKey = (channel: ChannelMonitorChannel): string => channelActionKey(channel, 'groups')

const openGroupEditor = async (channel: ChannelMonitorChannel) => {
  if (isChannelBusy(channel)) return
  groupEditingChannel.value = channel
  groupEditorSearch.value = ''
  groupEditorOwnGroups.value = []
  groupEditorOwnGroupIds.value = []
  groupEditorStaleCount.value = 0
  groupEditorError.value = ''
  groupEditorLoading.value = true
  try {
    const [options, connections] = await Promise.all([getMySiteMappingOptions(), listRealConnections()])
    const connection = connections.find(item => item.id === channel.connectionId)
    if (!connection) throw new Error('admin.channelMonitor.groupsEditor.loadFailed')
    groupEditorTestModel.value = connection.testModelId || ''
    groupEditorAllowHigherRate.value = connection.allowWhenUpstreamRateGteOwn ?? false
    groupEditorOwnGroups.value = options.ownGroups
    const availableGroupIDs = new Set(options.ownGroups.map(group => group.id))
    const validGroupIDs = connection.ownGroupIds.filter(groupID => availableGroupIDs.has(groupID))
    groupEditorStaleCount.value = connection.ownGroupIds.length - validGroupIDs.length
    groupEditorOwnGroupIds.value = validGroupIDs
  } catch (error: any) {
    groupEditorError.value = error?.message?.startsWith('admin.') ? t(error.message) : error?.message || t('admin.channelMonitor.groupsEditor.loadFailed')
  } finally {
    groupEditorLoading.value = false
  }
}

const closeGroupEditor = () => {
  if (groupEditorLoading.value) return
  groupEditingChannel.value = null
  groupEditorOwnGroups.value = []
  groupEditorOwnGroupIds.value = []
  groupEditorStaleCount.value = 0
  groupEditorError.value = ''
}

const saveGroupEditor = async () => {
  const channel = groupEditingChannel.value
  if (!channel || groupEditorLoading.value || groupEditorOwnGroupIds.value.length === 0) return
  const actionKey = groupEditorActionKey(channel)
  if (isActionActive(actionKey)) return
  groupEditorLoading.value = true
  groupEditorError.value = ''
  activeActionKeys.value = [...activeActionKeys.value, actionKey]
  try {
    await updateRealConnectionGroups(channel.connectionId, { connectionId: channel.connectionId, testModelId: groupEditorTestModel.value, allowWhenUpstreamRateGteOwn: groupEditorAllowHigherRate.value, ownGroupIds: groupEditorOwnGroupIds.value })
    groupEditorLoading.value = false
    closeGroupEditor()
    await checkedOperation({kind:'refresh'})
    await Promise.allSettled([loadSummary('silent'),loadChannels()])
  } catch (error: any) {
    groupEditorError.value = error?.message?.startsWith('admin.') ? t(error.message) : error?.message || t('admin.channelMonitor.groupsEditor.saveFailed')
  } finally {
    activeActionKeys.value = activeActionKeys.value.filter(key => key !== actionKey)
    groupEditorLoading.value = false
  }
}

const closeDisconnect = () => {
  disconnectingChannel.value = null
  disconnectMode.value = 'full'
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

const selectGroup = (groupID: string) => {
  selectedGroup.value = selectedGroup.value === groupID ? 'all' : groupID
}

const timelineNextLabel = (channel: ChannelMonitorChannel): string => {
  if (!channel.enabled) return t('admin.channelMonitor.timeline.monitorOff')
  if (!channel.checkSupported) return t('admin.channelMonitor.status.unsupported')
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
function channelPreview(channel:ChannelMonitorChannel){
 if(channel.modelSummary)return channel.modelSummary
 return {id:channel.effectiveTestModelId,model:channel.effectiveTestModelId,status:channel.status==='auto_paused'?'failed':channel.status,source:'upstream',checkedAt:channel.lastCheckedAt,message:channel.lastMessage,timeline:[...(channel.recentResults||[])].filter(v=>v.model===channel.effectiveTestModelId).reverse().map(v=>({status:v.status==='auto_paused'?'failed':v.status,checkedAt:v.createdAt,latencyMs:v.latencyMs}))}
}
const channelRowClass=(channel:ChannelMonitorChannel)=>{
 if(!channel.enabled)return 'bg-slate-100/80 hover:bg-slate-200/70 dark:bg-slate-800/50 dark:hover:bg-slate-700/60'
 const state=channel.dispatchState||(channel.schedulable===true?'enabled':channel.manualPaused?'manual':channel.schedulable===false?'disabled':'unknown')
 const tones:Record<string,string>={enabled:'bg-emerald-500/[0.06] hover:bg-emerald-500/[0.11]',auto:'bg-red-500/[0.08] hover:bg-red-500/[0.13]',balance:'bg-amber-500/[0.09] hover:bg-amber-500/[0.14]',manual:'bg-orange-500/[0.08] hover:bg-orange-500/[0.13]',rate:'bg-violet-500/[0.07] hover:bg-violet-500/[0.12]',disabled:'bg-rose-500/[0.06] hover:bg-rose-500/[0.11]',unknown:'bg-slate-500/[0.04] hover:bg-slate-500/[0.08]'}
 return tones[state]||tones.unknown
}
const dispatchBadge = (channel:ChannelMonitorChannel) => {
 const state=channel.dispatchState || (channel.schedulable==null?'unknown':channel.schedulable?'enabled':channel.manualPaused?'manual':'disabled')
 const badges:Record<string,{label:string;tone:string}>={
 enabled:{label:'可调度',tone:'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'},
 manual:{label:'人工停用',tone:'border-orange-500/25 bg-orange-500/10 text-orange-700 dark:text-orange-300'},
 balance:{label:'余额停用',tone:'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300'},
 rate:{label:'倍率限制',tone:'border-violet-500/25 bg-violet-500/10 text-violet-700 dark:text-violet-300'},
 auto:{label:'检测停用',tone:'border-red-500/25 bg-red-500/10 text-red-700 dark:text-red-300'},
 disabled:{label:'已停用',tone:'border-rose-500/25 bg-rose-500/10 text-rose-700 dark:text-rose-300'},
 unknown:{label:'未知',tone:'border-slate-400/25 bg-slate-500/10 text-slate-500 dark:text-slate-300'}}
 return badges[state]||badges.unknown!
}
</script>

<template>
  <div class="min-h-[calc(100vh-8rem)] flex flex-col gap-4">
    <ModelTestDialog v-if="testingChannel" :site-id="testingChannel.siteId" :group-id="testingChannel.upstreamGroupId" :connection-id="testingChannel.connectionId" :initial-model="testingChannel.effectiveTestModelId" :label="testingChannel.adminAccountName" @close="testingChannel = null" />
    <p v-if="healthSyncMessage" class="text-xs text-muted-foreground">{{healthSyncMessage}}</p>

    <div class="grid grid-cols-3 gap-3 md:grid-cols-6">
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

    <div class="flex flex-col gap-3 2xl:flex-row 2xl:items-center 2xl:justify-between">
      <div class="flex flex-1 flex-col gap-3 md:flex-row">
        <div class="relative w-full md:max-w-sm">
          <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            v-model="searchQuery"
            class="h-10 w-full rounded-xl border border-border/50 bg-surface pl-10 pr-4 text-sm outline-none transition focus:border-primary focus:ring-1 focus:ring-primary"
            :placeholder="t('admin.channelMonitor.filters.search')"
          />
        </div>
        <select v-model="selectedGroup" class="h-10 min-w-0 rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none transition focus:border-primary focus:ring-1 focus:ring-primary">
          <option v-for="group in groupOptions" :key="group.id" :value="group.id">
            {{ group.name }}
          </option>
        </select>
        <select v-model="groupStatusFilter" class="h-10 min-w-0 rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none transition focus:border-primary focus:ring-1 focus:ring-primary">
          <option value="all">{{ t('admin.channelMonitor.groups.statusAll') }}</option>
          <option value="available">{{ t('admin.channelMonitor.groups.statusAvailable') }}</option>
          <option value="unavailable">{{ t('admin.channelMonitor.groups.statusUnavailable') }}</option>
        </select>
        <select v-model="statusFilter" class="h-10 min-w-0 rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none transition focus:border-primary focus:ring-1 focus:ring-primary">
          <option value="all">{{ t('admin.channelMonitor.filters.allStatus') }}</option>
          <option value="healthy">{{ statusLabel('healthy') }}</option>
          <option value="failed">{{ statusLabel('failed') }}</option>
          <option value="unknown">{{ statusLabel('unknown') }}</option>
          <option value="generating">{{ statusLabel('generating') }}</option>
          <option value="config_error">{{ statusLabel('config_error') }}</option>
          <option value="auto_paused">{{ statusLabel('auto_paused') }}</option>
          <option value="balance_paused">{{ statusLabel('balance_paused') }}</option>
          <option value="monitor_paused">{{ statusLabel('monitor_paused') }}</option>
          <option value="dispatch_paused">{{ statusLabel('dispatch_paused') }}</option>
          <option value="unsupported">{{ statusLabel('unsupported') }}</option>
        </select>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <MonitorBindingSync :groups="summary.groups" @done="loadSummary('silent')" />
        <Button type="button" variant="secondary" class="h-10 gap-2 rounded-xl" :disabled="isLoading || isBulkActionLoading || allRuleIds.length === 0" @click="openBulkEditor('all')">
          <Settings2 class="h-4 w-4" />
          {{ t('admin.channelMonitor.bulk.editAllRules') }}
        </Button>
        <Button type="button" variant="secondary" class="h-10 gap-2 rounded-xl" :disabled="syncingHealth" @click="syncCurrentHealth"><RefreshCw :class="['h-4 w-4',syncingHealth?'animate-spin':'']" />{{syncingHealth?'同步状态中…':'同步上游状态（当前页／选中项）'}}</Button>
        <Button type="button" variant="secondary" class="h-10 gap-2 rounded-xl" :disabled="isLoading || isRefreshing" @click="runAction(() => submitOperation({kind:'refresh'}))">
          <RefreshCw :class="['h-4 w-4', (isLoading || isRefreshing) ? 'animate-spin' : '']" />
          {{ t('admin.channelMonitor.actions.refresh') }}
        </Button>
      </div>
    </div>

    <div class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border/40 bg-surface px-3 py-2">
      <div class="flex flex-wrap items-center gap-3"><StatusSourceControl /><select v-model="dispatchFilter" aria-label="调度状态筛选" class="h-8 rounded-md border border-border/50 bg-surface px-2 text-xs"><option value="all">全部调度状态</option><option value="enabled">可调度</option><option value="paused">全部已停用</option><option value="manual">人工停用</option><option value="balance">余额停用</option><option value="rate">倍率限制</option><option value="auto">检测停用</option><option value="disabled">其他停用</option><option value="unknown">调度未知</option><option value="monitor">监控暂停</option></select></div>
      <div class="flex flex-wrap items-center gap-2">
        <Button type="button" variant="secondary" size="sm" :disabled="!rotationFilterReady || !filteredRuleIds.length || isBulkActionLoading" @click="syncMonitorWhitelist('filtered')">同步当前筛选白名单（{{ filteredRuleIds.length }} 条）</Button>
        <Button type="button" variant="secondary" size="sm" :disabled="!rotationFilterReady || !filteredRuleIds.length || isBulkActionLoading" @click="rotateMonitorKeys('filtered')">轮换当前筛选 Key（{{ filteredRuleIds.length }} 条）</Button>
      </div>
      <KeyPolicyEditor :ids="rotationFilterReady ? selectedRuleIds.length ? selectedRuleIds : filteredRuleIds : []" />
    </div>

    <div v-if="selectedCount > 0" class="flex flex-wrap items-center gap-2 rounded-lg border border-border/50 bg-surface px-4 py-3">
      <span class="mr-2 text-sm font-medium text-foreground">{{ t('admin.channelMonitor.bulk.selected', { count: selectedCount }) }}</span>
      <Button type="button" variant="secondary" size="sm" :disabled="!rotationFilterReady || isBulkActionLoading" @click="rotateMonitorKeys('selected')">轮换已选 Key（{{ selectedCount }} 条）</Button>
      <Button type="button" variant="secondary" size="sm" :disabled="!rotationFilterReady || isBulkActionLoading" @click="syncMonitorWhitelist('selected')">同步已选白名单（{{ selectedCount }} 条）</Button>
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

    <OperationProgress :kinds="['key_security', 'manual_upstream_model']" @completed="loadChannels()" />
    <p v-if="rotationMessage" role="status" class="text-sm text-muted-foreground">{{ rotationMessage }}</p>
    <p v-if="whitelistMessage" role="status" class="text-sm text-muted-foreground">{{ whitelistMessage }}</p>
    <div v-if="errorKey" class="whitespace-pre-line rounded-xl border border-warning/20 bg-warning/10 p-3 text-sm text-warning">
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

          <div v-if="selectedGroupID" class="mt-3 flex flex-wrap items-center gap-3 rounded-lg border border-border/50 bg-surface-elevated px-3 py-2">
            <span class="text-xs font-medium text-foreground">{{ selectedGroupName }}</span><Button size="sm" variant="secondary" @click="openTestModelEditor">{{ t('admin.channelMonitor.testModel.configureShort') }}</Button>
            <label class="flex items-center gap-2 text-xs text-muted-foreground">
              <input :checked="selectedPriorityGroupRule?.enabled ?? false" type="checkbox" class="h-4 w-4 rounded border-border text-primary" @change="toggleSelectedGroupPriority(($event.target as HTMLInputElement).checked)" />
              {{ t('admin.channelMonitor.rateRule.groupEnabled') }}
            </label>
            <label class="flex items-center gap-2 text-xs text-muted-foreground">
              <input :checked="selectedPriorityGroupRule?.autoApplyOnRateChange ?? false" type="checkbox" class="h-4 w-4 rounded border-border text-primary" @change="toggleSelectedGroupAutoPriority(($event.target as HTMLInputElement).checked)" />
              {{ t('admin.channelMonitor.rateRule.groupAuto') }}
            </label>
          </div>
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
          <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-violet-500/30 !bg-violet-500/10 !text-violet-700 hover:!bg-violet-500/15 dark:!text-violet-300" :disabled="isBulkActionLoading || syncableRuleIds.length === 0" @click="runOneClickSync">
            <Power :class="['h-3.5 w-3.5', isActionActive('bulk:one-click-sync') ? 'animate-pulse' : '']" />
            {{ t('admin.channelMonitor.bulk.oneClickSync') }}
          </Button>
          <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-violet-500/30 !bg-violet-500/10 !text-violet-700 hover:!bg-violet-500/15 dark:!text-violet-300" :disabled="isBulkActionLoading" @click="openTestModelEditor">
            <Settings2 class="h-3.5 w-3.5" />
            {{ t('admin.channelMonitor.testModel.configureShort') }}
          </Button>
          <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-blue-500/30 !bg-blue-500/10 !text-blue-700 hover:!bg-blue-500/15 dark:!text-blue-300" :disabled="isBulkActionLoading" @click="previewRateRule">
            <RefreshCw :class="['h-3.5 w-3.5', isActionActive('rate-rule:preview') ? 'animate-spin' : '']" />
            {{ t('admin.channelMonitor.rateRule.preview') }}
          </Button>
          <Button type="button" variant="secondary" size="sm" class="gap-1.5 !border-emerald-500/30 !bg-emerald-500/10 !text-emerald-700 hover:!bg-emerald-500/15 dark:!text-emerald-300" :disabled="isBulkActionLoading || !selectedGroupID" @click="applyRateRule">
            <Play class="h-3.5 w-3.5" />
            {{ t('admin.channelMonitor.rateRule.applyPriority') }}
          </Button>
          <Button type="button" variant="secondary" size="sm" class="gap-1.5" :disabled="isBulkActionLoading" @click="openRateRuleEditor">
            <Settings2 class="h-3.5 w-3.5" />
            {{ t('admin.channelMonitor.rateRule.configure') }}
          </Button>
        </div>
      </div>
      <p v-if="selectedGroupSharesAccounts" class="mt-3 text-xs text-amber-600 dark:text-amber-300">{{ t('admin.channelMonitor.rateRule.sharedPriority') }}</p>
      <div v-if="summary.rateRule.lastResult" class="mt-3 text-xs text-muted-foreground">
        {{ t('admin.channelMonitor.rateRule.priorityResult', { time: formatDateTime(summary.rateRule.lastResult.createdAt), updated: summary.rateRule.lastResult.priorityUpdated, skipped: summary.rateRule.lastResult.skippedCount, unchanged: summary.rateRule.lastResult.total - summary.rateRule.lastResult.priorityUpdated - summary.rateRule.lastResult.skippedCount }) }}
        <span v-if="!summary.rateRule.lastResult.success" class="ml-2 text-red-600">{{ t(summary.rateRule.lastResult.message) }}</span>
      </div>
    </section>

    <div class="grid min-h-0 flex-1 grid-cols-1 gap-3 2xl:grid-cols-[220px_minmax(0,1fr)]">
      <section class="hidden min-h-0 overflow-hidden rounded-lg border border-border/50 bg-surface 2xl:block">
        <div class="border-b border-border/50 px-4 py-3">
          <h2 class="text-sm font-semibold text-foreground">{{ t('admin.channelMonitor.groups.title') }}</h2>

        </div>
        <div class="max-h-[65vh] overflow-auto">
          <table class="w-full text-left text-xs">
            <thead class="sticky top-0 bg-surface-elevated text-xs text-muted-foreground">
              <tr>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.groups.columns.group') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.groups.columns.available') }}</th>
                <th class="px-2 py-2 font-medium">停检/停调</th>

              </tr>
            </thead>
            <tbody class="divide-y divide-border/40">
              <tr
                v-for="group in visibleGroups"
                :key="`${group.groupId}-${group.platform}`"
                :class="[
                  'cursor-pointer transition-colors',
                  groupRowClass(group.groupId, group.platform)
                ]"
                @click="selectGroup(group.groupId)"
              >
                <td class="px-2 py-2">
                  <div class="font-medium text-foreground">{{ group.groupName }}</div>
                  <span :class="['mt-1 inline-flex rounded-md border px-1.5 py-0.5 text-[11px] font-medium', groupPlatformBadgeClass(group.platform)]">
                    {{ group.platform || t('admin.channelMonitor.common.unknown') }}
                  </span>
                </td>
                <td class="px-2 py-2 font-mono text-foreground">{{ group.available }}/{{ group.total }}</td>
                <td class="px-2 py-2 text-xs text-muted-foreground">{{ group.monitorPaused }}/{{ group.dispatchPaused }}</td>

              </tr>
              <tr v-if="!isLoading && visibleGroups.length === 0">
                <td colspan="3" class="px-4 py-10 text-center text-sm text-muted-foreground">{{ t('admin.channelMonitor.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="min-h-0 overflow-hidden rounded-lg border border-border/50 bg-surface">
        <div class="flex items-center justify-between gap-3 border-b border-border/50 px-4 py-3">
          <div>
            <h2 class="text-sm font-semibold text-foreground">{{ t('admin.channelMonitor.channels.title') }}</h2>
            
          </div>
          <button type="button" class="inline-flex items-center gap-2 rounded-lg px-2 py-1 text-xs text-muted-foreground hover:bg-surface-elevated hover:text-foreground" @click="toggleSelectAllVisible">
            <CheckSquare2 v-if="allVisibleSelected" class="h-4 w-4 text-primary" />
            <Square v-else class="h-4 w-4" />
            {{ t('admin.channelMonitor.actions.selectAll') }}
          </button>
        </div>

        <div v-if="channelsLoading && !summary.channels.length" class="flex h-64 items-center justify-center text-muted-foreground">
          <Loader2 class="mr-2 h-5 w-5 animate-spin" />
          {{ t('admin.channelMonitor.loading') }}
        </div>

        <div v-else class="max-h-[65vh] overflow-auto">
          <table class="w-full min-w-[1200px] table-fixed text-left text-xs">
            <colgroup><col style="width:3%"/><col style="width:16%"/><col style="width:15%"/><col style="width:5%"/><col style="width:5%"/><col style="width:12%"/><col style="width:5%"/><col style="width:8%"/><col style="width:6%"/><col style="width:5%"/><col style="width:20%"/></colgroup>
            <thead class="sticky top-0 bg-surface-elevated text-xs text-muted-foreground">
              <tr>
                <th class="w-9 px-3 py-2 font-medium"></th>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.channels.columns.channel') }}</th>
                <th class="px-2 py-2 font-medium">模型状态</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.rateRule.upstreamRate') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.rateRule.ownRate') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.rateRule.priority') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.rateRule.accountRate') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.channels.columns.group') }}</th>
                <th class="px-2 py-2 font-medium">调度状态</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.channelMonitor.channels.columns.balance') }}</th>
                <th class="px-2 py-2 text-right font-medium">{{ t('admin.channelMonitor.channels.columns.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border/40">
              <tr
                v-for="channel in pagedChannels"
                :key="channel.connectionId"
:class="channelRowClass(channel)"
              >
                <td class="px-2 py-2 align-top">
                  <button type="button" class="mt-1 text-muted-foreground hover:text-primary" @click="toggleSelect(channel.ruleId)">
                    <CheckSquare2 v-if="selectedRuleIds.includes(channel.ruleId)" class="h-4 w-4 text-primary" />
                    <Square v-else class="h-4 w-4" />
                  </button>
                </td>
                <td class="px-2 py-2 align-top">
                  <div class="min-w-0 space-y-1">
                    <div class="truncate font-medium" :class="channel.enabled?'text-foreground':'text-muted-foreground'" :title="channel.adminAccountName">{{channel.adminAccountName||channel.adminAccountId}}</div>
                    <div class="truncate text-xs text-muted-foreground" :title="channel.upstreamGroupName">{{channel.siteName}} · {{channel.upstreamGroupName}}</div>
                    <GroupKeyPolicy :whitelist-enabled="channel.keyWhitelistEnabled" :initial-policy="channel.keyPolicy" :site-id="channel.siteId" :group-id="channel.upstreamGroupId" :group-name="channel.upstreamGroupName" />
                    <span v-if="channel.schedulableConflict||channel.priorityConflict" class="text-xs text-amber-600" :title="t('admin.channelMonitor.flags.manualOverrideHelp')">人工设置优先</span>
                  </div>
                </td>
                <td class="px-2 py-2 align-top">
                    <GroupModelStatus :preview="channelPreview(channel)" :key="channel.ruleId+':'+healthSyncVersion" :rule-id="channel.ruleId" :site-id="channel.siteId" :group-id="channel.upstreamGroupId" :group-name="channel.upstreamGroupName" :multiplier="channel.upstreamEffectiveMultiplier" :model-id="channel.effectiveTestModelId" :monitor-group-id="channel.detectionGroupId" compact />
                </td>
                <td class="px-2 py-2 align-top">
                  <div class="font-mono text-xs font-semibold text-foreground">{{ formatMultiplier(channel.upstreamEffectiveMultiplier) }}</div>
                  <div v-if="channel.upstreamMultiplierOverride != null" class="mt-1 text-xs text-primary">{{ t('admin.channelMonitor.editor.customRate') }}</div>
                  <Button variant="secondary" size="sm" class="mt-1 !h-7 !rounded-md !px-1 !text-xs whitespace-nowrap" :disabled="isChannelBusy(channel)" @click="openEditor(channel)">{{ t('admin.channelMonitor.rateRule.setPriority') }}</Button>
                </td>
                <td class="px-2 py-2 align-top">
                  <div class="font-mono text-xs font-semibold text-foreground">{{ formatMultiplier(channel.ownGroupMultiplier) }}</div>
                </td>
                <td class="px-2 py-2 align-top">
                  <div class="font-mono text-xs font-semibold text-foreground">{{ channel.accountPriority ?? '-' }} → {{ channel.recommendedPriority ?? '-' }}</div>
                  <div class="mt-1 flex items-center gap-1.5">
                    <input
                      v-model.number="priorityDrafts[channel.ruleId]"
                      type="number"
                      min="0"
                      max="999"
                      class="h-8 w-[76px] min-w-[76px] shrink-0 rounded-md border border-border/50 bg-surface px-2 text-sm font-mono text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                      :disabled="isChannelBusy(channel) || !channel.supported"
                      :aria-label="t('admin.channelMonitor.rateRule.manualPriority')"
                    />
                    <Button
                      type="button"
                      variant="secondary"
                      size="sm"
                      class="!h-7 !rounded-md !px-1 !text-xs whitespace-nowrap !border-amber-500/30 !bg-amber-500/10 !text-amber-700 hover:!bg-amber-500/15 dark:!text-amber-300"
                      :disabled="isChannelBusy(channel) || !channel.supported"
                      @click="setChannelPriority(channel)"
                    >
                      <Loader2 v-if="isActionActive(channelActionKey(channel, 'priority'))" class="h-3 w-3 animate-spin" />
                      <span v-else>{{ t('admin.channelMonitor.rateRule.setPriority') }}</span>
                    </Button>
                  </div>
                </td>
                <td class="px-2 py-2 align-top">
                  <div class="font-mono text-xs font-semibold text-foreground">{{ formatMultiplier(channel.accountRateMultiplier) }}</div>
                </td>
                <td class="px-2 py-2 align-top">
                  <div class="flex max-w-[140px] flex-wrap gap-1">
                    <button v-for="group in channel.ownGroups" :key="group" type="button" :disabled="isChannelBusy(channel)" :aria-label="'修改绑定分组：'+group" class="rounded-md border border-border/50 bg-surface-elevated px-2 py-0.5 text-left text-xs font-medium text-muted-foreground hover:border-primary/40 hover:text-primary disabled:opacity-50" @click="openGroupEditor(channel)">{{group}}</button>
                  </div>

                </td>
                <td class="px-2 py-2 align-top">
                  <span :class="['inline-flex rounded-md border px-1.5 py-1 text-[11px] font-medium',dispatchBadge(channel).tone]" :title="channel.rateGateMessage || channel.lastMessage">{{dispatchBadge(channel).label}}</span>
                  <span v-if="!channel.enabled" class="mt-1 inline-flex rounded bg-slate-500/10 px-1.5 py-0.5 text-[10px] text-slate-500">未监控</span>
                </td>
                <td class="px-2 py-2 align-top">
                  <div class="font-mono text-foreground">{{ formatMoney(channel.balance) }}</div>

                </td>

                <td class="px-2 py-2 align-top">
                  <div class="flex flex-wrap justify-end gap-1">
                    <ChannelKeyActions :whitelist-enabled="channel.keyWhitelistEnabled" :connection-id="channel.connectionId" :disabled="isChannelBusy(channel)" />
                    <Button type="button" variant="secondary" size="sm" class="!h-7 !rounded-md !px-1.5 !text-xs whitespace-nowrap" @click="testingChannel = channel">实际测试</Button>
                    <Button variant="secondary" size="sm" class="!h-7 !rounded-md !px-2 !text-xs !font-medium whitespace-nowrap !bg-blue-500/10 !text-blue-600" :disabled="isChannelBusy(channel)" @click="runAction(()=>syncChannelStatus(channel),{actionKey:channelActionKey(channel,'sync-status')})">同步状态</Button>
                    <Button variant="secondary" size="sm" class="!h-7 !rounded-md !px-2 !text-xs !font-medium whitespace-nowrap !bg-indigo-500/10 !text-indigo-600" :disabled="isChannelBusy(channel)" @click="openGroupEditor(channel)">修改分组</Button>
                    <Button variant="secondary" size="sm" class="!h-7 !rounded-md !px-2 !text-xs !font-medium whitespace-nowrap !bg-amber-500/10 !text-amber-700" :disabled="isChannelBusy(channel)||(!channel.supported&&!channel.repairAvailable)" @click="syncAndTakeOverChannel(channel)">同步接管</Button>
                    <Button variant="secondary" size="sm" class="!h-7 !rounded-md !px-2 !text-xs !font-medium whitespace-nowrap" :class="channel.enabled?'!bg-emerald-500/10 !text-emerald-700 dark:!text-emerald-300':'!bg-slate-500/10 !text-slate-500'" :title="channel.enabled?'点击暂停监控':'尚未开启监控，点击开启；调度状态单独控制'" :aria-label="channel.enabled?'暂停监控':'开启监控'" :disabled="isChannelBusy(channel)" @click="toggleChannelMonitoring(channel)">{{channel.enabled?'监控中':'未监控'}}</Button>
                    <Button variant="secondary" size="sm" class="!h-7 !rounded-md !px-2 !text-xs !font-medium whitespace-nowrap !bg-emerald-500/10 !text-emerald-700" :disabled="isChannelBusy(channel)||!channel.supported" @click="toggleChannelSchedulable(channel)">{{channel.schedulable===false?'开启调度':'停止调度'}}</Button>
                    <Button variant="secondary" size="sm" class="!h-7 !rounded-md !px-2 !text-xs !font-medium whitespace-nowrap !bg-violet-500/10 !text-violet-600" :disabled="isChannelBusy(channel)" @click="openEditor(channel)">规则设置</Button>
                    <Button v-if="channel.mediaTaskKey" variant="secondary" size="sm" class="!h-7 !rounded-md !px-2 !text-xs !font-medium whitespace-nowrap !bg-orange-500/10 !text-orange-700" :disabled="isChannelBusy(channel)" @click="resetMedia(channel)">重置媒体任务</Button>
                    <Button variant="secondary" size="sm" class="!h-7 !rounded-md !px-2 !text-xs !font-medium whitespace-nowrap !bg-red-500/10 !text-red-600" :disabled="isChannelBusy(channel)" @click="openDisconnect(channel)">取消绑定</Button>
                  </div>
                </td>
              </tr>
              <tr v-if="filteredChannels.length === 0">
                <td colspan="11" class="px-5 py-16 text-center text-sm text-muted-foreground">{{ t('admin.channelMonitor.empty') }}</td>
              </tr>
            </tbody>
          </table>
          <div v-if="channelTotal > 0" class="sticky bottom-0 left-0 flex flex-wrap items-center justify-between gap-2 border-t border-border/40 bg-surface px-3 py-2 text-xs text-muted-foreground">
            <span>{{ (page - 1) * pageSize + 1 }}-{{ Math.min(page * pageSize, channelTotal) }} / {{ channelTotal }}</span>
            <div class="flex items-center gap-2">
              <select v-model.number="pageSize" class="h-8 rounded-md border border-border/50 bg-surface px-2">
                <option :value="25">25</option><option :value="50">50</option><option :value="100">100</option>
              </select>
              <Button type="button" variant="ghost" size="sm" :disabled="page <= 1" @click="page--">{{ t('admin.channelMonitor.pagination.previous') }}</Button>
              <span>{{ page }} / {{ pageCount }}</span>
              <Button type="button" variant="ghost" size="sm" :disabled="page >= pageCount" @click="page++">{{ t('admin.channelMonitor.pagination.next') }}</Button>
            </div>
          </div>
        </div>
      </section>
    </div>

    <div v-if="editingChannel || isBulkEditorOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div class="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-xl border border-border/50 bg-card p-6 shadow-xl">
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
          <label v-if="editingChannel" class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.editor.upstreamRate') }}</span>
            <input v-model.number="editForm.upstreamMultiplierOverride" type="number" min="0" step="any" :placeholder="t('admin.channelMonitor.editor.upstreamRatePlaceholder')" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 font-mono text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
            
          </label>
          <label v-if="editingChannel" class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.editor.allowHigherRate') }}</span>
              
            </span>
            <input v-model="editForm.allowWhenUpstreamRateGteOwn" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
          <div v-if="editingChannel" class="space-y-2">
            <div class="grid grid-cols-2 gap-2 rounded-xl border border-border/50 bg-surface p-1">
              <label :class="['flex cursor-pointer items-center justify-center gap-2 rounded-lg px-3 py-2 text-xs transition-colors', editForm.useDefaultTestModel ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-surface-elevated']">
                <input v-model="editForm.useDefaultTestModel" type="radio" :value="true" class="sr-only" />
                {{ t('admin.channelMonitor.editor.inheritTestModel') }}
              </label>
              <label :class="['flex cursor-pointer items-center justify-center gap-2 rounded-lg px-3 py-2 text-xs transition-colors', !editForm.useDefaultTestModel ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-surface-elevated']">
                <input v-model="editForm.useDefaultTestModel" type="radio" :value="false" class="sr-only" />
                {{ t('admin.channelMonitor.editor.customTestModel') }}
              </label>
            </div>
            <PrimaryModelPicker v-model="editForm.testModelId" :site-id="editingChannel.siteId" :group-id="editingChannel.upstreamGroupId" :connection-id="editingChannel.connectionId" :disabled="editForm.useDefaultTestModel" />
            
          </div>
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
      <div class="max-h-[90vh] overflow-y-auto w-full max-w-3xl rounded-xl border border-border/50 bg-card p-6 shadow-xl">
        <h2 class="text-lg font-semibold text-foreground">{{ t('admin.channelMonitor.testModel.title') }}</h2>
        
        <div class="mt-5 grid gap-4">
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.testModel.openai') }}</span>
            <input v-model.trim="testModelForm.openaiModelId" type="text" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 font-mono text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" placeholder="gpt-5.6-sol" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.testModel.anthropic') }}</span>
            <input v-model.trim="testModelForm.anthropicModelId" type="text" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 font-mono text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" placeholder="claude-sonnet-5" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.testModel.grok') }}</span>
            <input v-model.trim="testModelForm.grokModelId" type="text" class="h-10 w-full rounded-xl border border-border/50 bg-surface px-3 font-mono text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" placeholder="grok-4.5" />
          </label>
          <div class="space-y-2">
            <div>
              <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.testModel.groupDefaults') }}</span>
              
            </div>
            <div v-if="configurableGroups.length === 0" class="rounded-lg border border-border/50 bg-surface px-3 py-3 text-sm text-muted-foreground">
              {{ t('admin.channelMonitor.testModel.noGroups') }}
            </div>
            <div v-else class="max-h-52 space-y-2 overflow-y-auto rounded-lg border border-border/50 bg-surface p-3">
              <div v-for="group in configurableGroups" :key="group.groupId" class="grid gap-2 rounded-lg border border-border/40 p-2 sm:grid-cols-[minmax(0,1fr)_10rem_12rem] sm:items-center">
                <span class="min-w-0 truncate text-sm text-foreground" :title="group.groupName">{{ group.groupName }} · #{{ group.groupId }} · {{ group.platform }}</span>
                <input :value="groupModelValue(group.groupId)" type="text" class="h-9 w-full rounded-lg border border-border/50 bg-card px-3 font-mono text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" :placeholder="group.platform === 'anthropic' ? summary.testModelConfig.anthropicModelId : group.platform === 'grok' ? summary.testModelConfig.grokModelId : summary.testModelConfig.openaiModelId" @input="handleGroupModelInput(group, $event)" />
                <select :value="groupModelConfig(group.groupId)?.mode || 'text'" class="h-9 rounded-lg border border-border/50 bg-card px-2 text-sm" :disabled="!groupModelValue(group.groupId)" @change="updateGroupModelField(group, 'mode', ($event.target as HTMLSelectElement).value)">
                  <option value="text">{{ t('admin.channelMonitor.testModel.modeText') }}</option>
                  <option value="image">{{ t('admin.channelMonitor.testModel.modeImage') }}</option>
                  <option value="video">{{ t('admin.channelMonitor.testModel.modeVideo') }}</option>
                  <option value="audio">音频</option>
                  <option value="connection">{{ t('admin.channelMonitor.testModel.modeConnection') }}</option>
                </select>
                <input :value="groupModelConfig(group.groupId)?.prompt || ''" type="text" class="h-9 rounded-lg border border-border/50 bg-card px-3 text-xs sm:col-span-2" :placeholder="t('admin.channelMonitor.testModel.promptPlaceholder')" :disabled="!groupModelValue(group.groupId)" @input="updateGroupModelField(group, 'prompt', ($event.target as HTMLInputElement).value)" />
                <template v-if="['image', 'video', 'audio'].includes(groupModelConfig(group.groupId)?.mode || '')">
                  <select :value="groupModelConfig(group.groupId)?.presetModel || ''" class="h-9 rounded-lg border border-border/50 bg-card px-2 text-xs" @change="updateGroupModelField(group, 'presetModel', ($event.target as HTMLSelectElement).value)">
                    <option value="">{{ t('admin.channelMonitor.testModel.exactPreset') }}</option>
                    <option v-for="preset in mediaPresets.filter(p => p.media_type === groupModelConfig(group.groupId)?.mode)" :key="preset.model" :value="preset.model">{{ preset.model }}</option>
                  </select>
                  <select :value="groupModelConfig(group.groupId)?.adapter || ''" class="h-9 rounded-lg border border-border/50 bg-card px-2 text-xs" @change="updateGroupModelField(group, 'adapter', ($event.target as HTMLSelectElement).value)">
                    <option value="">{{ t('admin.channelMonitor.testModel.presetProtocol') }}</option>
                    <option v-if="groupModelConfig(group.groupId)?.mode === 'image'" value="yunfei_gpt_images">OpenAI Images</option>
                    <option v-if="groupModelConfig(group.groupId)?.mode === 'image'" value="yunfei_gemini_native">Gemini Native</option>
                    <option v-if="groupModelConfig(group.groupId)?.mode === 'video'" value="v6k_video_gateway">OpenAI Videos</option>
                    <option v-if="groupModelConfig(group.groupId)?.mode === 'video'" value="ark_native">Ark Native</option>
                  </select>
                  <input :value="groupModelConfig(group.groupId)?.size || ''" class="h-9 rounded-lg border border-border/50 bg-card px-3 text-xs" :placeholder="t('admin.channelMonitor.testModel.size')" @input="updateGroupModelField(group, 'size', ($event.target as HTMLInputElement).value)" />
                  <p class="text-xs text-amber-700 dark:text-amber-300 sm:col-span-3">{{ t('admin.channelMonitor.testModel.realFrequency') }}</p>
                </template>
                <input v-if="groupModelConfig(group.groupId)?.mode === 'video'" :value="groupModelConfig(group.groupId)?.seconds || 0" type="number" min="0" max="120" class="h-9 rounded-lg border border-border/50 bg-card px-2 text-xs"  :placeholder="t('admin.channelMonitor.testModel.seconds')" @input="updateGroupModelField(group, 'seconds', ($event.target as HTMLInputElement).value)" />
              </div>
            </div>
          </div>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-foreground">{{ t('admin.channelMonitor.testModel.balanceRefreshInterval') }}</span>
            <div class="flex items-center gap-2">
              <input v-model.number="testModelForm.balanceRefreshIntervalMinutes" type="number" min="1" max="1440" class="h-10 w-28 rounded-xl border border-border/50 bg-surface px-3 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary" />
              <span class="text-sm text-muted-foreground">{{ t('admin.channelMonitor.testModel.minutes') }}</span>
            </div>
          </label>
          <div class="rounded-lg border border-border/50 bg-surface-elevated px-3 py-2 text-xs text-muted-foreground">
            {{ t('admin.channelMonitor.testModel.current', { openai: summary.testModelConfig.openaiModelId, anthropic: summary.testModelConfig.anthropicModelId, grok: summary.testModelConfig.grokModelId, minutes: summary.testModelConfig.balanceRefreshIntervalMinutes }) }}
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
        <div class="flex shrink-0 justify-end gap-2 border-t border-border/50 px-6 py-4">
          <Button variant="secondary" :disabled="isActionActive(channelActionKey(disconnectingChannel, 'disconnect'))" @click="closeDisconnect">{{ t('admin.channelMonitor.actions.cancel') }}</Button>
          <Button :variant="disconnectMode === 'full' ? 'destructive' : 'default'" class="gap-2" :disabled="isActionActive(channelActionKey(disconnectingChannel, 'disconnect'))" @click="confirmDisconnect">
            <Loader2 v-if="isActionActive(channelActionKey(disconnectingChannel, 'disconnect'))" class="h-4 w-4 animate-spin" />
            {{ t('admin.channelMonitor.disconnect.confirm') }}
          </Button>
        </div>
      </div>
    </div>

    <div v-if="groupEditingChannel" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div role="dialog" aria-modal="true" aria-label="修改绑定分组" class="flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden rounded-xl border border-border/50 bg-card shadow-xl">
        <div class="border-b border-border/50 px-6 py-5">
          <h2 class="text-lg font-semibold text-foreground">{{ t('admin.channelMonitor.groupsEditor.title') }}</h2>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ groupEditingChannel.adminAccountName || groupEditingChannel.adminAccountId }} · {{ groupEditingChannel.siteName }} · {{ groupEditingChannel.upstreamGroupName }}
          </p>
        </div>
        <div v-if="groupEditorError" class="mx-6 mt-5 rounded-xl border border-warning/20 bg-warning/10 p-3 text-sm text-warning">
          {{ groupEditorError }}
        </div>
        <div class="min-h-0 overflow-y-auto px-6 py-5">
          
          <p v-if="groupEditorStaleCount > 0" class="mt-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
            {{ t('admin.channelMonitor.groupsEditor.staleGroups', { count: groupEditorStaleCount }) }}
          </p>
          <Loader2 v-if="groupEditorLoading && groupEditorOwnGroups.length === 0" class="mt-5 h-5 w-5 animate-spin text-primary" />
          <div v-else-if="groupEditorOwnGroups.length === 0" class="mt-5 rounded-lg border border-border/50 bg-surface p-4 text-sm text-muted-foreground">
            {{ t('admin.channelMonitor.groupsEditor.empty') }}
          </div>
          <input v-if="groupEditorOwnGroups.length" v-model="groupEditorSearch" aria-label="搜索绑定分组" placeholder="搜索分组" class="mt-3 h-9 w-full rounded-lg border border-border/50 bg-surface px-3 text-sm" />
          <div v-if="groupEditorOwnGroups.length" class="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2">
            <label
              v-for="ownGroup in visibleEditorGroups"
              :key="ownGroup.id"
              :class="[
                'flex cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2.5 text-sm transition-colors',
                groupEditorOwnGroupIds.includes(ownGroup.id) ? 'border-teal-500/40 bg-teal-500/10 text-teal-800 dark:text-teal-200' : 'border-border/60 bg-surface text-foreground hover:border-teal-500/40'
              ]"
            >
              <span class="min-w-0">
                <span class="block truncate font-medium">{{ ownGroup.groupName }}</span>
                <span class="mt-0.5 block text-xs text-muted-foreground">{{ ownGroup.platform }} · {{ formatMultiplier(ownGroup.multiplier) }}</span>
              </span>
              <input v-model="groupEditorOwnGroupIds" type="checkbox" :value="ownGroup.id" :disabled="groupEditorLoading" class="h-4 w-4 shrink-0 accent-teal-600" />
            </label>
          </div>
          <PrimaryModelPicker class="mt-4" :disabled="groupEditorLoading" v-model="groupEditorTestModel" :site-id="groupEditingChannel.siteId" :group-id="groupEditingChannel.upstreamGroupId" :connection-id="groupEditingChannel.connectionId" />
          <label class="mt-3 flex cursor-pointer items-center justify-between gap-3 rounded-lg border border-border/50 bg-surface px-4 py-3 text-sm"><span>倍率大于等于分组时仍允许启用</span><input v-model="groupEditorAllowHigherRate" type="checkbox" class="h-4 w-4 shrink-0 accent-blue-600" /></label>
          <p class="mt-3 text-xs text-muted-foreground">{{ t('admin.channelMonitor.groupsEditor.selected', { count: groupEditorOwnGroupIds.length }) }}</p>
        </div>
        <div class="flex justify-end gap-2 border-t border-border/50 px-6 py-4">
          <Button variant="secondary" :disabled="groupEditorLoading" @click="closeGroupEditor">{{ t('admin.channelMonitor.actions.cancel') }}</Button>
          <Button class="gap-2 !bg-teal-600 hover:!bg-teal-700" :disabled="groupEditorLoading || groupEditorOwnGroups.length === 0 || groupEditorOwnGroupIds.length === 0" @click="saveGroupEditor">
            <Loader2 v-if="groupEditorLoading" class="h-4 w-4 animate-spin" />
            {{ t('admin.channelMonitor.groupsEditor.save') }}
          </Button>
        </div>
      </div>
    </div>

    <div v-if="isRateRuleEditorOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div class="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-xl border border-border/50 bg-card p-6 shadow-xl">
        <h2 class="text-lg font-semibold text-foreground">{{ t('admin.channelMonitor.rateRule.configureTitle') }}</h2>
        
        <div class="mt-5 space-y-4">
          <label class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.rateRule.form.enabled') }}</span>
              
            </span>
            <input v-model="rateRuleForm.enabled" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
          <label class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.rateRule.form.autoApplyOnCheck') }}</span>
              
            </span>
            <input v-model="rateRuleForm.autoApplyOnCheck" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
          <label class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.rateRule.form.updatePriority') }}</span>
              
            </span>
            <input v-model="rateRuleForm.updatePriority" type="checkbox" class="h-4 w-4 rounded border-border text-primary focus:ring-primary" />
          </label>
          <label class="flex items-center justify-between gap-4 rounded-lg border border-border/50 bg-surface px-4 py-3">
            <span>
              <span class="block text-sm font-medium text-foreground">{{ t('admin.channelMonitor.rateRule.form.stopWhenMissingRate') }}</span>
              
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
