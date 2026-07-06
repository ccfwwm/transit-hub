<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Activity, AlertTriangle, CheckCircle2, CircleSlash, Loader2, PauseCircle, Play, RefreshCw, Search, Settings2, ShieldAlert, WalletCards } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import {
  getChannelMonitorSummary,
  pauseChannelMonitorRule,
  resumeChannelMonitorRule,
  runChannelMonitorRule,
  updateChannelMonitorRule,
} from '../api/channelMonitor'
import type { ChannelMonitorChannel, ChannelMonitorStatus, UpdateChannelMonitorRuleRequest } from '../types/channelMonitor'

const { t, locale } = useI18n()

const isLoading = ref(false)
const isActionLoading = ref(false)
const errorKey = ref('')
const searchQuery = ref('')
const statusFilter = ref<'all' | ChannelMonitorStatus>('all')
const selectedGroup = ref('all')
const summary = ref({
  stats: { total: 0, available: 0, failed: 0, balancePaused: 0, manualPaused: 0, unsupported: 0 },
  groups: [],
  channels: [],
} as Awaited<ReturnType<typeof getChannelMonitorSummary>>)
const editingChannel = ref<ChannelMonitorChannel | null>(null)
const editForm = ref({ enabled: true, checkIntervalMinutes: 10, failureThreshold: 3, balanceThreshold: 1 })

const loadSummary = async () => {
  isLoading.value = true
  errorKey.value = ''
  try {
    summary.value = await getChannelMonitorSummary()
  } catch (error: any) {
    errorKey.value = error?.message ?? 'admin.channelMonitor.errors.request'
  } finally {
    isLoading.value = false
  }
}

onMounted(() => {
  void loadSummary()
})

const statCards = computed(() => [
  { key: 'total', value: summary.value.stats.total, icon: Activity, tone: 'text-primary bg-primary/10 border-primary/20' },
  { key: 'available', value: summary.value.stats.available, icon: CheckCircle2, tone: 'text-emerald-600 bg-emerald-500/10 border-emerald-500/20' },
  { key: 'failed', value: summary.value.stats.failed, icon: AlertTriangle, tone: 'text-red-600 bg-red-500/10 border-red-500/20' },
  { key: 'balancePaused', value: summary.value.stats.balancePaused, icon: WalletCards, tone: 'text-amber-600 bg-amber-500/10 border-amber-500/20' },
  { key: 'manualPaused', value: summary.value.stats.manualPaused, icon: PauseCircle, tone: 'text-sky-600 bg-sky-500/10 border-sky-500/20' },
])

const groupOptions = computed(() => ['all', ...summary.value.groups.map(group => group.groupName)])

const filteredChannels = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return summary.value.channels.filter(channel => {
    const statusMatch = statusFilter.value === 'all' || channel.status === statusFilter.value
    const groupMatch = selectedGroup.value === 'all' || channel.ownGroups.includes(selectedGroup.value)
    const searchMatch = !query ||
      channel.siteName.toLowerCase().includes(query) ||
      channel.upstreamGroupName.toLowerCase().includes(query) ||
      channel.adminAccountName.toLowerCase().includes(query) ||
      channel.ownGroups.some(group => group.toLowerCase().includes(query))
    return statusMatch && groupMatch && searchMatch
  })
})

const statusLabel = (status: ChannelMonitorStatus): string => t(`admin.channelMonitor.status.${status}`)

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

const formatMoney = (value: number | null): string => {
  if (value === null || !Number.isFinite(value)) return t('admin.channelMonitor.common.unknown')
  return value.toFixed(2)
}

const runAction = async (action: () => Promise<unknown>) => {
  isActionLoading.value = true
  errorKey.value = ''
  try {
    await action()
    await loadSummary()
  } catch (error: any) {
    errorKey.value = error?.message ?? 'admin.channelMonitor.errors.request'
  } finally {
    isActionLoading.value = false
  }
}

const openEditor = (channel: ChannelMonitorChannel) => {
  editingChannel.value = channel
  editForm.value = {
    enabled: channel.enabled,
    checkIntervalMinutes: channel.checkIntervalMinutes,
    failureThreshold: channel.failureThreshold,
    balanceThreshold: channel.balanceThreshold,
  }
}

const closeEditor = () => {
  editingChannel.value = null
}

const saveEditor = async () => {
  if (!editingChannel.value) return
  const payload: UpdateChannelMonitorRuleRequest = {
    enabled: editForm.value.enabled,
    checkIntervalMinutes: Number(editForm.value.checkIntervalMinutes),
    failureThreshold: Number(editForm.value.failureThreshold),
    balanceThreshold: Number(editForm.value.balanceThreshold),
  }
  await runAction(() => updateChannelMonitorRule(editingChannel.value!.ruleId, payload))
  closeEditor()
}
</script>

<template>
  <div class="h-[calc(100vh-8rem)] flex flex-col gap-5">
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-5">
      <div v-for="card in statCards" :key="card.key" class="rounded-lg border border-border/50 bg-surface p-4">
        <div class="flex items-center justify-between gap-3">
          <div>
            <p class="text-xs font-medium text-muted-foreground">{{ t(`admin.channelMonitor.stats.${card.key}`) }}</p>
            <p class="mt-2 text-2xl font-semibold text-foreground">{{ card.value }}</p>
          </div>
          <div :class="['flex h-10 w-10 items-center justify-center rounded-lg border', card.tone]">
            <component :is="card.icon" class="h-5 w-5" />
          </div>
        </div>
      </div>
    </div>

    <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
      <div class="flex flex-1 flex-col gap-3 sm:flex-row">
        <div class="relative w-full sm:max-w-sm">
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
          <option value="manual_paused">{{ statusLabel('manual_paused') }}</option>
          <option value="unsupported">{{ statusLabel('unsupported') }}</option>
        </select>
      </div>
      <Button variant="secondary" class="h-10 gap-2 rounded-xl" :disabled="isLoading" @click="loadSummary">
        <RefreshCw :class="['h-4 w-4', isLoading ? 'animate-spin' : '']" />
        {{ t('admin.channelMonitor.actions.refresh') }}
      </Button>
    </div>

    <div v-if="errorKey" class="rounded-xl border border-warning/20 bg-warning/10 p-3 text-sm text-warning">
      {{ t(errorKey) }}
    </div>

    <div class="grid min-h-0 flex-1 grid-cols-1 gap-5 xl:grid-cols-[minmax(280px,360px)_1fr]">
      <section class="min-h-0 overflow-hidden rounded-lg border border-border/50 bg-surface">
        <div class="border-b border-border/50 px-5 py-4">
          <h2 class="text-sm font-semibold text-foreground">{{ t('admin.channelMonitor.groups.title') }}</h2>
          <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.channelMonitor.groups.subtitle') }}</p>
        </div>
        <div class="max-h-full overflow-auto">
          <table class="w-full text-left text-sm">
            <thead class="sticky top-0 bg-surface-elevated text-xs text-muted-foreground">
              <tr>
                <th class="px-4 py-3 font-medium">{{ t('admin.channelMonitor.groups.columns.group') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.channelMonitor.groups.columns.available') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.channelMonitor.groups.columns.last') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border/40">
              <tr v-for="group in summary.groups" :key="`${group.groupName}-${group.platform}`" class="hover:bg-surface-elevated/60">
                <td class="px-4 py-3">
                  <div class="font-medium text-foreground">{{ group.groupName }}</div>
                  <div class="text-xs text-muted-foreground">{{ group.platform || t('admin.channelMonitor.common.unknown') }}</div>
                </td>
                <td class="px-4 py-3 font-mono text-foreground">{{ group.available }}/{{ group.total }}</td>
                <td class="px-4 py-3 text-xs text-muted-foreground">{{ formatDateTime(group.lastCheckedAt) }}</td>
              </tr>
              <tr v-if="!isLoading && summary.groups.length === 0">
                <td colspan="3" class="px-4 py-10 text-center text-sm text-muted-foreground">{{ t('admin.channelMonitor.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="min-h-0 overflow-hidden rounded-lg border border-border/50 bg-surface">
        <div class="border-b border-border/50 px-5 py-4">
          <h2 class="text-sm font-semibold text-foreground">{{ t('admin.channelMonitor.channels.title') }}</h2>
          <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.channelMonitor.channels.subtitle', { count: filteredChannels.length }) }}</p>
        </div>

        <div v-if="isLoading" class="flex h-64 items-center justify-center text-muted-foreground">
          <Loader2 class="mr-2 h-5 w-5 animate-spin" />
          {{ t('admin.channelMonitor.loading') }}
        </div>

        <div v-else class="max-h-full overflow-auto">
          <table class="w-full min-w-[980px] text-left text-sm">
            <thead class="sticky top-0 bg-surface-elevated text-xs text-muted-foreground">
              <tr>
                <th class="px-5 py-3 font-medium">{{ t('admin.channelMonitor.channels.columns.channel') }}</th>
                <th class="px-5 py-3 font-medium">{{ t('admin.channelMonitor.channels.columns.group') }}</th>
                <th class="px-5 py-3 font-medium">{{ t('admin.channelMonitor.channels.columns.status') }}</th>
                <th class="px-5 py-3 font-medium">{{ t('admin.channelMonitor.channels.columns.balance') }}</th>
                <th class="px-5 py-3 font-medium">{{ t('admin.channelMonitor.channels.columns.last') }}</th>
                <th class="px-5 py-3 text-right font-medium">{{ t('admin.channelMonitor.channels.columns.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border/40">
              <tr v-for="channel in filteredChannels" :key="channel.connectionId" class="hover:bg-surface-elevated/60">
                <td class="px-5 py-4">
                  <div class="font-medium text-foreground">{{ channel.adminAccountName || channel.adminAccountId }}</div>
                  <div class="mt-1 text-xs text-muted-foreground">{{ channel.siteName }} · {{ channel.upstreamGroupName }}</div>
                </td>
                <td class="px-5 py-4">
                  <div class="flex flex-wrap gap-1.5">
                    <span v-for="group in channel.ownGroups" :key="group" class="rounded-md border border-border/50 bg-surface-elevated px-2 py-0.5 text-xs font-medium text-muted-foreground">
                      {{ group }}
                    </span>
                  </div>
                  <div class="mt-1 text-xs text-muted-foreground">{{ channel.groupType || t('admin.channelMonitor.common.unknown') }}</div>
                </td>
                <td class="px-5 py-4">
                  <span :class="['inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-semibold', statusClass(channel.status)]">
                    <component :is="statusIcon(channel.status)" class="h-3.5 w-3.5" />
                    {{ statusLabel(channel.status) }}
                  </span>
                  <div v-if="channel.consecutiveFailures > 0" class="mt-1 text-xs text-muted-foreground">
                    {{ t('admin.channelMonitor.channels.failures', { count: channel.consecutiveFailures }) }}
                  </div>
                </td>
                <td class="px-5 py-4">
                  <div class="font-mono text-foreground">{{ formatMoney(channel.balance) }}</div>
                  <div class="text-xs text-muted-foreground">{{ t('admin.channelMonitor.channels.threshold', { value: channel.balanceThreshold }) }}</div>
                </td>
                <td class="px-5 py-4">
                  <div class="text-xs text-muted-foreground">{{ formatDateTime(channel.lastCheckedAt) }}</div>
                  <div class="mt-1 max-w-[240px] truncate text-xs text-muted-foreground" :title="channel.lastMessage">{{ channel.lastMessage || '-' }}</div>
                </td>
                <td class="px-5 py-4">
                  <div class="flex justify-end gap-1.5">
                    <Button variant="secondary" size="sm" class="gap-1.5" :disabled="isActionLoading || !channel.supported" @click="runAction(() => runChannelMonitorRule(channel.ruleId))">
                      <RefreshCw class="h-3.5 w-3.5" />
                      {{ t('admin.channelMonitor.actions.run') }}
                    </Button>
                    <Button v-if="channel.manualPaused" size="sm" class="gap-1.5" :disabled="isActionLoading || !channel.supported" @click="runAction(() => resumeChannelMonitorRule(channel.ruleId))">
                      <Play class="h-3.5 w-3.5" />
                      {{ t('admin.channelMonitor.actions.resume') }}
                    </Button>
                    <Button v-else variant="secondary" size="sm" class="gap-1.5" :disabled="isActionLoading || !channel.supported" @click="runAction(() => pauseChannelMonitorRule(channel.ruleId))">
                      <PauseCircle class="h-3.5 w-3.5" />
                      {{ t('admin.channelMonitor.actions.pause') }}
                    </Button>
                    <Button variant="ghost" size="sm" :disabled="isActionLoading" @click="openEditor(channel)">
                      <Settings2 class="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </td>
              </tr>
              <tr v-if="filteredChannels.length === 0">
                <td colspan="6" class="px-5 py-16 text-center text-sm text-muted-foreground">{{ t('admin.channelMonitor.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>

    <div v-if="editingChannel" class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
      <div class="w-full max-w-md rounded-xl border border-border/50 bg-card p-6 shadow-xl">
        <h2 class="text-lg font-semibold text-foreground">{{ t('admin.channelMonitor.editor.title') }}</h2>
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
  </div>
</template>
