<script setup lang="ts">
import DomainAllowlist from '../components/DomainAllowlist.vue'
import PrimaryModelPicker from '../components/PrimaryModelPicker.vue'
import MonitorProfileEditor from '../components/MonitorProfileEditor.vue'
import StatusSourceControl from '../components/StatusSourceControl.vue'
import GroupModelStatus from '../components/GroupModelStatus.vue'
import GroupKeyPolicy from '../components/GroupKeyPolicy.vue'
import GroupAPIAddress from '../components/GroupAPIAddress.vue'
import GroupBindingList from '../components/GroupBindingList.vue'
import UpstreamWhitelistDialog from '../components/upstream/UpstreamWhitelistDialog.vue'
import OperationProgress from '../components/OperationProgress.vue'
import { keyJobMessage, previewKeyRotation, rotateKeys } from '../api/keyRotation'
import ModelHealthExplorer from '../components/ModelHealthExplorer.vue'
import { operationJSON, followJob, checkedOperation,type OperationJob } from '../api/operations'
import type { UpstreamGroupInfo as PreviewGroup } from '../types/upstream'

import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, Plus, CheckCircle2, XCircle, X, Loader2, AlertCircle, Trash2, Edit2, LayoutGrid, List, RefreshCw, Settings2, LogIn, Shield, KeyRound } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Tooltip } from '@/components/ui/tooltip'
import { getStrategySettings, saveStrategySettings } from '../api/settings'
import { getMySiteMappingOptions, listRealConnections, realConnect, realDisconnect, updateRealConnectionGroups } from '../api/mySites'
import { listGroupRates } from '../api/groupRates'
import { useUpstreamSites } from '../composables/useUpstreamSites'
import SiteSettingsModal from '../components/upstream/SiteSettingsModal.vue'
import type { StrategySettings } from '../types/settings'
import type { UpstreamGroupInfo, UpstreamMetricValue, UpstreamSite, UpstreamSiteForm, UpstreamStatus } from '../types/upstream'
import type { MySiteMappingOwnGroupOption, RealConnection } from '../types/mySites'
import { NEW_API_CHANNEL_TYPES } from '../types/mySites'
import type { GroupRate } from '../types/groupRates'

const { t } = useI18n()

const searchQuery = ref('')
const domainsOpen = ref(false)
const isAddModalOpen = ref(false)
const { sites: upstreamSites, isAdding, isRefreshing, addErrorKey, connectedCount, siteSyncStates, syncingSiteIds, reloggingSiteIds, loadSites, updateSite, deleteSite, streamRefreshSites, refreshSingleSite, reloginSingleSite } = useUpstreamSites()
const deletingSiteId = ref<string | null>(null)
const deleteErrorKey = ref<string | null>(null)
const editingSiteId = ref<string | null>(null)
const initialAPIURL = ref('')
const initialWhitelistEnabled=ref(true)
const refreshSettings = ref<StrategySettings | null>(null)
const isRefreshSettingsOpen = ref(false)
const refreshSettingsEnabled = ref(false)
const refreshSettingsInterval = ref('60')
const isSavingRefreshSettings = ref(false)
const refreshSettingsError = ref('')
let snapshotTimer: ReturnType<typeof window.setInterval> | null = null
let snapshotLoading = false
const minimumRefreshInterval = 60

const viewMode = ref<'card' | 'list'>('card')
const realConnections = ref<RealConnection[]>([])
const ownGroups = ref<MySiteMappingOwnGroupOption[]>([])
const isGroupActionLoading = ref(false)
const groupActionError = ref('')
const groupActionGroup = ref<UpstreamGroupInfo | null>(null)
const groupActionConnectionID = ref('')
const groupActionSite = ref<UpstreamSite | null>(null)
const groupActionOwnGroupIds = ref<string[]>([])
const groupActionSearch = ref('')
const groupActionTestModel = ref('')
const groupActionAllowHigherRate = ref(false)
const isGroupActionOpen = ref(false)
const connectionsLoaded = ref(false)
const connectionsError = ref('')
const groupOptionsLoading = ref(false)
const groupActionType = ref('')
const groupChannelType = ref(0)
const adminPlatform = ref('')
const siteGroupRates = ref<GroupRate[]>([])
const disconnectMode = ref<'unlink' | 'full'>('full')
const normalizeType = (value?: string | null) => value?.trim().toLowerCase().replace(/\bxai\b/g, 'grok').replace(/\bclaude\b/g, 'anthropic') || ''
const channelTypeForGroupType: Record<string, number> = {
  openai: 1, anthropic: 14, gemini: 24, grok: 48, kimi: 25, moonshot: 25,
  zhipu: 16, deepseek: 43, codex: 57,
}
const groupType = (site: UpstreamSite, group: UpstreamGroupInfo) => normalizeType(
  group.platform
  || siteGroupRates.value.find(rate => rate.siteId === site.id && rate.groupId === group.id)?.type
  || groupConnection(site, group)?.groupType,
)
const actionConnection = computed(() => groupActionSite.value && groupActionGroup.value
  ? groupConnections(groupActionSite.value, groupActionGroup.value).find(item => item.id === groupActionConnectionID.value) : undefined)
const dynamicGroupTypes = computed(() => {
  const values = new Set<string>()
  for (const site of upstreamSites.value) {
    for (const group of site.metrics.groups) {
      for (const type of groupType(site, group).split(',').map(item => item.trim())) {
        if (type) values.add(type)
      }
    }
  }
  for (const group of ownGroups.value) {
    for (const type of normalizeType(group.platform).split(',').map(item => item.trim())) {
      if (type) values.add(type)
    }
  }
  return Array.from(values).sort()
})

const compatibleOwnGroups = computed(() => {
  if (adminPlatform.value === 'newapi' || !groupActionType.value) return ownGroups.value
  const matching = ownGroups.value.filter(group => normalizeType(group.platform) === groupActionType.value)
  return matching.length > 0 ? matching : ownGroups.value
})
watch(groupActionType, () => { if (!actionConnection.value) groupActionOwnGroupIds.value = [] })

const refreshStatus = computed(() => refreshSettings.value?.enableRefreshInterval
  ? t('admin.upstream.refresh.scheduled', { seconds: Math.max(refreshSettings.value.refreshInterval, minimumRefreshInterval) })
  : t('admin.upstream.refresh.disabled'))

const stopSnapshotPolling = () => {
  if (snapshotTimer !== null) window.clearInterval(snapshotTimer)
  snapshotTimer = null
}

const applyRefreshSettings = (settings: StrategySettings) => {
  refreshSettings.value = settings
  refreshSettingsEnabled.value = settings.enableRefreshInterval
  refreshSettingsInterval.value = String(Math.max(settings.refreshInterval, minimumRefreshInterval))
  stopSnapshotPolling()
  if (settings.enableRefreshInterval) {
    // The backend schedules upstream requests. The page only reads saved results.
    snapshotTimer = window.setInterval(async () => {
      if (document.hidden || snapshotLoading || isRefreshing.value || isAdding.value || syncingSiteIds.value.size || reloggingSiteIds.value.size || deletingSiteId.value) return
      snapshotLoading = true
      try {
        await loadSites()
      } catch {
        // Keep the current results when a snapshot request fails.
      } finally {
        snapshotLoading = false
      }
    }, Math.max(settings.refreshInterval, minimumRefreshInterval) * 1000)
  }
}

const loadRefreshSettings = async () => {
  try {
    const settings = await getStrategySettings()
    applyRefreshSettings(settings)
  } catch {
    stopSnapshotPolling()
  }
}

const openRefreshSettings = () => {
  refreshSettingsError.value = ''
  isRefreshSettingsOpen.value = true
}

const saveRefreshSettings = async () => {
  if (isSavingRefreshSettings.value) return
  isSavingRefreshSettings.value = true
  refreshSettingsError.value = ''
  try {
    const base = refreshSettings.value ?? await getStrategySettings()
    const settings = await saveStrategySettings({
      ...base,
      enableRefreshInterval: refreshSettingsEnabled.value,
      refreshInterval: Math.max(Number.parseInt(refreshSettingsInterval.value, 10) || minimumRefreshInterval, minimumRefreshInterval),
    })
    applyRefreshSettings(settings)
    isRefreshSettingsOpen.value = false
  } catch (error) {
    refreshSettingsError.value = error instanceof Error ? error.message : 'admin.settings.errors.request'
  } finally {
    isSavingRefreshSettings.value = false
  }
}

const createEmptyForm = (): UpstreamSiteForm => ({
  name: '',
  siteUrl: '',
  apiUrl: '',
  keyWhitelistEnabled:true,
  platform: 'auto',
  authMode: 'password',
  account: '',
  password: '',
  accessToken: '',
  refreshToken: '',
  tokenType: 'Bearer',
  rechargeRate: 1,
  remark: '',
  skipTlsVerify: false,
  monitorProfile:{template:'auto',path:''},
})

const newSiteForm = ref<UpstreamSiteForm>(createEmptyForm())

const preview = ref<{id:string,status:string,groups: PreviewGroup[],jobId?:string}|null>(null)
watch(()=>[newSiteForm.value.monitorProfile,newSiteForm.value.apiUrl],()=>{preview.value=null},{deep:true})
const previewBusy = ref(false)
const previewError = ref('')
const healthSite = ref<UpstreamSite | null>(null)
const handleAddSite = async () => {
  if(previewBusy.value)return
  if (!editingSiteId.value && !preview.value) {
    previewBusy.value=true;previewError.value=''
    try {
      const value=await operationJSON<{id:string,status:string,groups:PreviewGroup[],jobId:string}>('/upstream-previews',{method:'POST',body:JSON.stringify(newSiteForm.value)})
      const job=await followJob(await operationJSON('/channel-monitor/operations/'+value.jobId))
      if(job.failed)throw new Error(job.items.find(item=>item.error)?.error||'连接失败')
      preview.value=await operationJSON('/upstream-previews/'+value.id)
    } catch(e:any){previewError.value=e.message}finally{previewBusy.value=false}
    return
  }
  if (preview.value && !editingSiteId.value) {
    previewBusy.value=true;previewError.value=''
    try {await operationJSON('/upstream-previews/'+preview.value.id+'/commit',{method:'POST'});await loadSites();isAddModalOpen.value=false;preview.value=null;newSiteForm.value=createEmptyForm()}
    catch(e:any){previewError.value=e.message}finally{previewBusy.value=false}
    return
  }
  if (!editingSiteId.value) return
  const form = {...newSiteForm.value}
  if (form.apiUrl === initialAPIURL.value) delete form.apiUrl
  const success=await updateSite(editingSiteId.value,form)
  if (!success)return
  if(newSiteForm.value.keyWhitelistEnabled!==initialWhitelistEnabled.value){
   previewBusy.value=true
   try{const job=await followJob(await operationJSON<OperationJob>('/my-sites/key-security/site-whitelist',{method:'POST',body:JSON.stringify({siteId:editingSiteId.value})}));if(job.failed)throw new Error(job.items.find(i=>i.error)?.error||'白名单同步失败')}
   catch(e:any){previewError.value=e.message;return}finally{previewBusy.value=false}
  }
  void operationJSON(`/upstream-sites/${editingSiteId.value}/model-health/refresh`,{method:'POST'}).catch(()=>{})
  isAddModalOpen.value=false;newSiteForm.value=createEmptyForm();editingSiteId.value=null;preview.value=null
}

const handleEditSite = (site: UpstreamSite) => {
  editingSiteId.value = site.id
  initialAPIURL.value = site.settings.defaultApiUrl || ''
  initialWhitelistEnabled.value=!site.settings.keyWhitelistDisabled
  newSiteForm.value = {
    name: site.name,
    siteUrl: site.baseUrl,
    apiUrl: site.settings.defaultApiUrl || '',
    keyWhitelistEnabled:!site.settings.keyWhitelistDisabled,
    platform: site.platform,
    authMode: 'password',
    account: site.account,
    password: '',
    accessToken: '',
    refreshToken: '',
    tokenType: 'Bearer',
    rechargeRate: site.rechargeRate > 0 ? site.rechargeRate : 1,
    remark: site.remark,
    skipTlsVerify: Boolean(site.skipTlsVerify),
    monitorProfile:site.settings.monitorProfile?{...site.settings.monitorProfile}:{template:'auto',path:''},
  }
  isAddModalOpen.value = true
}

const closeSiteModal = () => {
  isAddModalOpen.value = false
  editingSiteId.value = null
  newSiteForm.value = createEmptyForm()
}

const requestDeleteSite = (id: string) => {
  deletingSiteId.value = id
  deleteErrorKey.value = null
}

const cancelDeleteSite = () => {
  deletingSiteId.value = null
  deleteErrorKey.value = null
}

const confirmDeleteSite = async () => {
  if (!deletingSiteId.value) return
  try {
    await deleteSite(deletingSiteId.value)
    cancelDeleteSite()
  } catch (error) {
    deleteErrorKey.value = error instanceof Error ? error.message : 'admin.upstream.errors.unknown'
  }
}

const filteredSites = computed(() => {
  if (!searchQuery.value) return upstreamSites.value
  return upstreamSites.value.filter(site =>
    site.name.toLowerCase().includes(searchQuery.value.toLowerCase())
    || site.baseUrl.toLowerCase().includes(searchQuery.value.toLowerCase())
  )
})

const statusClasses: Record<UpstreamStatus, string> = {
  connecting: 'bg-primary/10 text-primary border-primary/20',
  syncing: 'bg-warning/10 text-warning border-warning/20',
  connected: 'bg-signal/10 text-signal border-signal/20',
  error: 'bg-warning/10 text-warning border-warning/20',
}

const statusLabel = (status: UpstreamStatus): string => t(`admin.upstream.status.${status}`)

const deletingSite = computed(() => upstreamSites.value.find((site) => site.id === deletingSiteId.value) ?? null)

// Groups Modal Logic
const isGroupsModalOpen = ref(false)
const selectedSiteForGroups = ref<UpstreamSite | null>(null)

const openGroupsModal = async (site: UpstreamSite) => {
  selectedSiteForGroups.value = site
  isGroupsModalOpen.value = true
  groupActionError.value = ''
  await loadGroupConnections()
  try {
    const rates: GroupRate[] = []
    let page = 1
    let totalPages = 1
    do {
      const response = await listGroupRates({ page, site: site.name, search: '', platform: '', type: '' })
      rates.push(...response.items)
      totalPages = response.totalPages
    } while (++page <= totalPages)
    siteGroupRates.value = rates
  } catch { /* Raw upstream types remain available. */ }
}

const loadGroupConnections = async () => {
  try {
    realConnections.value = await listRealConnections() ?? []
    connectionsLoaded.value = true
    connectionsError.value = ''
  } catch {
    connectionsLoaded.value = false
    connectionsError.value = t('admin.groupRates.errors.request')
  }
}

const groupConnections = (site: UpstreamSite, group: UpstreamGroupInfo) => realConnections.value.filter(connection =>
  connection.upstreamSiteId === site.id
  && (connection.upstreamGroupId && group.id ? connection.upstreamGroupId === group.id : connection.upstreamGroupName === group.name),
)

const groupConnection = (site: UpstreamSite, group: UpstreamGroupInfo) => groupConnections(site, group)[0]
const whitelistSite = ref<UpstreamSite | null>(null)
const rotatingSite = ref('')
const keyActionMessage = ref('')
const keyActionError = ref('')
const connectionIdsForSite = (siteId: string) => realConnections.value
  .filter(connection => connection.upstreamSiteId === siteId && connection.adminAccountId && connection.upstreamKeyId)
  .map(connection => connection.id)

async function rotateSite(site: UpstreamSite) {
  if (rotatingSite.value) return
  keyActionMessage.value = ''
  keyActionError.value = ''
  rotatingSite.value = site.id
  try {
    await loadGroupConnections()
    if (!connectionsLoaded.value) throw new Error('本站绑定读取失败，请重试')
    const connectionIds = connectionIdsForSite(site.id)
    if (!connectionIds.length) throw new Error('本站没有可轮换的绑定 Key')
    const targets = { connectionIds }
    const preview = await previewKeyRotation(targets)
    if (!preview.keys) throw new Error('本站没有可轮换的绑定 Key')
    const skipped = preview.skipped ? `，${preview.skipped} 条记录将跳过` : ''
    if (!window.confirm(`轮换 ${site.name} 的 ${preview.keys} 个已绑定 Key${skipped}？系统会核验转发账号后撤销旧 Key。`)) return
    keyActionMessage.value = `${site.name} 的 Key 正在轮换…`
    const job = await rotateKeys(targets, preview.version)
    const taskSkipped = job.items.filter(item => item.result?.skipped).length
    keyActionMessage.value = `${site.name} 已轮换 ${job.total - job.failed - taskSkipped} 个 Key，${job.failed} 个失败，${preview.skipped + taskSkipped} 个跳过`
    keyActionError.value = keyJobMessage(job)
    await loadGroupConnections()
    window.dispatchEvent(new Event('transithub:key-policy'))
  } catch (error) {
    keyActionMessage.value = ''
    keyActionError.value = error instanceof Error ? error.message : String(error)
  } finally {
    rotatingSite.value = ''
  }
}
const chosenGroupConnections = computed(() => groupActionSite.value && groupActionGroup.value ? groupConnections(groupActionSite.value, groupActionGroup.value) : [])
const occupiedGroupIds = computed(() => new Set(chosenGroupConnections.value.flatMap(connection => connection.ownGroupIds ?? [])))
const unusedOwnGroups = computed(() => adminPlatform.value === 'sub2api'
  ? compatibleOwnGroups.value.filter(group => !occupiedGroupIds.value.has(group.id))
  : compatibleOwnGroups.value)
const visibleOwnGroups = computed(() => unusedOwnGroups.value.filter(group => group.groupName.toLowerCase().includes(groupActionSearch.value.trim().toLowerCase())))
const canCreateGroupBinding = computed(() => !actionConnection.value && !!adminPlatform.value && !groupOptionsLoading.value
  && groupActionOwnGroupIds.value.length > 0
  && groupActionOwnGroupIds.value.every(id => unusedOwnGroups.value.some(group => group.id === id))
  && (adminPlatform.value === 'newapi' ? groupChannelType.value > 0 : !!groupActionType.value))
const chooseGroupConnection = (id: string) => {
  groupActionConnectionID.value = id
  const connection = chosenGroupConnections.value.find(item => item.id === id)
  groupActionOwnGroupIds.value = [...(connection?.ownGroupIds || [])]
  groupActionTestModel.value = connection?.testModelId || ''
  groupActionAllowHigherRate.value = connection?.allowWhenUpstreamRateGteOwn ?? false
  groupActionSearch.value = ''
  disconnectMode.value = 'full'
  groupActionError.value = ''
}

const connectedGroupCount = (site: UpstreamSite) => site.metrics.groups.filter(group => Boolean(groupConnection(site, group))).length

const openGroupAction = async (site: UpstreamSite, group: UpstreamGroupInfo) => {
  if (!connectionsLoaded.value || isGroupActionLoading.value) return
  groupActionSite.value = site
  groupActionGroup.value = group
  adminPlatform.value = ''
  ownGroups.value = []
  chooseGroupConnection(groupConnection(site, group)?.id || '')
  groupActionError.value = ''
  isGroupActionOpen.value = true
  groupActionType.value = groupType(site, group)
  groupChannelType.value = channelTypeForGroupType[groupActionType.value]
    ?? NEW_API_CHANNEL_TYPES.find(type => normalizeType(type.name) === groupActionType.value)?.id
    ?? 0
  disconnectMode.value = 'full'
  groupOptionsLoading.value = true
  try {
    const [options, connections] = await Promise.all([getMySiteMappingOptions(), listRealConnections()])
    if (groupActionSite.value?.id !== site.id || groupActionGroup.value?.id !== group.id) return
    realConnections.value = connections ?? []
    ownGroups.value = options.ownGroups ?? []
    adminPlatform.value = options.platform
    chooseGroupConnection(groupConnection(site, group)?.id || '')
  } catch (error) {
    groupActionError.value = error instanceof Error ? t(error.message) : t('admin.upstream.errors.request')
  } finally {
    groupOptionsLoading.value = false
  }
}

const closeGroupAction = () => {
  if (isGroupActionLoading.value || groupOptionsLoading.value) return
  isGroupActionOpen.value = false
  groupActionGroup.value = null
  groupActionSite.value = null
  groupActionOwnGroupIds.value = []
  groupActionTestModel.value = ''
  groupActionAllowHigherRate.value = false
}

const submitGroupAction = async () => {
  const site = groupActionSite.value
  const group = groupActionGroup.value
  if (!site || !group || isGroupActionLoading.value || !canCreateGroupBinding.value) return
  isGroupActionLoading.value = true
  groupActionError.value = ''
  try {
    const result = await realConnect({
      upstreamSiteId: site.id,
      upstreamGroupId: group.id,
      upstreamGroupName: group.name,
      groupType: groupActionType.value,
      channelType: adminPlatform.value === 'newapi' ? groupChannelType.value : undefined,
      ownGroupIds: groupActionOwnGroupIds.value,
      testModelId: groupActionTestModel.value,
      allowWhenUpstreamRateGteOwn: groupActionAllowHigherRate.value,
    })
    const created = result.connections || [result.connection]
    const createdById = new Map(created.map(item => [item.id, item]))
    realConnections.value = [...createdById.values(), ...realConnections.value.filter(item => !createdById.has(item.id))]
    chooseGroupConnection(created[0]?.id || '')
  } catch (error) {
    await loadGroupConnections()
    groupActionOwnGroupIds.value = groupActionOwnGroupIds.value.filter(id => unusedOwnGroups.value.some(group => group.id === id))
    groupActionError.value = error instanceof Error ? t(error.message) : t('admin.upstream.errors.request')
  } finally {
    isGroupActionLoading.value = false
  }
}

const saveGroupBinding = async () => {
  const connection=actionConnection.value
  if(!connection||!adminPlatform.value||isGroupActionLoading.value||groupOptionsLoading.value||!groupActionOwnGroupIds.value.length)return
  isGroupActionLoading.value=true;groupActionError.value=''
  try {
    const updated=await updateRealConnectionGroups(connection.id,{connectionId:connection.id,ownGroupIds:[...groupActionOwnGroupIds.value],testModelId:groupActionTestModel.value,allowWhenUpstreamRateGteOwn:groupActionAllowHigherRate.value})
    realConnections.value=realConnections.value.map(item=>item.id===updated.id?updated:item)
    await checkedOperation({kind:'refresh'})
    isGroupActionLoading.value=false;closeGroupAction()
  }catch(error:any){groupActionError.value=error?.message?.startsWith('admin.')?t(error.message):error.message||'保存分组失败'}finally{isGroupActionLoading.value=false}
}

const disconnectGroupAction = async () => {
  const connection = actionConnection.value
  if (!connection || isGroupActionLoading.value) return
  isGroupActionLoading.value = true
  groupActionError.value = ''
  try {
    await realDisconnect({ connectionId: connection.id, mode: disconnectMode.value })
    realConnections.value = realConnections.value.filter(item => item.id !== connection.id)
    isGroupActionLoading.value = false
    closeGroupAction()
  } catch (error) {
    groupActionError.value = error instanceof Error ? t(error.message) : t('admin.upstream.errors.request')
  } finally {
    isGroupActionLoading.value = false
  }
}

const closeGroupsModal = () => {
  isGroupsModalOpen.value = false
  selectedSiteForGroups.value = null
}

const isSiteSettingsOpen = ref(false)
const selectedSiteForSettings = ref<UpstreamSite | null>(null)

const openSiteSettings = (site: UpstreamSite) => {
  selectedSiteForSettings.value = site
  isSiteSettingsOpen.value = true
}

const closeSiteSettings = () => {
  isSiteSettingsOpen.value = false
  selectedSiteForSettings.value = null
}

const onSiteSettingsSaved = (siteId: string, settings: { balanceThreshold: number | null }) => {
  const site = upstreamSites.value.find(s => s.id === siteId)
  if (site) {
    site.settings = settings
  }
}

const groupedGroups = computed<Record<string, UpstreamGroupInfo[]>>(() => {
  if (!selectedSiteForGroups.value) return {}
  const groups = selectedSiteForGroups.value.metrics.groups
  return groups.reduce<Record<string, UpstreamGroupInfo[]>>((acc, group) => {
    const platform = groupType(selectedSiteForGroups.value!, group) || t('admin.upstream.fields.unknownPlatform')
    if (!acc[platform]) acc[platform] = []
    acc[platform].push(group)
    return acc
  }, {})
})

const cnyMetricDisplay = (site: UpstreamSite, metric: UpstreamMetricValue): string | null => {
  if (metric.value === null || !Number.isFinite(metric.value) || site.rechargeRate <= 0 || !Number.isFinite(site.rechargeRate)) return null
  return t('admin.upstream.currency.cnyValue', { amount: (metric.value * site.rechargeRate).toFixed(2) })
}

const usageSources: Record<string, string> = {
  sub2api_dashboard_stats: 'Sub2API 用量概览',
  sub2api_dashboard_snapshot: 'Sub2API 用量趋势',
  newapi_log_stat: 'New API 消费日志统计',
}
const usageDay = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' })
function todayUsageAvailable(site: UpstreamSite) {
  const metric = site.metrics.todayConsume
  if (site.status === 'error' || metric.value == null || metric.state === 'unavailable') return false
  const recordedDay = metric.date || (site.lastSyncedAt ? usageDay.format(new Date(site.lastSyncedAt)) : '')
  return recordedDay === usageDay.format(new Date())
}
function todayUsageSource(site: UpstreamSite) {
  return todayUsageAvailable(site) ? usageSources[site.metrics.todayConsume.source || ''] || '上次同步的消费数据' : '当前无法读取上游今日消费'
}
const usdMetricDisplay = (metric: UpstreamMetricValue): string => {
  if (metric.display.toUpperCase().includes('USD')) return metric.display
  return t('admin.upstream.currency.usdValue', { amount: metric.display })
}

const lastUpdatedDisplay = (site: UpstreamSite): string => {
  if (!site.lastSyncedAt) return t('admin.upstream.fields.notSynced')
  return new Date(site.lastSyncedAt).toLocaleString()
}

onMounted(() => {
  void loadRefreshSettings()
  void loadGroupConnections()
})

onBeforeUnmount(() => {
  stopSnapshotPolling()
})
</script>

<template>
  <DomainAllowlist v-if="domainsOpen" @close="domainsOpen=false" />
  <UpstreamWhitelistDialog v-if="whitelistSite" :site-id="whitelistSite.id" :name="whitelistSite.name" :platform="whitelistSite.platform" :connection-ids="connectionIdsForSite(whitelistSite.id)" @close="whitelistSite = null" />
  <div class="space-y-6">
    <OperationProgress :kinds="['key_security']" @completed="loadGroupConnections" />
    <p v-if="keyActionMessage" role="status" class="text-sm text-muted-foreground">{{ keyActionMessage }}</p>
    <p v-if="keyActionError" role="alert" class="whitespace-pre-line text-sm text-destructive">{{ keyActionError }}</p>
    <!-- Top Action Bar -->
    <div class="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
      <div class="flex flex-col gap-3 w-full sm:w-auto">
        <div class="relative w-full sm:w-80">
          <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
          <input
            v-model="searchQuery"
            type="text"
            :placeholder="t('admin.upstream.searchPlaceholder')"
            class="w-full h-10 pl-10 pr-4 rounded-xl bg-surface border border-border/50 focus:border-primary focus:ring-1 focus:ring-primary outline-none transition-all text-sm text-foreground placeholder:text-muted-foreground"
          />
        </div>
        <p class="text-xs text-muted-foreground">
          {{ t('admin.upstream.summary', { connected: connectedCount, total: upstreamSites.length }) }}
        </p>
      </div>

      <div class="flex flex-wrap items-center gap-2 w-full sm:w-auto sm:justify-end">
        <div class="flex items-center bg-surface border border-border/50 rounded-xl p-1 shrink-0">
          <button
            @click="viewMode = 'list'"
            :class="{'bg-card shadow-sm text-foreground': viewMode === 'list', 'text-muted-foreground hover:text-foreground': viewMode !== 'list'}"
            class="p-1.5 rounded-lg transition-all"
            :title="t('admin.upstream.viewMode.list')"
          >
            <List class="w-4 h-4" />
          </button>
          <button
            @click="viewMode = 'card'"
            :class="{'bg-card shadow-sm text-foreground': viewMode === 'card', 'text-muted-foreground hover:text-foreground': viewMode !== 'card'}"
            class="p-1.5 rounded-lg transition-all"
            :title="t('admin.upstream.viewMode.card')"
          >
            <LayoutGrid class="w-4 h-4" />
          </button>
        </div>
        <div class="hidden md:flex h-10 items-center rounded-xl border border-border/50 bg-surface px-3 text-xs text-muted-foreground whitespace-nowrap">
          {{ refreshStatus }}
        </div>
        <Tooltip :text="t('admin.upstream.refresh.settings')">
          <button
            type="button"
            class="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-border/50 bg-surface text-muted-foreground transition-colors hover:border-primary/60 hover:bg-primary/10 hover:text-primary"
            @click="openRefreshSettings"
          >
            <Settings2 class="h-4 w-4" />
          </button>
        </Tooltip>
        <Button :disabled="isRefreshing" @click="streamRefreshSites" variant="secondary" class="w-full sm:w-auto h-10 rounded-xl px-4 gap-2">
          <Loader2 v-if="isRefreshing" class="w-4 h-4 animate-spin" />
          <RefreshCw v-else class="w-4 h-4" />
          {{ isRefreshing ? t('admin.upstream.refresh.refreshing') : t('admin.upstream.refresh.action') }}
        </Button>
        <Button variant="secondary" class="h-10 shrink-0 rounded-xl px-4" @click="domainsOpen=true">域名白名单</Button>
        <Button @click="isAddModalOpen = true" class="w-full sm:w-auto h-10 rounded-xl px-4 gap-2 bg-primary text-primary-foreground hover:bg-primary/90 shadow-sm">
          <Plus class="w-4 h-4" />
          {{ t('admin.upstream.addSite') }}
        </Button>
      </div>
    </div>

    <!-- Cards Grid -->
    <div v-if="viewMode === 'card'" class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-6">
      <div
        v-for="site in filteredSites"
        :key="site.id"
        class="group relative bg-card border border-border/60 rounded-2xl p-5 hover:border-primary/50 transition-colors shadow-sm hover:shadow-md"
      >
        <!-- Sync Progress Overlay -->
        <div
          v-if="siteSyncStates.get(site.id)?.phase && siteSyncStates.get(site.id)?.phase !== 'idle'"
          class="absolute inset-0 z-10 flex flex-col items-center justify-center rounded-2xl backdrop-blur-sm transition-all"
          :class="{
            'bg-background/60': siteSyncStates.get(site.id)?.phase === 'syncing',
            'bg-signal/10 dark:bg-signal/5': siteSyncStates.get(site.id)?.phase === 'done',
            'bg-destructive/10 dark:bg-destructive/5': siteSyncStates.get(site.id)?.phase === 'error',
          }"
        >
          <template v-if="siteSyncStates.get(site.id)?.phase === 'syncing'">
            <Loader2 class="h-6 w-6 animate-spin text-primary" />
            <span class="mt-2 text-sm font-medium text-foreground">{{ t('admin.upstream.syncStream.syncing') }}</span>
          </template>
          <template v-else-if="siteSyncStates.get(site.id)?.phase === 'done'">
            <CheckCircle2 class="h-6 w-6 text-signal" />
            <span class="mt-2 text-sm font-medium text-signal">{{ t('admin.upstream.syncStream.done') }}</span>
          </template>
          <template v-else-if="siteSyncStates.get(site.id)?.phase === 'error'">
            <XCircle class="h-6 w-6 text-destructive" />
            <span class="mt-2 max-w-[90%] break-words text-center text-sm font-medium text-destructive">{{ t(siteSyncStates.get(site.id)?.errorKey || site.errorKey || 'admin.upstream.syncStream.error') }}</span>
          </template>
        </div>

        <!-- Card Header -->
        <div class="flex flex-col gap-4 mb-5 border-b border-border/40 pb-4">
          <div class="flex items-start justify-between gap-2">
            <div class="flex items-center gap-3 min-w-0">
              <div :class="['w-10 h-10 rounded-xl flex items-center justify-center font-bold text-lg shrink-0', site.logoBg]">
                {{ site.logo }}
              </div>
              <div class="flex flex-col min-w-0">
                <a :href="site.baseUrl" target="_blank" rel="noopener noreferrer" class="font-semibold text-lg text-foreground hover:text-primary transition-colors cursor-pointer truncate" :title="site.name">
                  {{ site.name }}
                </a><button type="button" class="ml-2 text-xs text-primary" @click.stop="healthSite=site">模型状态</button>
                <span class="px-2 py-0.5 mt-1 rounded-md bg-primary/10 text-primary border border-primary/20 text-[10px] font-bold uppercase tracking-wider w-fit">
                  {{ t(`admin.upstream.modal.form.platforms.${site.platform}`) }}
                </span>
              </div>
            </div>

            <div
              class="flex items-center gap-1.5 px-2 py-1 rounded-md text-[11px] font-medium border shrink-0"
              :class="statusClasses[site.status]"
            >
              <Loader2 v-if="site.status === 'connecting' || site.status === 'syncing'" class="w-3 h-3 animate-spin" />
              <CheckCircle2 v-else-if="site.status === 'connected'" class="w-3 h-3" />
              <XCircle v-else class="w-3 h-3" />
              {{ statusLabel(site.status) }}
            </div>
          </div>
        </div>

        <!-- Card Body (Stats) -->
        <div class="space-y-4">
          <div class="grid grid-cols-3 gap-3">
            <div class="flex flex-col items-center justify-center p-3 rounded-xl bg-surface/50 border border-border/40">
              <span class="text-xs text-muted-foreground mb-1">{{ t('admin.upstream.fields.balance') }}</span>
              <span v-if="cnyMetricDisplay(site, site.metrics.balance)" class="font-bold text-primary text-sm text-center">
                {{ cnyMetricDisplay(site, site.metrics.balance) }}
              </span>
              <span :class="[cnyMetricDisplay(site, site.metrics.balance) ? 'text-[10px] font-medium text-primary/70 mt-0.5' : 'font-bold text-primary text-sm', 'text-center']">
                {{ usdMetricDisplay(site.metrics.balance) }}
              </span>
            </div>
            <div :title="todayUsageSource(site)" class="flex flex-col items-center justify-center p-3 rounded-xl bg-surface/50 border border-border/40">
              <span class="text-xs text-muted-foreground mb-1">{{ t('admin.upstream.fields.todayConsume') }}</span>
              <template v-if="todayUsageAvailable(site)">
                <span v-if="cnyMetricDisplay(site, site.metrics.todayConsume)" :class="['font-bold text-sm text-center', site.metrics.todayConsume.value && site.metrics.todayConsume.value > 0 ? 'text-orange-500' : 'text-foreground']">
                {{ cnyMetricDisplay(site, site.metrics.todayConsume) }}
              </span>
              <span :class="[cnyMetricDisplay(site, site.metrics.todayConsume) ? 'text-[10px] font-medium mt-0.5' : 'font-bold text-sm', site.metrics.todayConsume.value && site.metrics.todayConsume.value > 0 ? (cnyMetricDisplay(site, site.metrics.todayConsume) ? 'text-orange-500/70' : 'text-orange-500') : (cnyMetricDisplay(site, site.metrics.todayConsume) ? 'text-muted-foreground' : 'text-foreground'), 'text-center']">
                {{ usdMetricDisplay(site.metrics.todayConsume) }}
              </span>
              </template>
              <span v-else class="text-sm font-medium text-muted-foreground">不可用</span>
            </div>
            <div class="flex flex-col items-center justify-center p-3 rounded-xl bg-surface/50 border border-border/40">
              <span class="text-xs text-muted-foreground mb-1">{{ t('admin.upstream.fields.historyRecharge') }}</span>
              <span v-if="cnyMetricDisplay(site, site.metrics.historyRecharge)" class="font-bold text-foreground text-sm text-center">
                {{ cnyMetricDisplay(site, site.metrics.historyRecharge) }}
              </span>
              <span :class="[cnyMetricDisplay(site, site.metrics.historyRecharge) ? 'text-[10px] font-medium text-muted-foreground mt-0.5' : 'font-bold text-foreground text-sm', 'text-center']">
                {{ usdMetricDisplay(site.metrics.historyRecharge) }}
              </span>
            </div>
          </div>

          <Button
            v-if="site.metrics.groups.length > 0"
            variant="secondary"
            class="w-full h-9 text-xs font-medium bg-surface hover:bg-surface-elevated border-border/50 border"
            @click="openGroupsModal(site)"
          >
            {{ t('admin.upstream.fields.viewAvailableGroups') }} · {{ t('admin.upstream.fields.connected') }} {{ connectionsLoaded ? connectedGroupCount(site) : '-' }}/{{ site.metrics.groups.length }}
          </Button>

          <!-- Card Actions (Edit/Delete) -->
          <div class="flex flex-wrap items-center justify-between gap-3 pt-4 mt-2 border-t border-border/40">
            <div class="min-w-0 text-left text-[11px] leading-5 text-muted-foreground">
              <span class="block truncate">{{ t('admin.upstream.fields.lastUpdated') }}</span>
              <span class="block truncate font-medium text-foreground/80">{{ lastUpdatedDisplay(site) }}</span>
            </div>
            <div class="flex shrink-0 items-center justify-end gap-2">
              <Tooltip text="设置白名单"><button type="button" aria-label="设置白名单" class="grid h-8 w-8 place-items-center rounded-lg border border-border/60 text-cyan-700 hover:bg-cyan-500/10 dark:text-cyan-300" @click="whitelistSite = site"><Shield class="h-4 w-4" /></button></Tooltip>
              <Tooltip text="一键轮换 Key"><button type="button" aria-label="一键轮换 Key" class="grid h-8 w-8 place-items-center rounded-lg border border-border/60 text-orange-700 hover:bg-orange-500/10 disabled:opacity-50 dark:text-orange-300" :disabled="!!rotatingSite || !connectionIdsForSite(site.id).length" @click="rotateSite(site)"><Loader2 v-if="rotatingSite === site.id" class="h-4 w-4 animate-spin" /><KeyRound v-else class="h-4 w-4" /></button></Tooltip>
              <Tooltip :text="syncingSiteIds.has(site.id) ? t('admin.upstream.action.syncing') : t('admin.upstream.action.sync')">
                <button
                  type="button"
                  class="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border/60 text-muted-foreground transition-colors hover:border-primary/60 hover:bg-primary/10 hover:text-primary"
                  :disabled="syncingSiteIds.has(site.id)"
                  @click="refreshSingleSite(site.id)"
                >
                  <Loader2 v-if="syncingSiteIds.has(site.id)" class="h-4 w-4 animate-spin" />
                  <RefreshCw v-else class="h-4 w-4" />
                </button>
              </Tooltip>
              <Tooltip :text="site.canRelogin ? (reloggingSiteIds.has(site.id) ? t('admin.upstream.action.relogging') : t('admin.upstream.action.relogin')) : t('admin.upstream.action.reloginUnavailable')">
                <button
                  type="button"
                  :class="['inline-flex h-8 w-8 items-center justify-center rounded-lg border transition-colors disabled:cursor-not-allowed disabled:opacity-40', site.status === 'error' && site.canRelogin ? 'border-warning/50 bg-warning/10 text-warning hover:bg-warning/20' : 'border-border/60 text-muted-foreground hover:border-primary/60 hover:bg-primary/10 hover:text-primary']"
                  :disabled="!site.canRelogin || syncingSiteIds.has(site.id) || reloggingSiteIds.has(site.id)"
                  @click="reloginSingleSite(site.id)"
                >
                  <Loader2 v-if="reloggingSiteIds.has(site.id)" class="h-4 w-4 animate-spin" />
                  <LogIn v-else class="h-4 w-4" />
                </button>
              </Tooltip>
              <Tooltip :text="t('admin.upstream.action.settings')">
                <button
                  type="button"
                  class="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border/60 text-muted-foreground transition-colors hover:border-primary/60 hover:bg-primary/10 hover:text-primary"
                  @click="openSiteSettings(site)"
                >
                  <Settings2 class="h-4 w-4" />
                </button>
              </Tooltip>
              <Tooltip :text="t('admin.upstream.action.edit')">
                <button
                  type="button"
                  class="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border/60 text-muted-foreground transition-colors hover:border-primary/60 hover:bg-primary/10 hover:text-primary"
                  aria-label="编辑上游" @click="handleEditSite(site)"
                >
                  <Edit2 class="h-4 w-4" />
                </button>
              </Tooltip>
              <Tooltip :text="t('admin.upstream.delete.action')">
                <button
                  type="button"
                  class="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border/60 text-muted-foreground transition-colors hover:border-red-400/60 hover:bg-red-500/10 hover:text-red-400"
                  @click="requestDeleteSite(site.id)"
                >
                  <Trash2 class="h-4 w-4" />
                </button>
              </Tooltip>
            </div>
          </div>
        </div>

        <div v-if="site.errorKey" class="mt-4 flex items-start gap-2 rounded-xl border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
          <AlertCircle class="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{{ t(site.errorKey) }}</span>
        </div>
      </div>
    </div>

    <!-- Table (List) View -->
    <div v-if="viewMode === 'list'" class="rounded-2xl border border-border/60 bg-card overflow-hidden shadow-sm">
      <div class="overflow-x-auto">
        <table class="w-full text-sm text-left">
          <thead class="bg-surface/50 text-muted-foreground border-b border-border/40">
            <tr>
              <th class="px-6 py-4 font-medium">{{ t('admin.upstream.fields.siteName') }}</th>
              <th class="px-6 py-4 font-medium">{{ t('admin.upstream.fields.platform') }}</th>
              <th class="px-6 py-4 font-medium">{{ t('admin.upstream.status.connected') }}</th>
              <th class="px-6 py-4 font-medium">{{ t('admin.upstream.fields.balance') }}</th>
              <th class="px-6 py-4 font-medium">{{ t('admin.upstream.fields.todayConsume') }}</th>
              <th class="px-6 py-4 font-medium">{{ t('admin.upstream.fields.historyRecharge') }}</th>
              <th class="px-6 py-4 font-medium text-right">{{ t('admin.upstream.action.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border/40">
            <tr v-for="site in filteredSites" :key="site.id" class="hover:bg-surface/30 transition-colors">
              <td class="px-6 py-4">
                <div class="flex items-center gap-3">
                  <div :class="['w-8 h-8 rounded-lg flex items-center justify-center font-bold text-sm shrink-0', site.logoBg]">
                    {{ site.logo }}
                  </div>
                  <a :href="site.baseUrl" target="_blank" rel="noopener noreferrer" class="font-medium text-foreground hover:text-primary transition-colors truncate max-w-[150px] inline-block">
                    {{ site.name }}
                  </a>
                </div>
              </td>
              <td class="px-6 py-4">
                <span class="px-2 py-1 rounded-md bg-primary/10 text-primary border border-primary/20 text-xs font-semibold uppercase tracking-wider">
                  {{ t(`admin.upstream.modal.form.platforms.${site.platform}`) }}
                </span>
              </td>
              <td class="px-6 py-4">
                <div
                  v-if="siteSyncStates.get(site.id)?.phase && siteSyncStates.get(site.id)?.phase !== 'idle'"
                  class="inline-flex items-center gap-1.5 text-xs font-medium"
                  :class="{
                    'text-primary': siteSyncStates.get(site.id)?.phase === 'syncing',
                    'text-signal': siteSyncStates.get(site.id)?.phase === 'done',
                    'text-destructive': siteSyncStates.get(site.id)?.phase === 'error',
                  }"
                >
                  <Loader2 v-if="siteSyncStates.get(site.id)?.phase === 'syncing'" class="w-3.5 h-3.5 animate-spin" />
                  <CheckCircle2 v-else-if="siteSyncStates.get(site.id)?.phase === 'done'" class="w-3.5 h-3.5" />
                  <XCircle v-else class="w-3.5 h-3.5" />
                  <template v-if="siteSyncStates.get(site.id)?.phase === 'syncing'">{{ t('admin.upstream.syncStream.syncing') }}</template>
                  <template v-else-if="siteSyncStates.get(site.id)?.phase === 'done'">{{ t('admin.upstream.syncStream.done') }}</template>
                  <template v-else>{{ t('admin.upstream.syncStream.error') }}</template>
                </div>
                <div
                  v-else
                  class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border"
                  :class="statusClasses[site.status]"
                >
                  <Loader2 v-if="site.status === 'connecting' || site.status === 'syncing'" class="w-3.5 h-3.5 animate-spin" />
                  <CheckCircle2 v-else-if="site.status === 'connected'" class="w-3.5 h-3.5" />
                  <XCircle v-else class="w-3.5 h-3.5" />
                  {{ statusLabel(site.status) }}
                </div>
                <p
                  v-if="siteSyncStates.get(site.id)?.phase === 'error' || site.errorKey"
                  class="mt-1 max-w-[180px] break-words text-xs leading-4 text-warning"
                >
                  {{ t(siteSyncStates.get(site.id)?.errorKey || site.errorKey || 'admin.upstream.syncStream.error') }}
                </p>
              </td>
              <td class="px-6 py-4">
                <div class="flex flex-col gap-0.5">
                  <span v-if="cnyMetricDisplay(site, site.metrics.balance)" class="font-medium text-primary">
                    {{ cnyMetricDisplay(site, site.metrics.balance) }}
                  </span>
                  <span :class="[cnyMetricDisplay(site, site.metrics.balance) ? 'text-xs font-medium text-primary/70' : 'font-medium text-primary']">
                    {{ usdMetricDisplay(site.metrics.balance) }}
                  </span>
                </div>
              </td>
              <td class="px-6 py-4">
                <div :title="todayUsageSource(site)" class="flex flex-col gap-0.5">
                  <template v-if="todayUsageAvailable(site)">
                  <span v-if="cnyMetricDisplay(site, site.metrics.todayConsume)" :class="['font-medium', site.metrics.todayConsume.value && site.metrics.todayConsume.value > 0 ? 'text-orange-500' : 'text-muted-foreground']">
                    {{ cnyMetricDisplay(site, site.metrics.todayConsume) }}
                  </span>
                  <span :class="[cnyMetricDisplay(site, site.metrics.todayConsume) ? 'text-xs font-medium' : 'font-medium', site.metrics.todayConsume.value && site.metrics.todayConsume.value > 0 ? (cnyMetricDisplay(site, site.metrics.todayConsume) ? 'text-orange-500/70' : 'text-orange-500') : 'text-muted-foreground']">
                    {{ usdMetricDisplay(site.metrics.todayConsume) }}
                  </span>
                  </template>
                  <span v-else class="font-medium text-muted-foreground">不可用</span>
                </div>
              </td>
              <td class="px-6 py-4">
                <div class="flex flex-col gap-0.5">
                  <span v-if="cnyMetricDisplay(site, site.metrics.historyRecharge)" class="font-medium text-muted-foreground">
                    {{ cnyMetricDisplay(site, site.metrics.historyRecharge) }}
                  </span>
                  <span :class="[cnyMetricDisplay(site, site.metrics.historyRecharge) ? 'text-xs font-medium text-muted-foreground' : 'text-muted-foreground']">
                    {{ usdMetricDisplay(site.metrics.historyRecharge) }}
                  </span>
                </div>
              </td>
              <td class="px-6 py-4 text-right">
                <div class="flex items-center justify-end gap-2">
                  <Button
                    v-if="site.metrics.groups.length > 0"
                    variant="ghost"
                    class="h-8 px-2 text-xs text-primary hover:text-primary hover:bg-primary/10"
                    @click="openGroupsModal(site)"
                  >
                    {{ t('admin.upstream.fields.availableGroups') }} · {{ t('admin.upstream.fields.connected') }} {{ connectionsLoaded ? connectedGroupCount(site) : '-' }}/{{ site.metrics.groups.length }}
                  </Button>
                  <Tooltip :text="syncingSiteIds.has(site.id) ? t('admin.upstream.action.syncing') : t('admin.upstream.action.sync')">
                    <button
                      class="p-1.5 rounded-md text-muted-foreground hover:bg-primary/10 hover:text-primary transition-colors"
                      :disabled="syncingSiteIds.has(site.id)"
                      @click="refreshSingleSite(site.id)"
                    >
                      <Loader2 v-if="syncingSiteIds.has(site.id)" class="w-4 h-4 animate-spin" />
                      <RefreshCw v-else class="w-4 h-4" />
                    </button>
                  </Tooltip>
                  <Tooltip :text="site.canRelogin ? (reloggingSiteIds.has(site.id) ? t('admin.upstream.action.relogging') : t('admin.upstream.action.relogin')) : t('admin.upstream.action.reloginUnavailable')">
                    <button
                      :class="['p-1.5 rounded-md transition-colors disabled:cursor-not-allowed disabled:opacity-40', site.status === 'error' && site.canRelogin ? 'bg-warning/10 text-warning hover:bg-warning/20' : 'text-muted-foreground hover:bg-primary/10 hover:text-primary']"
                      :disabled="!site.canRelogin || syncingSiteIds.has(site.id) || reloggingSiteIds.has(site.id)"
                      @click="reloginSingleSite(site.id)"
                    >
                      <Loader2 v-if="reloggingSiteIds.has(site.id)" class="w-4 h-4 animate-spin" />
                      <LogIn v-else class="w-4 h-4" />
                    </button>
                  </Tooltip>
                  <Tooltip :text="t('admin.upstream.siteSettings.title')">
                    <button
                      class="p-1.5 rounded-md text-muted-foreground hover:bg-primary/10 hover:text-primary transition-colors"
                      @click="openSiteSettings(site)"
                    >
                      <Settings2 class="w-4 h-4" />
                    </button>
                  </Tooltip>
                  <Tooltip text="设置白名单"><button type="button" aria-label="设置白名单" class="rounded-md p-1.5 text-cyan-700 hover:bg-cyan-500/10 dark:text-cyan-300" @click="whitelistSite = site"><Shield class="h-4 w-4" /></button></Tooltip>
                  <Tooltip text="一键轮换 Key"><button type="button" aria-label="一键轮换 Key" class="rounded-md p-1.5 text-orange-700 hover:bg-orange-500/10 disabled:opacity-50 dark:text-orange-300" :disabled="!!rotatingSite || !connectionIdsForSite(site.id).length" @click="rotateSite(site)"><Loader2 v-if="rotatingSite === site.id" class="h-4 w-4 animate-spin" /><KeyRound v-else class="h-4 w-4" /></button></Tooltip>
                  <Tooltip :text="t('admin.upstream.action.edit')">
                    <button
                      class="p-1.5 rounded-md text-muted-foreground hover:bg-primary/10 hover:text-primary transition-colors"
                      aria-label="编辑上游" @click="handleEditSite(site)"
                    >
                      <Edit2 class="w-4 h-4" />
                    </button>
                  </Tooltip>
                  <Tooltip :text="t('admin.upstream.delete.action')">
                    <button
                      class="p-1.5 rounded-md text-muted-foreground hover:bg-red-500/10 hover:text-red-400 transition-colors"
                      @click="requestDeleteSite(site.id)"
                    >
                      <Trash2 class="w-4 h-4" />
                    </button>
                  </Tooltip>
                </div>
              </td>
            </tr>
            <tr v-if="filteredSites.length === 0">
              <td colspan="7" class="px-6 py-12 text-center text-muted-foreground">
                {{ t('admin.upstream.empty.description') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Empty State -->
    <div v-if="filteredSites.length === 0" class="flex flex-col items-center justify-center py-12 text-center border border-dashed border-border/60 rounded-2xl bg-surface/30">
      <div class="w-12 h-12 rounded-full bg-muted/50 flex items-center justify-center mb-4">
        <Search class="w-6 h-6 text-muted-foreground" />
      </div>
      <p class="text-foreground font-medium">{{ t('admin.upstream.empty.title') }}</p>
      
    </div>

    <!-- Delete Confirm Modal -->
    <div v-if="deletingSite" class="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div class="absolute inset-0 bg-background/80 backdrop-blur-sm" @click="cancelDeleteSite" />
      <div class="relative w-full max-w-md overflow-hidden rounded-2xl border border-border/70 bg-card p-6 shadow-2xl">
        <div class="absolute inset-x-0 top-0 h-1 bg-gradient-to-r from-red-500 via-warning to-red-500" />
        <div class="flex items-start gap-4">
          <div class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border border-red-500/30 bg-red-500/10 text-red-400">
            <Trash2 class="h-5 w-5" />
          </div>
          <div class="min-w-0 flex-1">
            <h3 class="text-lg font-semibold text-foreground">{{ t('admin.upstream.delete.title') }}</h3>
            
          </div>
        </div>

        <div v-if="deleteErrorKey" class="mt-5 flex items-start gap-2 rounded-xl border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">
          <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
          <span>{{ t(deleteErrorKey) }}</span>
        </div>

        <div class="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
          <Button type="button" variant="secondary" @click="cancelDeleteSite">
            {{ t('admin.upstream.delete.cancel') }}
          </Button>
          <Button type="button" class="bg-red-500 text-white hover:bg-red-400" @click="confirmDeleteSite">
            {{ t('admin.upstream.delete.confirm') }}
          </Button>
        </div>
      </div>
    </div>

    <Teleport defer to="body">
      <div v-if="isRefreshSettingsOpen" class="fixed inset-0 z-[100] flex items-center justify-center p-4">
        <div class="absolute inset-0 bg-background/80 backdrop-blur-sm" @click="isRefreshSettingsOpen = false" />
        <div class="relative w-full max-w-sm rounded-2xl border border-border/70 bg-card p-5 shadow-2xl">
          <div class="flex items-center justify-between gap-4">
            <h3 class="text-base font-semibold text-foreground">{{ t('admin.upstream.refresh.settings') }}</h3>
            <button type="button" class="rounded-md p-1 text-muted-foreground transition-colors hover:bg-surface-elevated hover:text-foreground" @click="isRefreshSettingsOpen = false">
              <X class="h-5 w-5" />
            </button>
          </div>
          <div class="mt-5 flex items-center justify-between gap-4">
            <span class="text-sm text-foreground">{{ t('admin.upstream.refresh.auto') }}</span>
            <label class="relative inline-flex cursor-pointer items-center">
              <input v-model="refreshSettingsEnabled" type="checkbox" class="peer sr-only">
              <span class="h-6 w-11 rounded-full bg-surface-elevated transition-colors after:absolute after:left-0.5 after:top-0.5 after:h-5 after:w-5 after:rounded-full after:border after:border-border after:bg-white after:transition-transform after:content-[''] peer-checked:bg-primary peer-checked:after:translate-x-5 peer-focus-visible:ring-2 peer-focus-visible:ring-primary" />
            </label>
          </div>
          <div class="mt-4">
            <label class="mb-2 block text-sm text-foreground" for="upstream-refresh-interval">{{ t('admin.upstream.refresh.interval') }}</label>
            <div class="flex items-center gap-2">
              <Input id="upstream-refresh-interval" v-model="refreshSettingsInterval" :disabled="!refreshSettingsEnabled" type="number" :min="minimumRefreshInterval" step="10" class="h-10" />
              <span class="shrink-0 text-sm text-muted-foreground">{{ t('admin.upstream.refresh.seconds') }}</span>
            </div>
          </div>
          <p v-if="refreshSettingsError" class="mt-3 text-sm text-destructive">{{ t(refreshSettingsError) }}</p>
          <div class="mt-6 flex justify-end gap-2">
            <Button type="button" variant="secondary" @click="isRefreshSettingsOpen = false">{{ t('admin.upstream.refresh.cancel') }}</Button>
            <Button type="button" :disabled="isSavingRefreshSettings" @click="saveRefreshSettings">
              <Loader2 v-if="isSavingRefreshSettings" class="h-4 w-4 animate-spin" />
              {{ t('admin.upstream.refresh.save') }}
            </Button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Groups Modal -->
    <Teleport defer to="body">
      <div v-if="isGroupsModalOpen" class="fixed inset-0 z-[100] flex items-center justify-center p-4 sm:p-0">
        <!-- Backdrop -->
        <div
          class="absolute inset-0 bg-background/80 backdrop-blur-sm"
          @click="closeGroupsModal"
        ></div>

        <!-- Modal Content -->
        <div role="dialog" aria-modal="true" aria-label="查看可用分组" class="relative bg-card border border-border/60 rounded-[2rem] shadow-2xl w-full max-w-4xl overflow-hidden animate-in fade-in zoom-in-95 duration-200">
          <div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-primary via-accent to-primary" />

          <div class="flex shrink-0 items-center justify-between px-6 py-5 border-b border-border/40">
            <h3 class="text-lg font-semibold text-foreground">
              {{ t('admin.upstream.fields.availableGroups') }}
              <span class="text-muted-foreground ml-2 text-sm font-medium">{{ selectedSiteForGroups?.name }}</span>
            </h3>
            <button type="button" aria-label="关闭可用分组" @click="closeGroupsModal" class="text-muted-foreground hover:text-foreground transition-colors p-1 rounded-md hover:bg-surface-elevated">
              <X class="w-5 h-5" />
            </button>
          </div>

          <div class="px-5 py-3 border-b border-border/40 text-sm text-muted-foreground">上游模型状态</div>
          <div class="p-5 max-h-[60vh] overflow-y-auto space-y-5">

            <p v-if="connectionsError" class="text-sm text-destructive">{{ connectionsError }}</p>
            <p v-if="!selectedSiteForGroups?.metrics.groups.length" class="text-sm text-muted-foreground">{{ t('admin.groupRates.empty.title') }}</p>
            <div v-for="(groups, platform) in groupedGroups" :key="platform" class="space-y-3">
              <h4 class="text-sm font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-2">
                <div class="w-1.5 h-1.5 rounded-full bg-primary"></div>
                {{ platform }}
              </h4>
              <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                <div
                  v-for="group in groups"
                  :key="group.id || group.name"
                  :class="[
                    'flex min-w-0 flex-col items-stretch gap-1 rounded-xl border p-3 text-left transition-colors group',
                    groupConnection(selectedSiteForGroups!, group)
                      ? 'border-signal/50 bg-signal/10 hover:bg-signal/15'
                      : 'border-border/60 bg-surface/50 hover:bg-surface hover:border-primary/50'
                  ]"
                >
                  <span class="text-sm font-medium text-foreground truncate w-full group-hover:text-primary transition-colors">{{ group.name }}</span>

                  <span class="mt-1 text-[10px] font-semibold" :class="groupConnection(selectedSiteForGroups!, group) ? 'text-signal' : 'text-muted-foreground'">
                    {{ !connectionsLoaded ? '—' : groupConnection(selectedSiteForGroups!, group) ? `已对接 ${groupConnections(selectedSiteForGroups!, group).length} 条` : t('admin.upstream.fields.disconnected') }}
                  </span>
                  <span
                    v-if="group.multiplier !== null && selectedSiteForGroups && selectedSiteForGroups.rechargeRate > 0"
                    class="mt-2 text-xs font-semibold text-primary px-2 py-0.5 rounded-md bg-primary/10 border border-primary/20"
                  >
                    {{ (group.multiplier * selectedSiteForGroups.rechargeRate).toFixed(2) }}
                  </span>
                  <template v-if="group.hasDedicatedMultiplier">
                    <Tooltip :text="t('admin.upstream.fields.dedicatedMultiplierTooltip')" wide>
                      <span class="text-[10px] text-muted-foreground mt-1">
                        {{ group.defaultMultiplierDisplay }} -&gt; {{ group.dedicatedMultiplierDisplay }}
                      </span>
                    </Tooltip>
                    <span class="mt-1 text-[9px] font-semibold text-accent px-1.5 py-0.5 rounded bg-accent/10 border border-accent/20">
                      {{ t('admin.upstream.fields.dedicatedMultiplierBadge') }}
                    </span>
                  </template>

                  <GroupModelStatus upstream-only v-if="selectedSiteForGroups" :site-id="selectedSiteForGroups.id" :group-id="group.id" :group-name="group.name" :multiplier="group.multiplier==null?null:group.multiplier*(selectedSiteForGroups?.rechargeRate??1)" :rule-id="groupConnection(selectedSiteForGroups!,group)?.id" compact summary class="mt-3 w-full text-left" />
                  <GroupKeyPolicy v-if="selectedSiteForGroups" :site-id="selectedSiteForGroups.id" :group-id="group.id" :group-name="group.name" />
                  <button
                    type="button"
                    :aria-label="`${group.name} · ${groupConnection(selectedSiteForGroups!,group) ? '管理对接' : '对接分组'}`"
                    :disabled="!connectionsLoaded"
                    class="mt-1 rounded-md border border-primary/20 px-2 py-1.5 text-left text-xs font-medium text-primary hover:bg-primary/10 disabled:opacity-50"
                    @click="selectedSiteForGroups && openGroupAction(selectedSiteForGroups, group)"
                  >{{ groupConnection(selectedSiteForGroups!,group) ? '管理对接' : '对接分组' }} ›</button>
                </div>
              </div>
            </div>
          </div>

          <div class="p-4 border-t border-border/40 flex justify-between">
             <Button variant="ghost" @click="closeGroupsModal">{{ t('admin.upstream.fields.closeGroupsModal') }}</Button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Direct group connection modal -->
    <Teleport defer to="body">
      <div v-if="isGroupActionOpen" class="fixed inset-0 z-[110] flex items-center justify-center p-4">
        <div class="absolute inset-0 bg-background/80 backdrop-blur-sm" @click="closeGroupAction" />
        <div role="dialog" aria-modal="true" aria-label="分组绑定设置" class="relative max-h-[90vh] w-full max-w-6xl overflow-y-auto rounded-2xl border border-border/70 bg-card p-5 shadow-2xl">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h3 class="text-base font-semibold text-foreground">{{ groupActionGroup?.name }}</h3>
              <p class="mt-1 text-xs text-muted-foreground">
                {{ groupActionSite?.name }} · {{ groupActionType || t('admin.upstream.fields.unknownPlatform') }}
              </p>
            </div>
            <button type="button" class="rounded-md p-1 text-muted-foreground hover:bg-surface-elevated hover:text-foreground" @click="closeGroupAction">
              <X class="h-5 w-5" />
            </button>
          </div>
          <div v-if="groupActionError" class="mt-4 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">{{ groupActionError }}</div>
          <GroupBindingList class="mt-4" :connections="chosenGroupConnections" :own-groups="ownGroups" :selected-id="groupActionConnectionID" :can-add="adminPlatform === 'sub2api'" :disabled="isGroupActionLoading || groupOptionsLoading" @select="chooseGroupConnection" />
          <div class="mt-4 grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
          <div class="min-w-0">
          <GroupAPIAddress v-if="groupActionSite && groupActionGroup" :site-id="groupActionSite.id" :group-id="groupActionGroup.id" :account-api-url="actionConnection?.upstreamApiUrl" />
          <PrimaryModelPicker v-if="groupActionSite && groupActionGroup" :key="`${groupActionSite.id}:${groupActionGroup.id}`" v-model="groupActionTestModel" class="mt-4" :site-id="groupActionSite.id" :group-id="groupActionGroup.id" :connection-id="actionConnection?.id" :disabled="isGroupActionLoading" />
          <label class="mt-4 flex cursor-pointer items-center justify-between gap-3 rounded-lg border border-border/50 bg-surface px-4 py-3 text-sm"><span>倍率大于等于分组时仍允许启用</span><input v-model="groupActionAllowHigherRate" type="checkbox" :disabled="isGroupActionLoading" class="h-4 w-4 shrink-0 accent-blue-600" /></label>
          <template v-if="actionConnection">
            <div class="mt-4 rounded-xl border border-border/60 p-3">
              <template v-if="adminPlatform === 'newapi'">
                <h4 class="text-sm font-medium">自有分组</h4>
                <div class="mt-2 max-h-48 space-y-2 overflow-auto">
                  <label v-for="ownGroup in compatibleOwnGroups" :key="ownGroup.id" class="flex items-center justify-between rounded border border-border/50 px-3 py-2 text-sm">
                    <span>{{ ownGroup.groupName }} · {{ ownGroup.platform }}</span>
                    <input v-model="groupActionOwnGroupIds" type="checkbox" :value="ownGroup.id" :disabled="isGroupActionLoading || groupOptionsLoading" />
                  </label>
                </div>
              </template>
              <Button :class="adminPlatform === 'newapi' ? 'mt-3' : ''" :disabled="!adminPlatform || isGroupActionLoading || groupOptionsLoading || !groupActionOwnGroupIds.length" @click="saveGroupBinding">保存绑定设置</Button>
            </div>
            <label class="mt-4 flex gap-2 text-sm"><input v-model="disconnectMode" type="radio" value="unlink" :disabled="isGroupActionLoading" />{{ t('admin.groupRates.disconnect.unlinkOnly') }}</label>
            <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.groupRates.disconnect.unlinkOnlyHint') }}</p>
            <label class="mt-3 flex gap-2 text-sm"><input v-model="disconnectMode" type="radio" value="full" :disabled="isGroupActionLoading" />{{ t('admin.groupRates.disconnect.deleteAll') }}</label>
            <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.groupRates.disconnect.deleteAllHint') }}</p>
            <div class="mt-5 flex justify-end gap-2">
              <Button variant="secondary" :disabled="isGroupActionLoading" @click="closeGroupAction">{{ t('admin.groupRates.actions.cancel') }}</Button>
              <Button variant="destructive" :disabled="isGroupActionLoading || groupOptionsLoading" @click="disconnectGroupAction">
                <Loader2 v-if="isGroupActionLoading" class="h-4 w-4 animate-spin" />
                {{ t('admin.groupRates.disconnect.action') }}
              </Button>
            </div>
          </template>
          <template v-else>
            <label v-if="groupActionSite && groupActionGroup && adminPlatform !== 'newapi' && (!groupActionType || groupActionType.includes(','))" class="mt-4 block text-sm">
              {{ t('admin.groupRates.connect.groupTypeLabel') }}
              <select v-model="groupActionType" :disabled="isGroupActionLoading" class="mt-2 h-9 w-full rounded-lg border border-border bg-surface px-2">
                <option value="">{{ t('admin.groupRates.connect.groupTypePlaceholder') }}</option>
                <option v-for="type in dynamicGroupTypes" :key="type" :value="type">{{ type }}</option>
              </select>
            </label>
            <label v-if="adminPlatform === 'newapi'" class="mt-4 block text-sm">
              {{ t('admin.groupRates.connect.channelTypeLabel') }}
              <select v-model="groupChannelType" :disabled="isGroupActionLoading" class="mt-2 h-9 w-full rounded-lg border border-border bg-surface px-2">
                <option :value="0">{{ t('admin.groupRates.connect.channelTypePlaceholder') }}</option>
                <option v-for="type in NEW_API_CHANNEL_TYPES" :key="type.id" :value="type.id">{{ type.name }}</option>
              </select>
            </label>
            <h4 class="mt-5 text-sm font-medium">{{ adminPlatform === 'sub2api' ? '选择要新增对接的自有分组' : t('admin.groupRates.connect.ownGroupLabel') }}</h4>
            <p v-if="adminPlatform === 'sub2api'" class="mt-1 text-xs text-muted-foreground">每个选中的分组会创建独立的上游 Key 和 Sub2API 转发账号。</p>
            <Loader2 v-if="groupOptionsLoading" class="mt-3 h-5 w-5 animate-spin text-primary" />
            <p v-else-if="!compatibleOwnGroups.length" class="mt-3 text-sm text-muted-foreground">{{ t('admin.groupRateCampaigns.editor.groupsEmpty') }}</p>
            <p v-else-if="!unusedOwnGroups.length" class="mt-3 text-sm text-muted-foreground">所有可用的自有分组均已绑定。</p>
            <input v-else v-model="groupActionSearch" aria-label="搜索可绑定分组" placeholder="搜索可绑定分组" class="mt-3 h-9 w-full rounded-lg border border-border/50 bg-surface px-3 text-sm" />
            <p v-if="unusedOwnGroups.length && !visibleOwnGroups.length" class="mt-3 text-sm text-muted-foreground">没有匹配的分组</p>
            <div class="mt-3 max-h-56 space-y-2 overflow-y-auto">
              <label
                v-for="ownGroup in visibleOwnGroups"
                :key="ownGroup.id"
                :class="[
                  'flex w-full items-center justify-between rounded-lg border px-3 py-2 text-left text-sm transition-colors',
                  groupActionOwnGroupIds.includes(ownGroup.id) ? 'border-primary bg-primary/10 text-primary' : 'border-border/60 bg-surface text-foreground hover:border-primary/50'
                ]"
              >
                <span>{{ ownGroup.groupName }} <span class="text-xs text-muted-foreground">{{ ownGroup.platform }}</span></span>
                <input v-model="groupActionOwnGroupIds" type="checkbox" :value="ownGroup.id" :disabled="isGroupActionLoading || groupOptionsLoading" class="h-4 w-4 accent-primary" />
              </label>
            </div>
            <div class="mt-5 flex justify-end gap-2">
              <Button variant="secondary" :disabled="isGroupActionLoading" @click="closeGroupAction">{{ t('admin.groupRates.actions.cancel') }}</Button>
              <Button :disabled="isGroupActionLoading || !canCreateGroupBinding" @click="submitGroupAction">
                <Loader2 v-if="isGroupActionLoading" class="h-4 w-4 animate-spin" />
                {{ adminPlatform === 'sub2api' ? '创建 Key 并对接' : t('admin.groupRates.actions.connect') }}
              </Button>
            </div>
          </template>
          </div>
          <GroupModelStatus v-if="groupActionSite && groupActionGroup" inline selectable upstream-only summary class="min-w-0 border-t border-border/50 pt-4 lg:border-l lg:border-t-0 lg:pl-5 lg:pt-0" :site-id="groupActionSite.id" :group-id="groupActionGroup.id" :group-name="groupActionGroup.name" :multiplier="groupActionGroup.multiplier == null ? null : groupActionGroup.multiplier * (groupActionSite.rechargeRate ?? 1)" :rule-id="actionConnection?.id" @select-model="groupActionTestModel = $event" />
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Add Site Modal -->
    <Teleport defer to="body">
      <div v-if="isAddModalOpen" class="fixed inset-0 z-[100] flex items-center justify-center p-4 sm:p-0">
        <!-- Backdrop -->
        <div
          class="absolute inset-0 bg-background/80 backdrop-blur-sm"
          @click="closeSiteModal"
        ></div>

        <!-- Modal Content -->
        <div class="relative bg-card border border-border/60 rounded-[2rem] shadow-2xl flex max-h-[calc(100dvh-2rem)] w-full max-w-2xl flex-col overflow-hidden animate-in fade-in zoom-in-95 duration-200">
          <div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-primary via-accent to-primary" />

          <div class="flex shrink-0 items-center justify-between px-6 py-5 border-b border-border/40">
            <h3 class="text-lg font-semibold text-foreground">
              {{ t(editingSiteId ? 'admin.upstream.modal.editTitle' : 'admin.upstream.modal.title') }}
            </h3>
            <button @click="closeSiteModal" class="text-muted-foreground hover:text-foreground transition-colors p-1 rounded-md hover:bg-surface-elevated">
              <X class="w-5 h-5" />
            </button>
          </div>

          <form @submit.prevent="handleAddSite" class="min-h-0 overflow-y-auto p-6">
            <div v-if="addErrorKey" class="flex items-start gap-2 rounded-xl border border-warning/20 bg-warning/10 px-3 py-2 text-sm text-warning mb-5">
              <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
              <span>{{ t(addErrorKey) }}</span>
            </div>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-5">
              <!-- Site Name -->
              <div class="space-y-2">
                <label class="text-sm font-medium text-foreground flex items-center gap-1">
                  <span class="text-red-500">*</span>
                  {{ t('admin.upstream.modal.form.siteName') }}
                </label>
                <Input
                  v-model="newSiteForm.name"
                  :placeholder="t('admin.upstream.modal.form.siteNamePlaceholder')"
                  :disabled="isAdding"
                  required
                  class="bg-surface border-border/50 focus:border-primary h-10"
                />
              </div>

              <!-- Platform Select -->
              <div class="space-y-2">
                <label class="text-sm font-medium text-foreground flex items-center gap-1">
                  <span class="text-red-500">*</span>
                  {{ t('admin.upstream.modal.form.platform') }}
                </label>
                <div class="relative">
                  <select
                    v-model="newSiteForm.platform"
                    :disabled="isAdding"
                    class="w-full h-10 px-3 rounded-xl bg-surface border border-border/50 focus:border-primary focus:ring-1 focus:ring-primary outline-none appearance-none text-sm text-foreground transition-all"
                  >
                    <option value="auto">{{ t('admin.upstream.modal.form.platforms.auto') }}</option>
                    <option value="sub2api">{{ t('admin.upstream.modal.form.platforms.sub2api') }}</option>
                    <option value="newapi">{{ t('admin.upstream.modal.form.platforms.newapi') }}</option>
                  </select>
                  <!-- Custom arrow since we removed appearance -->
                  <div class="absolute right-3 top-1/2 -translate-y-1/2 pointer-events-none text-muted-foreground">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>
                  </div>
                </div>
              </div>

              <!-- Site URL -->
              <div class="space-y-2 sm:col-span-2">
                <label class="text-sm font-medium text-foreground flex items-center gap-1">
                  <span class="text-red-500">*</span>
                  管理地址
                </label>
                <Input
                  v-model="newSiteForm.siteUrl"
                  type="url"
                  :placeholder="t('admin.upstream.modal.form.siteUrlPlaceholder')"
                  :disabled="isAdding"
                  required
                  class="bg-surface border-border/50 focus:border-primary h-10"
                />
              </div>
              <div class="space-y-2">
                <label for="site-default-api-url" class="text-sm font-medium text-foreground">API 地址</label>
                <input id="site-default-api-url" v-model="newSiteForm.apiUrl" type="url" :placeholder="newSiteForm.siteUrl || '留空沿用管理地址'" class="h-10 w-full rounded-lg border border-border/60 bg-surface px-3 text-sm text-foreground" />
                <span class="text-xs text-muted-foreground">保存后自动同步已绑定账号，分组单独设置优先。</span>
              </div>

              <label class="sm:col-span-2 flex cursor-pointer items-start gap-3 rounded-xl border border-warning/30 bg-warning/5 p-3 text-sm">
                <input v-model="newSiteForm.skipTlsVerify" type="checkbox" :disabled="isAdding" class="mt-1 h-4 w-4 accent-warning" />
                <span class="space-y-1">
                  <span class="block font-medium text-foreground">{{ t('admin.upstream.modal.form.skipTlsVerify') }}</span>
                  
                </span>
              </label>

              <!-- Auth Mode -->
              <div class="space-y-2 sm:col-span-2">
                <label class="text-sm font-medium text-foreground flex items-center gap-1">
                  <span class="text-red-500">*</span>
                  {{ t('admin.upstream.modal.form.authMode') }}
                </label>
                <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <label class="flex cursor-pointer items-start gap-3 rounded-xl border border-border/50 bg-surface p-3 text-sm transition-colors hover:border-primary/50">
                    <input v-model="newSiteForm.authMode" type="radio" value="password" :disabled="isAdding" class="mt-1" />
                    <span class="space-y-1">
                      <span class="block font-medium text-foreground">{{ t('admin.upstream.modal.form.authModes.password') }}</span>
                      
                    </span>
                  </label>
                  <label class="flex cursor-pointer items-start gap-3 rounded-xl border border-border/50 bg-surface p-3 text-sm transition-colors hover:border-primary/50">
                    <input v-model="newSiteForm.authMode" type="radio" value="token" :disabled="isAdding" class="mt-1" />
                    <span class="space-y-1">
                      <span class="block font-medium text-foreground">{{ t(newSiteForm.platform === 'newapi' ? 'admin.upstream.modal.form.authModes.newApiToken' : 'admin.upstream.modal.form.authModes.token') }}</span>
                      <span class="block text-xs leading-5 text-muted-foreground">{{ t(newSiteForm.platform === 'newapi' ? 'admin.upstream.modal.form.authModes.newApiTokenHelp' : 'admin.upstream.modal.form.authModes.tokenHelp') }}</span>
                    </span>
                  </label>
                </div>
              </div>

              <!-- Account -->
              <div v-if="newSiteForm.authMode === 'password' || newSiteForm.platform === 'newapi'" class="space-y-2">
                <label class="text-sm font-medium text-foreground flex items-center gap-1">
                  <span class="text-red-500">*</span>
                  {{ t(newSiteForm.authMode === 'token' ? 'admin.upstream.modal.form.newApiUserId' : 'admin.upstream.modal.form.account') }}
                </label>
                <Input
                  v-model="newSiteForm.account"
                  :placeholder="t(newSiteForm.authMode === 'token' ? 'admin.upstream.modal.form.newApiUserIdPlaceholder' : 'admin.upstream.modal.form.accountPlaceholder')"
                  :disabled="isAdding"
                  required
                  class="bg-surface border-border/50 focus:border-primary h-10"
                />
              </div>

              <!-- Password -->
              <div v-if="newSiteForm.authMode === 'password'" class="space-y-2">
                <label class="text-sm font-medium text-foreground flex items-center gap-1">
                  <span v-if="!editingSiteId" class="text-red-500">*</span>
                  {{ t('admin.upstream.modal.form.password') }}
                </label>
                <Input
                  v-model="newSiteForm.password"
                  type="password"
                  :placeholder="t(editingSiteId ? 'admin.upstream.modal.form.passwordEditPlaceholder' : 'admin.upstream.modal.form.passwordPlaceholder')"
                  :disabled="isAdding"
                  :required="!editingSiteId"
                  class="bg-surface border-border/50 focus:border-primary h-10"
                />
                
                <label v-if="newSiteForm.platform !== 'newapi'" class="mt-3 block text-xs text-muted-foreground">
                  {{ t('admin.upstream.modal.form.loginAgreementRevision') }}
                  <Input :model-value="newSiteForm.loginAgreementRevision || ''" @update:model-value="newSiteForm.loginAgreementRevision = $event" :disabled="isAdding" class="mt-1 h-9" />
                </label>
              </div>

              <template v-else>
                <div class="space-y-2 sm:col-span-2">
                  <label class="text-sm font-medium text-foreground flex items-center gap-1">
                    <span v-if="newSiteForm.platform === 'newapi'" class="text-red-500">*</span>
                    {{ t(newSiteForm.platform === 'newapi' ? 'admin.upstream.modal.form.newApiAccessToken' : 'admin.upstream.modal.form.accessToken') }}
                  </label>
                  <Input
                    v-model="newSiteForm.accessToken"
                    :placeholder="t(newSiteForm.platform === 'newapi' ? 'admin.upstream.modal.form.newApiAccessTokenPlaceholder' : 'admin.upstream.modal.form.accessTokenPlaceholder')"
                    :disabled="isAdding"
                    :required="newSiteForm.platform === 'newapi'"
                    class="bg-surface border-border/50 focus:border-primary h-10"
                  />
                  <p v-if="newSiteForm.platform === 'newapi'" class="text-xs leading-5 text-muted-foreground">
                    {{ t('admin.upstream.modal.form.tokenHelpNewApi') }}
                  </p>
                </div>
                <div v-if="newSiteForm.platform !== 'newapi'" class="space-y-2">
                  <label class="text-sm font-medium text-foreground flex items-center gap-1">
                    {{ t('admin.upstream.modal.form.refreshToken') }}
                  </label>
                  <Input
                    v-model="newSiteForm.refreshToken"
                    :placeholder="t('admin.upstream.modal.form.refreshTokenPlaceholder')"
                    :disabled="isAdding"
                    class="bg-surface border-border/50 focus:border-primary h-10"
                  />
                </div>
                <div v-if="newSiteForm.platform !== 'newapi'" class="space-y-2">
                  <label class="text-sm font-medium text-foreground flex items-center gap-1">
                    {{ t('admin.upstream.modal.form.tokenType') }}
                  </label>
                  <Input
                    v-model="newSiteForm.tokenType"
                    :placeholder="t('admin.upstream.modal.form.tokenTypePlaceholder')"
                    :disabled="isAdding"
                    class="bg-surface border-border/50 focus:border-primary h-10"
                  />
                  
                </div>
              </template>

              <!-- Recharge Rate -->
              <div class="space-y-2">
                <label class="text-sm font-medium text-foreground flex items-center gap-1">
                  <span class="text-red-500">*</span>
                  {{ t('admin.upstream.modal.form.rechargeRate') }}
                </label>
                <input
                  v-model.number="newSiteForm.rechargeRate"
                  type="number"
                  min="0.000001"
                  step="0.000001"
                  :placeholder="t('admin.upstream.modal.form.rechargeRatePlaceholder')"
                  :disabled="isAdding"
                  required
                  class="w-full h-10 px-3 rounded-xl bg-surface border border-border/50 focus:border-primary focus:ring-1 focus:ring-primary outline-none text-sm text-foreground placeholder:text-muted-foreground transition-all disabled:cursor-not-allowed disabled:opacity-50"
                />
                
              </div>

              <!-- Remark -->
              <div class="space-y-2">
                <label class="text-sm font-medium text-foreground ml-2.5">
                  {{ t('admin.upstream.modal.form.remark') }}
                </label>
                <Input
                  v-model="newSiteForm.remark"
                  :placeholder="t('admin.upstream.modal.form.remarkPlaceholder')"
                  :disabled="isAdding"
                  class="bg-surface border-border/50 focus:border-primary h-10"
                />
              </div>
            </div>

            <label class="mt-4 flex items-center justify-between gap-3 rounded-lg border border-border/50 bg-surface px-4 py-3 text-sm"><span>启用 Key IP 白名单</span><input v-model="newSiteForm.keyWhitelistEnabled" type="checkbox" class="h-4 w-4 accent-blue-600" :disabled="isAdding||previewBusy" /></label>
            <MonitorProfileEditor v-model="newSiteForm.monitorProfile" :disabled="previewBusy" class="mt-4" />
            <p v-if="previewError" class="mt-3 text-sm text-red-500">{{ previewError }}</p>
            <ModelHealthExplorer v-if="preview" :preview-id="preview.id" :groups="preview.groups" class="mt-4" />
            
            <!-- Actions -->
            <div class="flex items-center justify-end gap-3 pt-4 border-t border-border/40 mt-6">
              <Button type="button" variant="ghost" :disabled="isAdding" @click="isAddModalOpen = false" class="hover:bg-surface-line">
                {{ t('admin.upstream.modal.cancel') }}
              </Button>
              <Button type="submit" :disabled="isAdding || previewBusy" class="bg-primary text-primary-foreground hover:bg-primary/90">
                <Loader2 v-if="isAdding" class="h-4 w-4 animate-spin" />
              {{ previewBusy ? '连接检查中…' : editingSiteId ? t('admin.upstream.modal.updateSubmit') : preview ? '确认添加上游' : '连接并预览模型状态' }}
            </Button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>

    <Teleport to="body"><div v-if="healthSite" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-6 backdrop-blur-sm" @click.self="healthSite=null"><div class="max-h-[90vh] w-full max-w-4xl overflow-auto rounded-xl bg-card p-5 shadow-xl"><div class="mb-3 flex justify-between"><h2 class="font-semibold">{{healthSite.name}}</h2><button type="button" @click="healthSite=null">关闭</button></div><ModelHealthExplorer :site-id="healthSite.id" :groups="healthSite.metrics.groups" /></div></div></Teleport>
    <SiteSettingsModal
      :open="isSiteSettingsOpen"
      :site="selectedSiteForSettings"
      @close="closeSiteSettings"
      @saved="onSiteSettingsSaved"
    />
  </div>
</template>
