<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ClipboardList, X } from 'lucide-vue-next'
import { operationJSON, followJob, type OperationJob, operationJobs, retryJob } from '../api/operations'

const props = defineProps<{ workspaceId?: string }>()
const open = ref(false)
const trigger = ref<HTMLButtonElement>()
const closeButton = ref<HTMLButtonElement>()
const pending = ref<string[]>([])
const error = ref('')
const active = computed(() => operationJobs.value.filter(job => job.status === 'running').length)
const labels: Record<string, string> = {
  upstream_key_cleanup: '上游 Key 清理', managed_key_binding: 'Key 命名与拆分', membership_priority: '账号变动自动优先级',
  key_security_bindings: 'Key 绑定发现', key_security: 'Key 安全', key_security_scan: 'Key 白名单发现', monitor: '渠道操作',
  health_preview: '连接预览', health_probe: '模型预检', health_refresh: '状态同步', model_catalog: '模型目录',
  manual_upstream_model: '手动模型测试', upstream_api_sync: 'API 地址同步', monitor_bindings: '同步监控绑定',
}

watch([open, () => props.workspaceId], async ([visible, workspace], _, cleanup) => {
  if (!visible || !workspace) return
  const controller = new AbortController()
  cleanup(() => controller.abort())
  error.value = ''
  try {
    const jobs = await operationJSON<OperationJob[]>('/channel-monitor/operations', { signal: controller.signal })
    if (controller.signal.aborted) return
    const existing = new Map(operationJobs.value.map(job => [job.id, job]))
    operationJobs.value = jobs.map(job => existing.get(job.id) || job)
    for (const job of operationJobs.value) {
      if (job.status === 'running') void followJob(job).catch(() => {})
    }
  } catch (err) {
    if (!controller.signal.aborted) error.value = err instanceof Error ? err.message : String(err)
  }
})
watch(open, visible => { if (visible) closeButton.value?.focus(); else trigger.value?.focus() }, { flush: 'post' })

async function retry(job: OperationJob) {
  if (pending.value.includes(job.id)) return
  pending.value = [...pending.value, job.id]
  error.value = ''
  try {
    await retryJob(job.id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    pending.value = pending.value.filter(id => id !== job.id)
  }
}

function resultText(item: OperationJob['items'][number]) {
  return item.error || item.result?.message || item.result?.Message || item.result?.status || ''
}
</script>

<template>
  <button ref="trigger" type="button" class="inline-flex h-8 items-center gap-1.5 rounded-md px-2 text-xs text-muted-foreground hover:bg-surface-elevated hover:text-foreground" aria-label="任务进度" aria-controls="operation-sidebar" :aria-expanded="open" @click="open = !open">
    <ClipboardList class="size-4" /><span class="hidden sm:inline">任务进度</span><span v-if="active" class="rounded bg-primary/10 px-1 text-primary">{{ active }}</span>
  </button>
  <Teleport to="body">
    <div v-if="open" class="fixed inset-0 z-[180] bg-black/20" @click.self="open = false" @keydown.esc.stop="open = false">
      <aside id="operation-sidebar" role="dialog" aria-label="任务进度" class="ml-auto flex h-full w-full max-w-sm flex-col border-l border-border bg-card shadow-xl">
        <header class="flex items-center justify-between border-b border-border/50 px-4 py-3">
          <h2 class="text-sm font-semibold">任务进度</h2>
          <button ref="closeButton" type="button" class="rounded p-1.5 text-muted-foreground hover:bg-surface-elevated" aria-label="收起任务进度" @click="open = false"><X class="size-4" /></button>
        </header>
        <div class="flex-1 divide-y divide-border/50 overflow-y-auto px-4">
          <p v-if="error" role="alert" class="py-3 text-sm text-destructive">{{ error }}</p>
          <p v-else-if="!operationJobs.length" class="py-4 text-sm text-muted-foreground">暂无任务</p>
          <article v-for="job in operationJobs" :key="job.id" class="space-y-1.5 py-3 text-sm">
            <div class="flex items-center justify-between gap-2">
              <span class="font-medium">{{ labels[job.kind] || job.kind }}</span>
              <span :class="job.failed ? 'text-destructive' : 'text-muted-foreground'">{{ job.completed + job.failed }}/{{ job.total }} · {{ job.status === 'running' ? '执行中' : job.failed ? `${job.failed} 项失败` : '已完成' }}</span>
            </div>
            <progress v-if="job.status === 'running'" class="block h-1 w-full accent-primary" :value="job.completed + job.failed" :max="job.total" :aria-label="labels[job.kind] || job.kind" />
            <button v-if="job.failed && job.status !== 'running'" type="button" class="text-xs text-primary disabled:opacity-50" :disabled="pending.includes(job.id)" @click="retry(job)">重试失败项</button>
            <details v-if="job.items.some(item => item.error || item.result)" class="text-xs text-muted-foreground">
              <summary class="cursor-pointer">逐项结果</summary>
              <ul class="mt-1.5 max-h-44 space-y-1 overflow-auto">
                <li v-for="item in job.items" :key="item.id" class="break-words">
                  {{ item.result?.Model || item.id }} · {{ resultText(item) || ({ done: '已完成', waiting: '等待上游', running: '执行中', queued: '排队中', cancelled: '已取消' } as Record<string, string>)[item.status] || item.status }}
                  <span v-if="item.result?.TaskID" class="block">上游任务 {{ item.result.TaskID }}</span>
                </li>
              </ul>
            </details>
          </article>
        </div>
      </aside>
    </div>
  </Teleport>
</template>
