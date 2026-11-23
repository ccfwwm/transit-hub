<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import ModelTestDialog from '../ModelTestDialog.vue'
import { operationJSON } from '../../api/operations'
import { keyJobMessage, previewKeyRotation, rotateKeys } from '../../api/keyRotation'
import { realConnect, realDisconnect } from '../../api/mySites'
import type { KeyEntry, KeyInventory, KeyUsage, KeyUsageSummary } from '../../types/upstreamKeys'

const props = defineProps<{ entry: KeyEntry | null; inventory: KeyInventory; initialSite: string; startRotation?: boolean; overview?: KeyUsageSummary }>()
const emit = defineEmits<{ close: []; changed: []; migrate: [site: string, binding: string] }>()
const siteID = ref(props.entry?.siteId || props.initialSite || props.inventory.sites.find(site => !site.error)?.id || '')
const site = computed(() => props.inventory.sites.find(item => item.id === siteID.value))
const name = ref('')
const custom = ref('')
const createdKey = ref('')
const created = ref(false)
const config = ref<Record<string, any>>({ ...props.entry?.config })
const tab = ref(props.startRotation ? 'binding' : props.entry ? 'usage' : 'settings')
const interval = ref('day')
const start = ref('')
const end = ref('')
const usage = ref<KeyUsage | null>(null)
const error = ref('')
const message = ref('')
const busy = ref(false)
const testing = ref(false)
const confirmAction = ref('')
const confirmRotation = ref(Boolean(props.startRotation))
const rotationSubmitted = ref(false)
const connect = ref(false)
const selectedGroups = ref<string[]>([])
const type = ref('openai')
const base = computed(() => '/upstream-keys/' + encodeURIComponent(siteID.value) + (props.entry ? '/' + encodeURIComponent(props.entry.id) : ''))
const bound = computed(() => Boolean(props.entry?.bindings.length))
const currentGroup = computed(() => site.value?.groups?.find(group => group.id === props.entry?.groupId))
const fieldDefs = computed(() => {
  const common = site.value?.platform === 'newapi'
    ? [
        ['remain_quota','剩余额度','number'], ['unlimited_quota','不限额度','checkbox'],
        ['expired_time','到期时间（Unix 秒，-1 为不限）','number'], ['allow_ips','IP 白名单','text'],
        ['model_limits_enabled','启用模型白名单','checkbox'], ['model_limits','模型白名单（逗号分隔）','text'],
        ['remark','备注','text'],
      ]
    : [
        ['quota','总额度（美元，0 为不限）','number'], [props.entry ? 'expires_at' : 'expires_in_days',props.entry ? '到期时间（ISO 8601）' : '有效天数',props.entry ? 'text' : 'number'],
        ['ip_whitelist','IP 白名单','lines'], ['ip_blacklist','IP 黑名单','lines'], ['rate_limit_5h','5 小时限额','number'],
        ['rate_limit_1d','1 天限额','number'], ['rate_limit_7d','7 天限额','number'],
      ]
  if (site.value?.platform === 'newapi' && props.entry) {
    for (const [key, label, kind] of [
      ['group_config', '分组配置', 'json'], ['preset_id', '分组预设', 'number'], ['auto_groups', '自动分组', 'json'],
      ['sticky_session', '会话粘滞', 'checkbox'], ['cross_group_retry', '跨组重试', 'checkbox'], ['allow_user_quota_query', '允许查询额度', 'checkbox'],
    ]) if (key && (!bound.value || !['group_config','preset_id','auto_groups'].includes(key)) && Object.hasOwn(props.entry.config, key)) common.push([key,label!,kind!])
  }
  return common as [string, string, string][]
})
const changed = ref(new Set<string>())
function setField(key: string, value: unknown) {
  config.value[key] = value
  changed.value.add(key)
}
async function action(task: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try { await task() } catch (err) { error.value = err instanceof Error ? err.message : String(err) }
  finally { busy.value = false }
}
async function loadUsage() {
  if (!props.entry) return
  await action(async () => {
    const query = new URLSearchParams({ interval: interval.value })
    if (start.value || end.value) { query.set('start', start.value); query.set('end', end.value) }
    usage.value = await operationJSON<KeyUsage>(base.value + '/usage?' + query)
  })
}
async function loadConfig() {
  if (!props.entry || changed.value.size) return
  await action(async () => {
    const fresh = await operationJSON<KeyEntry>(base.value)
    config.value = { ...fresh.config }
  })
}
watch(tab, value => { if (value === 'usage' && !usage.value) void loadUsage(); if (value === 'settings') void loadConfig() }, { immediate: true })
watch(siteID, () => { if (!props.entry) { config.value = {}; changed.value.clear(); custom.value = '' } })
function fieldValue(key: string, kind: string) {
  const data = config.value[key]
  if (kind === 'lines' && Array.isArray(data)) return data.join('\n')
  if (kind === 'json' && typeof data === 'object') return JSON.stringify(data)
  return data ?? ''
}
function status(enabled: boolean) {
  setField('status', site.value?.platform === 'newapi' ? (enabled ? 1 : 2) : (enabled ? 'active' : 'disabled'))
}
async function save() {
  await action(async () => {
    const patch: Record<string, any> = {}
    for (const key of changed.value) {
      const kind = fieldDefs.value.find(field => field[0] === key)?.[2]
      const value = config.value[key]
      if (kind === 'number' && value === '') continue
      patch[key] = kind === 'lines' ? String(fieldValue(key, kind)).split(/[\s,]+/).filter(Boolean)
        : kind === 'json' && typeof value === 'string' ? JSON.parse(value) : kind === 'number' ? Number(value) : value
    }
    const result = await operationJSON<{ createdKey?: string }>(base.value, { method: props.entry ? 'PATCH' : 'POST', body: JSON.stringify({ ...(!props.entry ? { name: name.value, customKey: custom.value } : {}), config: patch }) })
    emit('changed')
    if (!props.entry) {
      custom.value = ''
      createdKey.value = result.createdKey || ''
      created.value = true
      message.value = createdKey.value ? '已创建。请保存本次显示的 Key。' : '已创建。上游未返回完整 Key。'
    } else { changed.value.clear(); message.value = '配置已保存' }
  })
}
const remove = (connection?: string) => action(async () => {
  if (connection) await realDisconnect({ connectionId: connection, mode: 'full' })
  else await operationJSON(base.value, { method: 'DELETE' })
  emit('changed')
  emit('close')
})
const bind = () => action(async () => {
  if (!props.entry || !selectedGroups.value.length) return
  await realConnect({
    upstreamSiteId: siteID.value, upstreamGroupId: props.entry.groupId, upstreamGroupName: currentGroup.value?.name || props.entry.groupName,
    groupType: currentGroup.value?.platform || type.value, ownGroupIds: selectedGroups.value,
  })
  emit('changed')
  message.value = '已为所选分组创建独立 Key'
  connect.value = false
})
const rotationGroups = computed(() => [...new Set(props.entry?.bindings.flatMap(binding => binding.groups) || [])])
async function rotate() {
  if (rotationSubmitted.value || !props.entry?.bindings.length) return
  const targets = { connectionIds: [...new Set(props.entry.bindings.map(binding => binding.id))] }
  await action(async () => {
    const preview = await previewKeyRotation(targets)
    if (preview.keys !== 1 || preview.skipped) throw new Error('绑定已变化，请关闭详情后刷新 Key 列表')
    rotationSubmitted.value = true
    message.value = '轮换任务进行中…'
    try {
      const job = await rotateKeys(targets, preview.version)
      confirmRotation.value = false
      emit('changed')
      if (job.failed || job.items.some(item => item.result?.skipped)) {
        message.value = ''
        throw new Error(keyJobMessage(job) || '轮换未完成，请在任务进度中查看')
      }
      message.value = 'Key 已轮换'
    } finally {
      if (confirmRotation.value) rotationSubmitted.value = false
    }
  })
}
const copyCreatedKey = () => action(async () => { await navigator.clipboard.writeText(createdKey.value); message.value = 'Key 已复制' })
const amount = (value: number | null | undefined) => value == null ? '—' : value.toLocaleString(undefined, { maximumFractionDigits: 8 })
const maxUsage = computed(() => Math.max(1, ...(usage.value?.trend.map(point => point.requests) || [])))
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[110] flex items-center justify-center bg-black/50 p-3" @click.self="!busy && emit('close')">
      <section role="dialog" aria-modal="true" aria-labelledby="key-detail-title" class="max-h-[92dvh] w-full max-w-3xl overflow-y-auto rounded-xl border border-border bg-card p-5 shadow-2xl">
        <header class="flex items-start justify-between gap-3"><h2 id="key-detail-title" class="break-all text-lg font-semibold">{{ entry?.name || '创建独立 Key' }}</h2><button aria-label="关闭" :disabled="busy" class="rounded-md p-1 hover:bg-muted" @click="emit('close')"><X class="size-5" /></button></header>
        <nav v-if="entry" aria-label="Key 详情" class="mt-4 flex flex-wrap gap-2 border-b border-border pb-3">
          <Button v-for="item in [['usage','用量'],['settings','配置'],['test','测试'],['binding','对接与维护']]" :key="item[0]" size="sm" :variant="tab === item[0] ? 'default' : 'ghost'" :disabled="busy" @click="tab = item[0]!">{{ item[1] }}</Button>
        </nav>
        <p v-if="error || message" :role="error ? 'alert' : 'status'" class="mt-3 whitespace-pre-line text-sm" :class="error ? 'text-destructive' : 'text-signal'">{{ error || message }}</p>
        <div v-if="created" class="mt-4 space-y-3">
          <input v-if="createdKey" :value="createdKey" readonly aria-label="完整 Key" @focus="($event.target as HTMLInputElement).select()" />
          <div class="flex gap-2"><Button v-if="createdKey" type="button" @click="copyCreatedKey">复制 Key</Button><Button variant="secondary" @click="emit('close')">完成</Button></div>
        </div>
        <form v-if="tab === 'settings' && !created" class="mt-4 grid gap-4 sm:grid-cols-2" @submit.prevent="save">
          <template v-if="!entry">
            <label>上游站点<select v-model="siteID" required><option disabled value="">请选择</option><option v-for="item in inventory.sites" :key="item.id" :disabled="Boolean(item.error)" :value="item.id">{{ item.name }}</option></select></label>
            <label>Key 名称<input v-model="name" required /></label>
            <label v-if="site?.platform === 'sub2api'" class="sm:col-span-2">自定义 Key 值（选填）<input v-model="custom" type="password" autocomplete="new-password" /></label>
          </template>
          <label v-if="site && !bound">上游分组<select :value="config.group_id ?? config.group ?? ''" @change="setField(site.platform === 'newapi' ? 'group' : 'group_id', site.platform === 'newapi' ? ($event.target as HTMLSelectElement).value : Number(($event.target as HTMLSelectElement).value))"><option value="" disabled>选择分组</option><option v-for="group in site.groups" :key="group.id" :value="group.id">{{ group.name }}</option></select></label>
          <label v-if="entry && !bound">状态<select :value="['active','1'].includes(String(config.status)) ? 'on' : 'off'" @change="status(($event.target as HTMLSelectElement).value === 'on')"><option value="on">启用</option><option value="off">停用</option></select></label>
          <label v-for="[key,label,kind] in fieldDefs" :key="key" :class="kind === 'checkbox' ? 'inline-flex items-center gap-2 self-center' : ''">
            <input v-if="kind === 'checkbox'" type="checkbox" :checked="Boolean(config[key])" @change="setField(key, ($event.target as HTMLInputElement).checked)" />
            {{ label }}
            <textarea v-if="kind === 'lines' || kind === 'json'" :value="fieldValue(key,kind)" rows="2" @input="setField(key, ($event.target as HTMLTextAreaElement).value)" />
            <input v-else-if="kind !== 'checkbox'" :type="kind" :step="kind === 'number' ? 'any' : undefined" :value="fieldValue(key,kind)" @input="setField(key, ($event.target as HTMLInputElement).value)" />
          </label>
          <div class="sm:col-span-2"><Button type="submit" :disabled="busy || !siteID">{{ busy ? '保存中…' : entry ? '保存配置' : '创建 Key' }}</Button></div>
        </form>
        <section v-if="tab === 'usage'" class="mt-4 space-y-4">
          <form class="flex flex-wrap items-end gap-2" @submit.prevent="loadUsage">
            <label>周期<select v-model="interval"><option value="hour">小时</option><option value="day">日</option><option value="week">周</option></select></label>
            <label>开始日期<input v-model="start" type="date" /></label><label>结束日期<input v-model="end" type="date" /></label>
            <Button type="submit" :disabled="busy">{{ busy ? '查询中…' : '查询用量' }}</Button>
          </form>
          <p v-if="entry?.bindings.some(v => v.sharedSince)" class="text-sm text-warning">拆分日前的用量来自原共享 Key。</p>
          <template v-if="usage">
            <p class="text-sm" :class="usage.state === 'complete' ? 'text-muted-foreground' : 'text-warning'">{{ { complete: '完整数据', partial: '部分数据', unavailable: '上游用量不可用' }[usage.state] }} · 北京时间 · {{ usage.unit === 'quota' ? 'New API 额度' : 'USD' }}</p>
            <div v-if="usage.state !== 'unavailable'" class="space-y-4">
              <p class="text-xl font-semibold tabular-nums">{{ usage.total.requests }} 次请求 · {{ amount(usage.total.amount) }}</p>
              <div class="max-h-60 space-y-1 overflow-auto" aria-label="用量趋势">
                <div v-for="point in usage.trend" :key="point.label" class="grid grid-cols-[9rem_1fr_auto] items-center gap-2 text-xs tabular-nums">
                  <span>{{ point.label }}</span><span class="bg-primary/30" :style="{ width: (point.requests / maxUsage) * 100 + '%', height: '0.5rem' }"></span><span>{{ point.requests || (usage.state === 'complete' ? 0 : '—') }} 次 · {{ amount(point.amount) }}</span>
                </div>
              </div>
              <details v-if="usage.models.length"><summary class="cursor-pointer text-sm font-medium">模型用量</summary><ul class="mt-2 space-y-1 text-sm"><li v-for="item in usage.models" :key="item.label">{{ item.label }} · {{ item.requests }} 次 · {{ amount(item.amount) }}</li></ul></details>
              <details v-if="usage.logs.length"><summary class="cursor-pointer text-sm font-medium">最近调用明细</summary><div class="mt-2 max-h-64 overflow-auto"><table class="w-full text-left text-xs"><thead><tr><th>时间</th><th>模型</th><th>IP</th><th>用量</th></tr></thead><tbody><tr v-for="(row,index) in usage.logs" :key="index" class="border-t border-border"><td>{{ new Date(row.time * 1000).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai'}) }}</td><td>{{ row.model }}</td><td>{{ row.ip || '—' }}</td><td>{{ amount(row.amount) }}</td></tr></tbody></table></div></details>
            </div>
          </template>
        </section>
        <section v-if="tab === 'test'" class="mt-4 space-y-3">
          <Button :disabled="busy" @click="testing = true">实际测试</Button>
        </section>
        <section v-if="tab === 'binding' && entry" class="mt-4 space-y-3">
          <div v-if="bound" class="space-y-3 rounded-lg border border-border bg-surface p-3">
            <Button v-if="!confirmRotation" variant="secondary" :disabled="busy || rotationSubmitted" @click="confirmRotation = true">轮换 Key</Button>
            <template v-else>
              <p class="font-medium">轮换 {{ entry.name }}</p>
              <p class="text-sm text-muted-foreground">自有分组 {{ rotationGroups.join(' / ') || '—' }}</p>
              <p class="text-sm text-muted-foreground">今日消费 {{ amount(overview?.today.amount) }} {{ overview?.today.amount == null ? '' : overview.unit === 'quota' ? '额度' : 'USD' }}</p>
              <p class="text-sm text-muted-foreground">新 Key 沿用现有的规范名称和编号。系统会等待已有媒体任务结束，并核验转发切换后再撤销旧 Key。</p>
              <div class="flex gap-2"><Button variant="secondary" :disabled="busy" @click="confirmRotation = false">取消</Button><Button :disabled="busy" @click="rotate">{{ busy ? '轮换中…' : '确认轮换' }}</Button></div>
            </template>
          </div>
          <div v-for="item in entry.bindings" :key="item.id" class="flex flex-wrap items-center justify-between gap-2 border-b border-border pb-2">
            <div class="break-all"><p class="font-medium">{{ item.account }}</p><p class="text-sm text-muted-foreground">{{ item.groups.join(' / ') }}</p></div>
            <div class="flex flex-wrap gap-2"><Button v-if="item.update" variant="secondary" @click="emit('migrate', siteID, item.id)">命名与拆分</Button><Button v-if="confirmAction === item.id" variant="destructive" :disabled="busy" @click="remove(item.id)">确认移除账号和 Key</Button><Button v-else variant="secondary" :disabled="busy" @click="confirmAction = item.id">解除对接</Button></div>
          </div>
          <div class="flex flex-wrap gap-2"><Button :disabled="busy || !entry.groupId" variant="secondary" @click="connect = !connect">对接该上游分组</Button>
            <template v-if="!bound"><Button v-if="confirmAction === 'delete'" variant="destructive" :disabled="busy" @click="remove()">确认删除独立 Key</Button><Button v-else variant="secondary" @click="confirmAction = 'delete'">删除 Key</Button></template>
          </div>
          <div v-if="connect" class="space-y-3 py-2">
            <p class="text-sm text-muted-foreground">为所选自有分组各建一套独立 Key 与转发账号。</p>
            <div class="grid gap-2 sm:grid-cols-2"><label v-for="item in inventory.ownGroups" :key="item.id" class="flex items-center gap-2"><input v-model="selectedGroups" type="checkbox" :value="item.id" />{{ item.name }}</label></div>
            <label v-if="!currentGroup?.platform">转发平台<input v-model="type" /></label>
            <Button :disabled="busy || !selectedGroups.length" @click="bind">创建独立对接</Button>
          </div>
        </section>
      </section>
    </div>
  </Teleport>
  <ModelTestDialog v-if="testing && entry" :site-id="entry.siteId" :group-id="entry.groupId" :key-id="entry.id" :label="entry.name" @close="testing = false" />
</template>

<style scoped>
label { @apply text-sm text-muted-foreground; }
input:not([type='checkbox']), select, textarea { @apply mt-1 block w-full rounded-md border border-border bg-background px-3 py-2 text-foreground; }
th, td { @apply p-2; }
</style>
