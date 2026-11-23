<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { followJob, operationJSON, type OperationJob } from '../api/operations'

const props = defineProps<{
  siteId?: string
  previewId?: string
  groupId?: string
  groups?: { id: string; name: string; platform?: string | null }[]
  connectionId?: string
  keyId?: string
  initialModel?: string
  label?: string
}>()
const emit = defineEmits<{ close: [] }>()
const target = computed(() => props.previewId
  ? `/upstream-previews/${encodeURIComponent(props.previewId)}/manual-model-test`
  : `/upstream-sites/${encodeURIComponent(props.siteId ?? '')}/manual-model-test`)
const group = ref(props.groupId || props.groups?.[0]?.id || '')
const key = ref(props.keyId ?? '')
const model = ref(props.initialModel ?? '')
const mode = ref('auto')
const platform = ref('')
const loading = ref(false)
const busy = ref(false)
const keys = ref<{id: string; name: string}[]>([])
const models = ref<string[]>([])
const reportedModels = ref<string[]>([])
const warning = ref('')
const error = ref('')
type TestResult = { Success: boolean; Inconclusive?: boolean; Message: string; Model: string; LatencyMS: number; TaskID?: string; models?: string[] }
const result = ref<TestResult>()
const fixedKey = Boolean(props.connectionId || props.keyId)
const ready = computed(() => (fixedKey || Boolean(group.value)) && !!model.value.trim())
let controller: AbortController | undefined

async function readCatalog(readKeys: boolean) {
  controller?.abort()
  controller = new AbortController()
  const request = controller
  loading.value = true
  warning.value = ''
  try {
    const query = new URLSearchParams({ groupId: group.value })
    if (!readKeys && key.value) query.set('keyId', key.value)
    if (props.connectionId) query.set('connectionId', props.connectionId)
    const response = await operationJSON<{ keys: typeof keys.value; models: string[]; warning?: string }>(target.value + '?' + query, { signal: request.signal })
    if (request.signal.aborted) return
    warning.value = response.warning ?? ''
    if (readKeys) {
      keys.value = response.keys
      reportedModels.value = response.models ?? []
      models.value = reportedModels.value
      key.value = keys.value[0]?.id ?? ''
    } else {
      models.value = response.models?.length ? response.models : reportedModels.value
    }
  } catch (err) {
    if (!request.signal.aborted) warning.value = err instanceof Error ? err.message : String(err)
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

watch(group, () => {
  result.value = undefined
  if (fixedKey) void readCatalog(false)
  else if (group.value) {
    key.value = ''
    keys.value = []
    models.value = []
    reportedModels.value = []
    void readCatalog(true)
  }
}, { immediate: true })
watch(key, () => {
  result.value = undefined
  if (key.value) void readCatalog(false)
  else models.value = reportedModels.value
})
onBeforeUnmount(() => controller?.abort())

async function runTest(catalogOnly = false) {
  if (busy.value || (catalogOnly ? !group.value || fixedKey || !!key.value : !ready.value)) return
  busy.value = true
  error.value = ''
  result.value = undefined
  try {
    const payload = { groupId: group.value, keyId: key.value, connectionId: props.connectionId, model: model.value.trim(), mode: mode.value, platform: platform.value, catalogOnly }
    const job = await followJob(await operationJSON<OperationJob>(target.value, { method: 'POST', body: JSON.stringify(payload) }))
    const item = job.items[0]
    if (job.failed || !item?.result) throw new Error(item?.error || '测试任务未完成')
    const output: TestResult = item.result
    if (catalogOnly) {
      if (!output.Success) throw new Error(output.Message)
      reportedModels.value = output.models ?? []
      models.value = reportedModels.value
      warning.value = output.Message
    } else {
      result.value = output
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[220] flex items-center justify-center bg-black/40 p-4" @click.self="emit('close')">
      <section role="dialog" aria-modal="true" aria-label="实际测试模型" class="max-h-[90vh] w-full max-w-lg space-y-4 overflow-auto rounded-xl border border-border/60 bg-card p-5 shadow-xl">
        <header class="flex items-start justify-between gap-3">
          <div><h3 class="font-semibold">实际测试模型</h3><p v-if="label" class="mt-1 break-words text-sm text-muted-foreground">{{ label }}</p></div>
          <Button variant="ghost" size="sm" @click="emit('close')">关闭</Button>
        </header>
        <div v-if="!groupId && groups?.length" class="space-y-1 text-sm">
          <label for="manual-test-group">上游分组</label>
          <select id="manual-test-group" v-model="group" :disabled="busy" class="block w-full rounded-lg border border-border bg-background px-3 py-2">
            <option v-for="item in groups" :key="item.id" :value="item.id">{{ item.name }}</option>
          </select>
        </div>
        <label v-if="!fixedKey && group" class="block space-y-1 text-sm">
          <span>测试凭据</span>
          <select v-model="key" :disabled="busy" class="block w-full rounded-lg border border-border bg-background px-3 py-2">
            <option v-for="item in keys" :key="item.id" :value="item.id">{{ item.name || ('Key ID ' + item.id) }}</option>
            <option value="">创建临时测试 Key</option>
          </select>
          <span v-if="!key" class="block text-xs text-muted-foreground">发起测试时创建，完成后自动清理。</span>
        </label>
        <label class="block space-y-1 text-sm">
          <span>模型</span>
          <input v-model="model" list="manual-test-models" maxlength="200" placeholder="选择或输入模型 ID" :disabled="busy" class="block w-full rounded-lg border border-border bg-background px-3 py-2" />
          <datalist id="manual-test-models"><option v-for="id in models" :key="id" :value="id" /></datalist>
        </label>
        <div class="flex flex-wrap items-center gap-2">
          <Button v-if="!fixedKey && group && !key" variant="secondary" size="sm" :disabled="loading || busy" @click="runTest(true)">读取临时 Key 的模型目录</Button>
          <p v-if="loading || warning" class="text-xs text-muted-foreground">{{ loading ? '读取模型目录…' : warning }}</p>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="block space-y-1 text-sm"><span>测试类型</span><select v-model="mode" :disabled="busy" class="block w-full rounded-lg border border-border bg-background px-3 py-2"><option value="auto">自动识别</option><option value="text">文本</option><option value="image">生图</option><option value="video">视频</option><option value="audio">音频</option></select></label>
          <label class="block space-y-1 text-sm"><span>调用协议</span><select v-model="platform" :disabled="busy" class="block w-full rounded-lg border border-border bg-background px-3 py-2"><option value="">沿用分组</option><option value="openai">OpenAI 兼容</option><option value="anthropic">Anthropic</option><option value="gemini">Gemini</option><option value="grok">Grok</option></select></label>
        </div>
        <p v-if="result" role="status" class="break-words rounded-lg border border-border bg-surface p-3 text-sm" :class="result.Success ? 'text-emerald-600' : 'text-warning'">
          <span class="block font-medium">{{ result.Success ? '调用成功' : result.Inconclusive ? '调用待确认' : '调用失败' }} · {{ result.Model }}</span>
          <span>{{ result.Message }}<template v-if="result.LatencyMS"> · {{ result.LatencyMS }} ms</template></span>
        </p>
        <p v-if="error" role="alert" class="break-words text-sm text-destructive">{{ error }}</p>
        <div class="flex flex-wrap items-center justify-end gap-3">
          <span class="text-xs text-muted-foreground">将发送实际请求，可能产生费用。</span>
          <Button :disabled="busy || loading || !ready" @click="runTest()">{{ busy ? '后台测试中…' : '发起实际测试' }}</Button>
        </div>
      </section>
    </div>
  </Teleport>
</template>
