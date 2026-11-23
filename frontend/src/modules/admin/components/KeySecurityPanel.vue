<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { operationJSON, followJob, type OperationJob } from '../api/operations'
interface Policy { enabled: boolean; auditEnabled: boolean; auditIntervalMinutes: number; time: string; timezone: string; intervalHours: number; version: number; nextRotation: string }
interface SecurityEvent { siteId: string; keyId: string; status: string; message: string; createdAt: string }
const open = ref(false), busy = ref(false), error = ref(''), message = ref('')
const policy = ref<Policy>({ enabled: true, auditEnabled: true, auditIntervalMinutes: 5, time: '04:00', timezone: 'Asia/Shanghai', intervalHours: 24, version: 0, nextRotation: '' })
const events = ref<SecurityEvent[]>([])
async function load() {
  try { const v = await operationJSON<{policy: Policy; events: SecurityEvent[]}>('/my-sites/key-security'); policy.value = v.policy; events.value = v.events }
  catch (e: any) { error.value = e.message }
}
onMounted(load)
async function show() { open.value = true; error.value = ''; await load() }
async function save() {
  const minutes = policy.value.auditIntervalMinutes
  if (!Number.isInteger(minutes) || minutes < 1 || minutes > 1440) {
    error.value = '巡检间隔应为 1 至 1440 分钟'
    return
  }
  busy.value = true; error.value = ''; message.value = ''
  try { policy.value = await operationJSON<Policy>('/my-sites/key-security', { method: 'PUT', body: JSON.stringify(policy.value) }); message.value = '安全策略已保存' }
  catch (e: any) { error.value = e.message } finally { busy.value = false }
}
async function run(action: 'sync-all' | 'sync' | 'rotate') {
  busy.value = true; error.value = ''; message.value = '后台执行中，可关闭此窗口'
  try {
    let discoveryWarning = ''
    if (action === 'rotate' || action === 'sync-all') {
      const discovery = await followJob(await operationJSON<OperationJob>('/my-sites/key-security/discover', { method: 'POST' }))
      const result = discovery.items[0]?.result
      if (discovery.failed) discoveryWarning = '绑定发现失败，已处理现有绑定；详情见任务记录。'
      else if (result?.skipped) discoveryWarning = `${result.skipped} 个渠道未能匹配上游，详情见任务记录。`
    }
    let job = await operationJSON<OperationJob>(`/my-sites/key-security/${action}`, { method: 'POST' })
    job = await followJob(job)
    const children = job.items.filter(v => v.result?.childJobId).map(v => v.result.childJobId as string)
    const results = await Promise.all(children.map(async id => followJob(await operationJSON<OperationJob>(`/channel-monitor/operations/${encodeURIComponent(id)}`))))
    const failed = job.failed + results.reduce((n, v) => n + v.failed, 0)
    const count = children.length ? results.reduce((n, v) => n + v.total, 0) : job.total
    const namingPending = [job, ...results].flatMap(v => v.items).filter(v => v.result?.namingWarning).length
    if (failed) error.value = `${failed} 项失败，请在任务进度中查看并重试`
    message.value = `已处理 ${count} 个目标${failed ? `，${failed} 项失败` : ''}${namingPending ? `，${namingPending} 个命名待处理` : ''}${discoveryWarning ? `。${discoveryWarning}` : ''}`
    await load(); window.dispatchEvent(new Event('transithub:key-policy'))
  } catch (e: any) { error.value = e.message; message.value = '' } finally { busy.value = false }
}
function date(s: string) { return new Date(s).toLocaleString('zh-CN', { timeZone: policy.value.timezone, hour12: false }) }
</script>
<template>
  <button type="button" :disabled="busy" class="rounded-lg bg-sky-500/10 px-3 py-1.5 text-xs font-medium text-sky-700 dark:text-sky-300 disabled:opacity-50" @click="run('sync-all')">一键同步白名单</button>
  <button type="button" :disabled="busy" class="rounded-lg bg-orange-500/10 px-3 py-1.5 text-xs font-medium text-orange-700 dark:text-orange-300 disabled:opacity-50" @click="run('rotate')">一键轮换 Key</button>
  <span v-if="busy || message || error" role="status" class="max-w-sm text-xs" :class="error ? 'text-red-500' : 'text-muted-foreground'">{{ error || (busy ? '后台执行中…' : message) }}</span>
  <button type="button" class="rounded-lg border border-border/50 bg-surface px-3 py-1.5 text-xs" @click="show">Key 安全 · {{ policy.enabled ? `${policy.time} 自动轮换` : '轮换已暂停' }}</button>
  <Teleport to="body">
    <div v-if="open" class="fixed inset-0 z-[85] flex items-center justify-center bg-black/40 p-4" @click.self="open=false">
      <section role="dialog" aria-modal="true" aria-label="Key 安全" class="max-h-[90vh] w-full max-w-2xl space-y-5 overflow-y-auto rounded-xl border border-border/50 bg-surface p-5 shadow-xl">
        <div class="flex items-center justify-between"><h3 class="text-base font-semibold">Key 安全</h3><button type="button" aria-label="关闭 Key 安全" @click="open=false">✕</button></div>
        <div class="flex flex-wrap gap-3">
          <button type="button" :disabled="busy" class="rounded-lg bg-primary px-3 py-2 text-sm text-white disabled:opacity-50" @click="run('sync-all')">同步自有站绑定 Key 白名单</button>
          <button type="button" :disabled="busy" class="rounded-lg border border-border/60 px-3 py-2 text-sm disabled:opacity-50" @click="run('rotate')">立即轮换全部绑定 Key</button>
        </div>
        <p class="text-sm text-muted-foreground">仅管理自有 Sub2API 当前绑定使用的 Key；其他用途的 Key 不修改。</p>
        <div class="space-y-4 rounded-lg border border-border/50 p-4 text-sm">
          <label class="flex items-center gap-2"><input v-model="policy.auditEnabled" type="checkbox" :disabled="busy" />启用白名单自动巡检</label>
          <label class="block max-w-56 space-y-1">
            <span class="block">巡检间隔（分钟）</span>
            <input v-model.number="policy.auditIntervalMinutes" type="number" min="1" max="1440" :disabled="busy || !policy.auditEnabled" class="w-full rounded border border-border/50 bg-background p-2" />
          </label>
          <label class="flex items-center gap-2"><input v-model="policy.enabled" type="checkbox" :disabled="busy" />启用定时轮换</label>
          <div class="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <label class="space-y-1"><span class="block">更新时间</span><input v-model="policy.time" type="time" :disabled="busy" class="w-full rounded border border-border/50 bg-background p-2" /></label>
            <label class="space-y-1"><span class="block">间隔（小时）</span><input v-model.number="policy.intervalHours" type="number" min="1" max="2160" :disabled="busy" class="w-full rounded border border-border/50 bg-background p-2" /></label>
            <label class="space-y-1"><span class="block">时区</span><select v-model="policy.timezone" :disabled="busy" class="w-full rounded border border-border/50 bg-background p-2"><option value="Asia/Shanghai">北京时间</option><option value="UTC">UTC</option></select></label>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-3"><span v-if="policy.enabled && policy.nextRotation" class="text-muted-foreground">下次：{{ date(policy.nextRotation) }}</span><button type="button" :disabled="busy" class="rounded-lg border border-primary/40 px-3 py-1.5 text-primary disabled:opacity-50" @click="save">保存设置</button></div>
        </div>
        <p v-if="message" role="status" class="text-sm text-primary">{{ message }}</p><p v-if="error" role="alert" class="text-sm text-red-500">{{ error }}</p>
        <details v-if="events.length" class="text-sm"><summary class="cursor-pointer">最近安全记录</summary><div class="mt-2 max-h-52 space-y-2 overflow-auto"><div v-for="(item, index) in events" :key="index" class="flex flex-wrap gap-x-3 gap-y-1 border-t border-border/30 pt-2"><span class="text-muted-foreground">{{ date(item.createdAt) }}</span><span>Key {{ item.keyId }}</span><span :class="item.status === 'failed' ? 'text-red-500' : 'text-emerald-600'">{{ item.message }}</span></div></div></details>
      </section>
    </div>
  </Teleport>
</template>
