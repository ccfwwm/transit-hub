<script setup lang="ts">
import StatusSourceControl from './StatusSourceControl.vue'
import ModelStatusStrip from './ModelStatusStrip.vue'
import type {StatusPoint} from './ModelStatusStrip.vue'
import { ref, watch } from 'vue'
import { operationJSON, followJob, type OperationJob } from '../api/operations'
export interface HealthRow {timeline?:StatusPoint[];latencyMs?:number|null;availability?:number|null;throughput?:number|null;id: string; model: string; groupId: string; groupName: string; source: string; scope: string; status: string; checkedAt: string | null; fresh: boolean; usable: boolean; message: string}
export interface HealthPage {items: HealthRow[]; total: number; page: number; pageSize: number; capabilities: {source: string; status: string; message: string}[]}
const props = defineProps<{ siteId?: string; previewId?: string; groups?: {id: string; name: string;multiplier?:number|null}[] }>()
const mapping=ref<HealthRow|null>(null),mappedGroup=ref(''),mappedModel=ref('')
const page = ref(1), search = ref(''), status = ref('all'), groupId = ref('')
const loading = ref(false), error = ref('')
const data = ref<HealthPage>({items: [], total: 0, page: 1, pageSize: 25, capabilities: []})
const sources: Record<string,string> = {sub2api_image_status:'上游生图分组统计', sub2api_v1: '上游监控状态', sub2api_v2: '上游真实调用统计',sub2api_marketplace:'上游号池请求统计', newapi_kuma: '状态页参考', newapi_channels:'渠道配置参考',newapi_perf:'上游模型调用统计',newapi_monitors:'上游线路监控',newapi_service:'上游服务状态'}
const labels: Record<string,string> = {healthy: '可用', failed: '故障', warning: '降级', unknown: '未知'}
let request = 0
async function load() {
  if (!props.siteId && !props.previewId) return
  const id = ++request; loading.value = true; error.value = ''
  const query = new URLSearchParams({page: String(page.value), pageSize: '25', q: search.value, status: status.value, groupId: groupId.value})
  try {
    const result = props.previewId
      ? (await operationJSON<{health: HealthPage}>(`/upstream-previews/${encodeURIComponent(props.previewId)}?${query}`)).health
      : await operationJSON<HealthPage>(`/upstream-sites/${encodeURIComponent(props.siteId!)}/model-health?${query}`)
    if (id === request) data.value = result
  } catch (e: any) { if (id === request) error.value = e.message }
  finally { if (id === request) loading.value = false }
}
watch(() => [props.siteId, props.previewId], () => { page.value = 1; void load() }, {immediate: true})
watch([page, search, status, groupId], () => { void load() })
async function refresh() {
  if (!props.siteId) { await load(); return }
  loading.value = true
  try { const job=await followJob(await operationJSON<OperationJob>(`/upstream-sites/${encodeURIComponent(props.siteId)}/model-health/refresh`, {method: 'POST'})); if(job.failed)throw new Error(job.items.find(i=>i.error)?.error||'同步失败'); await load() }
  catch (e: any) { error.value = e.message } finally {loading.value = false}
}
async function saveMapping(){if(!mapping.value||!props.siteId)return;try{await operationJSON(`/upstream-sites/${encodeURIComponent(props.siteId)}/model-health/mapping`,{method:'PUT',body:JSON.stringify({sourceId:mapping.value.id,groupId:mappedGroup.value,model:mappedModel.value})});mapping.value=null;await load()}catch(e:any){error.value=e.message}}
</script>
<template>
  <section class="space-y-3 rounded-xl border border-border/50 bg-surface p-4">
    <div class="flex flex-wrap items-center justify-between gap-2"><h3 class="font-semibold">上游模型状态</h3><button type="button" class="text-sm text-primary" :disabled="loading" @click="refresh">{{ loading ? '读取中…' : '重新同步' }}</button></div>
    <StatusSourceControl />
    <div class="flex flex-wrap gap-2 text-sm"><input v-model="search" class="min-w-40 rounded border border-border/50 bg-background px-2 py-1" placeholder="搜索模型或分组" /><select v-model="groupId" class="rounded border border-border/50 bg-background px-2 py-1"><option value="">全部分组</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option></select><select v-model="status" class="rounded border border-border/50 bg-background px-2 py-1"><option value="all">全部状态</option><option v-for="(label, value) in labels" :key="value" :value="value">{{ label }}</option></select></div>
    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
    <div class="max-h-[55vh] overflow-auto"><div class="grid grid-cols-1 gap-2 sm:grid-cols-2"><article v-for="row in data.items" :key="row.id" class="min-w-0 rounded-lg border border-border/50 p-3"><div class="mb-1 flex items-start justify-between gap-2"><span class="min-w-0 break-words text-sm font-medium">{{row.model}}</span><span class="shrink-0 text-xs" :class="row.status==='healthy'?'text-emerald-600':row.status==='failed'?'text-red-500':'text-muted-foreground'" :title="row.message">{{labels[row.status]||row.status}}{{row.scope==='infrastructure'||row.scope.includes('reference')?' · 参考':''}}</span></div><div class="mb-2 flex items-center justify-between gap-2 text-xs text-muted-foreground"><span class="truncate">{{row.groupName||'站点参考'}}<span v-if="groups?.find(g=>g.id===row.groupId)?.multiplier!=null"> · {{groups?.find(g=>g.id===row.groupId)?.multiplier}}×</span></span><button v-if="siteId&&row.scope!=='infrastructure'" class="shrink-0 text-primary" @click="mapping=row;mappedGroup=row.groupId;mappedModel=row.model">映射</button></div><ModelStatusStrip :points="row.timeline" :status="row.status" :latency-ms="row.latencyMs" :availability="row.availability" :throughput="row.throughput" hide-details /></article></div><p v-if="!data.items.length&&!loading" class="p-3 text-sm text-muted-foreground">上游暂未公开匹配的模型状态</p></div>
    <div v-if="mapping" class="flex flex-wrap gap-2 text-sm"><select v-model="mappedGroup" class="rounded border bg-background p-1"><option value="">选择对应分组</option><option v-for="g in groups" :key="g.id" :value="g.id">{{g.name}}</option></select><input v-model="mappedModel" class="rounded border bg-background p-1" placeholder="实际调用模型 ID" /><button type="button" :disabled="!mappedGroup||!mappedModel" @click="saveMapping">保存映射</button><button type="button" @click="mapping=null">取消</button></div>
    <div class="flex justify-between text-xs"><span>共 {{ data.total }} 项</span><div class="flex gap-3"><button type="button" :disabled="page <= 1" @click="page--">上一页</button><span>{{ page }}</span><button type="button" :disabled="page * 25 >= data.total" @click="page++">下一页</button></div></div>
    <details v-if="data.capabilities.length" class="text-xs text-muted-foreground"><summary>接口能力</summary><div v-for="cap in data.capabilities" :key="cap.source">{{ sources[cap.source] || cap.source }} · {{ cap.status }} · {{ cap.message }}</div></details>

  </section>
</template>
