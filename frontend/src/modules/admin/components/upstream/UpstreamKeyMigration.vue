<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { operationJSON, followJob, retryJob, type OperationJob } from '../../api/operations'
import type { BindingPlan, KeySite } from '../../types/upstreamKeys'

const props = defineProps<{ sites: KeySite[]; initialSite: string; initialBinding: string }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const site = ref(props.initialSite)
const slug = ref('')
const plans = ref<BindingPlan[]>([])
const selectedIDs = ref<string[]>([])
const retain = ref<Record<string,string>>({})
const budgets = ref<Record<string, Record<string, Record<string, number>>>>({})
const busy = ref(false)
const error = ref('')
const job = ref<OperationJob | null>(null)
const ready = (plan: BindingPlan) => !plan.error && plan.targets.every(target => !target.error)
const selected = computed(() => plans.value.filter(plan => selectedIDs.value.includes(plan.id)))
const format = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 8 })
const limitLabels: Record<string,string> = { remain_quota:'剩余额度', quota:'总额度（USD）', rate_limit_5h:'5 小时额度', rate_limit_1d:'1 天额度', rate_limit_7d:'7 天额度' }
async function load() {
  busy.value = true
  error.value = ''
  try {
    plans.value = await operationJSON<BindingPlan[]>('/upstream-key-bindings/preview' + (site.value ? '?site=' + encodeURIComponent(site.value) : ''))
    selectedIDs.value = plans.value.filter(plan => plan.id === props.initialBinding && ready(plan)).map(plan => plan.id)
    for (const plan of plans.value) {
      retain.value[plan.id] = plan.targets.find(target => target.retain)?.groupId || ''
      budgets.value[plan.id] = Object.fromEntries(plan.targets.map(target => [target.groupId, {}]))
    }
  } catch (err) { error.value = err instanceof Error ? err.message : String(err) }
  finally { busy.value = false }
}
onMounted(load)
async function saveSlug() {
  try {
    await operationJSON('/upstream-key-bindings/sites/' + encodeURIComponent(site.value) + '/slug', { method: 'PUT', body: JSON.stringify({ slug: slug.value }) })
    await load()
  } catch (err) { error.value = err instanceof Error ? err.message : String(err) }
}
async function apply(retry = false) {
  busy.value = true
  error.value = ''
  try {
    job.value = retry && job.value ? await retryJob(job.value.id) : await followJob(await operationJSON<OperationJob>('/upstream-key-bindings/apply', {
      method: 'POST', body: JSON.stringify({ entries: selected.value.map(plan => ({ id: plan.id, retain: retain.value[plan.id], budgets: budgets.value[plan.id] })) }),
    }))
    emit('changed')
  } catch (err) { error.value = err instanceof Error ? err.message : String(err) }
  finally { busy.value = false }
}
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[110] flex items-center justify-center bg-black/50 p-3" @click.self="!busy && emit('close')">
      <section class="flex max-h-[91dvh] w-full max-w-4xl flex-col rounded-xl border border-border bg-card shadow-2xl" role="dialog" aria-modal="true" aria-labelledby="key-preview-title">
        <header class="flex flex-wrap items-center justify-between gap-2 p-5">
          <h2 class="text-lg font-semibold" id="key-preview-title">命名与拆分预览</h2>
          <button class="rounded-md p-1 hover:bg-muted" aria-label="关闭" @click="emit('close')"><X class="size-5" /></button>
          <div class="flex w-full flex-wrap items-center gap-2">
            <select v-model="site" aria-label="选择站点" :disabled="busy" @change="load"><option value="">所有站点</option><option v-for="item in sites" :key="item.id" :value="item.id">{{ item.name }}</option></select>
            <Button variant="secondary" :disabled="busy" @click="load">刷新预览</Button>
            <form v-if="site" class="flex gap-2" @submit.prevent="saveSlug"><input v-model="slug" aria-label="自定义域名简称" placeholder="自定义域名简称" required /><Button variant="ghost" type="submit" :disabled="busy">保存简称</Button></form>
          </div>
        </header>
        <div class="min-h-0 flex-1 overflow-y-auto px-5">
          <p v-if="error" role="alert" class="mb-3 text-sm text-destructive">{{ error }}</p>
          <p v-if="busy && !plans.length" class="py-6 text-sm text-muted-foreground">读取绑定预览…</p>
          <p v-else-if="!plans.length" class="py-6 text-sm text-muted-foreground">当前站点没有对接绑定。</p>
          <article v-for="plan in plans" :key="plan.id" class="space-y-2 border-t border-border py-3">
            <label class="flex items-start gap-2 text-sm font-medium">
              <input v-model="selectedIDs" type="checkbox" :value="plan.id" :disabled="busy || !ready(plan)" class="mt-1" />
              <span class="break-all">{{ plan.siteName }} · {{ plan.accountName }}<span class="block text-xs text-muted-foreground">上游 Key {{ plan.keyName || '—' }}</span></span>
            </label>
            <p v-if="plan.error" class="text-sm text-warning">{{ plan.error }}</p>
            <label v-if="plan.targets.length > 1" class="flex flex-wrap items-center gap-2 text-sm">保留原 Key 和账号的分组<select v-model="retain[plan.id]" :disabled="busy"><option v-for="target in plan.targets" :key="target.groupId" :value="target.groupId">{{ target.groupName }}</option></select></label>
            <div v-for="target in plan.targets" :key="target.groupId" class="space-y-1 pl-5 text-sm">
              <p class="text-xs text-muted-foreground">{{ target.groupId === retain[plan.id] ? '保留并改名' : '新建独立对接' }} · 两端统一为</p>
              <p class="break-words">{{ target.name.slice(0, target.name.lastIndexOf('-', target.name.lastIndexOf('-') - 1) + 1) }}<span class="whitespace-nowrap">{{ target.name.slice(target.name.lastIndexOf('-', target.name.lastIndexOf('-') - 1) + 1) }}</span> <span class="text-warning">{{ target.error }}</span></p>
              <div v-if="selectedIDs.includes(plan.id) && plan.limits && budgets[plan.id]?.[target.groupId]" class="flex flex-wrap gap-2">
                <label v-for="(limit,field) in plan.limits" :key="field" class="flex items-center gap-2 text-xs">{{ limitLabels[field] || field }}（总可用 {{ format.format(limit) }}）<input v-model.number="budgets[plan.id]![target.groupId]![field]" type="number" min="0" step="any" class="w-24" /></label>
              </div>
            </div>
          </article>
          <details v-if="job" open class="my-3 rounded-lg bg-muted p-3 text-sm">
            <summary>已完成 {{ job.completed }} / {{ job.total }} · 失败 {{ job.failed }}</summary>
            <ul v-if="job.failed" class="mt-2 space-y-1 text-destructive"><li v-for="item in job.items.filter(row => row.error)" :key="item.id">{{ item.result?.phase || '检查' }} · {{ item.error }}</li></ul>
          </details>
        </div>
        <footer class="flex flex-wrap justify-end gap-2 border-t border-border p-4">
          <Button v-if="job?.failed" variant="secondary" :disabled="busy" @click="apply(true)">重试失败条目</Button>
          <Button :disabled="busy || !selected.length" @click="apply()">{{ busy ? '处理中…' : '执行已选 ' + selected.length + ' 条' }}</Button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
select, input:not([type='checkbox']) { @apply rounded-md border border-border bg-background px-2 py-2 text-sm; }
</style>
