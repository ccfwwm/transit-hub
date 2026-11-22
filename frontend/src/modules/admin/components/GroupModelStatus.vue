<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import ManualModelCheck from './ManualModelCheck.vue'
import ModelStatusStrip,{type StatusPoint} from './ModelStatusStrip.vue'
import StatusSourceControl from './StatusSourceControl.vue'
import {monitorStatusSource,loadStatusSource} from '../api/statusSource'
import { operationJSON,followJob,type OperationJob } from '../api/operations'
export interface ModelRow {mode?:string;id:string;model:string;status:string;source:string;scope?:string;groupId?:string;groupName?:string;fresh?:boolean;usable?:boolean;checkedAt?:string|null;latencyMs?:number|null;pingMs?:number|null;availability?:number|null;throughput?:number|null;timeline?:StatusPoint[];message?:string}
interface ManualCheck {status:string;message:string;checkedAt?:string;startedAt:string;jobId:string}
interface ModelPage {monitoredModel?:string;manualChecks?:Record<string,ManualCheck>;reportedTotal?:number;items:ModelRow[];references?:ModelRow[];total:number;multiplier?:number|null;catalogUpdatedAt?:string|null;catalogError?:string}
const props=defineProps<{preview?:ModelRow;upstreamOnly?:boolean;inline?:boolean;selectable?:boolean;siteId:string;groupId?:string;groupName?:string;ruleId?:string;compact?:boolean;summary?:boolean;multiplier?:number|null;modelId?:string;monitorGroupId?:string}>()
const emit=defineEmits<{selectModel:[model:string]}>()
const effectiveSource=computed(()=>props.upstreamOnly||!props.ruleId?'upstream':monitorStatusSource.value)
const data=ref<ModelPage>({items:[],total:0}),opened=ref(!!props.inline),loading=ref(false),busy=ref(false),error=ref(''),search=ref(''),filter=ref('all'),page=ref(1)
const labels:Record<string,string>={auto_paused:'故障',healthy:'正常',failed:'故障',warning:'降级',unknown:'未报告',generating:'生成中',config_error:'待配置',balance_paused:'余额不足',manual_paused:'已停检',unsupported:'未适配'}
function color(status:string){return status==='healthy'?'text-emerald-600 dark:text-emerald-400':['failed','auto_paused'].includes(status)?'text-red-500':status==='warning'?'text-amber-600':'text-muted-foreground'}
// A real monitor's recent observations; model counts are never used as history.
const summaryModel=computed(()=>{
 if(props.preview&&!opened.value)return props.preview
 if(props.summary&&effectiveSource.value==='upstream'){
  // Group cards summarize the latest returned model observations, including
  // __other__. Unknown catalog entries and infrastructure are not failures.
  const modelRows=data.value.items.filter(m=>m.source!=='catalog'&&!['infrastructure','channel_reference'].includes(m.scope||''))
  const groupRows=(data.value.references||[]).filter(m=>m.scope==='channel_reference'&&(m.groupId===props.groupId||!m.groupId&&m.groupName===props.groupName))
  const rows=modelRows.length?modelRows:groupRows
  const rank:Record<string,number>={healthy:0,failed:1,warning:2,unknown:3}
  return [...rows].sort((a,b)=>(rank[currentStatus(a)]??3)-(rank[currentStatus(b)]??3)||Date.parse(b.checkedAt||'1970-01-01')-Date.parse(a.checkedAt||'1970-01-01'))[0]
 }
 const selected=props.modelId||data.value.monitoredModel
 if(selected)return data.value.items.find(m=>m.model===selected)||{id:selected,model:selected,status:'unknown',source:'catalog',timeline:[]}
 if(props.ruleId)return undefined
 return [...data.value.items.filter(m=>m.source!=='catalog'),...(data.value.references||[]).filter(m=>m.groupId===props.groupId||m.scope==='infrastructure')].sort((a,b)=>(b.timeline?.length||0)-(a.timeline?.length||0))[0]
})
function currentCheck(m?:ModelRow){if(!m)return;const v=data.value.manualChecks?.[m.model];if(v?.status==='generating'&&Date.now()-Date.parse(v.startedAt)<3600000)return v;if(!v?.checkedAt)return;if(m.checkedAt&&Date.parse(m.checkedAt)>Date.parse(v.checkedAt))return;return v}
function currentStatus(m?:ModelRow){return currentCheck(m)?.status||m?.status||'unknown'}
function displayPoints(m?:ModelRow){const points=m?.timeline||[];const v=currentCheck(m);return v?.checkedAt&&['healthy','failed'].includes(v.status)?[...points,{status:v.status,checkedAt:v.checkedAt}].slice(-60):points}
function currentLabel(m?:ModelRow){if(currentStatus(m)==='config_error'&&(currentCheck(m)?.message||m?.message||'').includes('HTTP 403'))return '访问受限';return currentStatus(m)==='unknown'?'当前未知':labels[currentStatus(m)]||currentStatus(m)}
const multiplier=computed(()=>props.multiplier??data.value.multiplier)
const references=computed(()=>(data.value.references||[]).filter(m=>!search.value||m.model.toLowerCase().includes(search.value.toLowerCase())))
const filtered=computed(()=>data.value.items.filter(m=>(!search.value||m.model.toLowerCase().includes(search.value.toLowerCase()))&&(filter.value==='all'||currentStatus(m)===filter.value)).sort((a,b)=>Number(a.source==='catalog')-Number(b.source==='catalog')||a.model.localeCompare(b.model)))
const visible=computed(()=>filtered.value.slice((page.value-1)*25,page.value*25))
watch([search,filter],()=>{page.value=1})
let controller:AbortController|undefined
async function load(){
 if(props.preview&&!opened.value)return
 controller?.abort();controller=new AbortController();const current=controller;loading.value=true;error.value=''
 try {
  if(props.ruleId&&!props.upstreamOnly){const result=await operationJSON<ModelPage>(`/channel-monitor/rules/${encodeURIComponent(props.ruleId)}/models?${new URLSearchParams({...(opened.value?{includeCatalog:'1',includeReported:'1'}:{}),monitorGroupId:props.monitorGroupId||''})}`,{signal:current.signal});if(current.signal.aborted)return;data.value=result}
  else if(effectiveSource.value==='upstream'){
   const all:ModelRow[]=[];for(let p=1;p<=100;p++){const q=new URLSearchParams({page:String(p),pageSize:'100',groupId:props.groupId||'',groupName:props.groupName||'',includeReferences:'1'});const v=await operationJSON<ModelPage>(`/upstream-sites/${encodeURIComponent(props.siteId)}/model-health?${q}`,{signal:current.signal});all.push(...v.items);if(all.length>=v.total)break}
   if(current.signal.aborted)return;const unique=new Map<string,ModelRow>(),refs:ModelRow[]=[];for(const m of all){if((['infrastructure','channel_reference'].includes(m.scope||'')||m.source==='newapi_kuma'&&m.groupId!==props.groupId)){refs.push(m);continue}const prev=unique.get(m.model);if(!prev||String(m.checkedAt||'')>String(prev.checkedAt||''))unique.set(m.model,m)}data.value={items:[...unique.values()],references:refs,total:unique.size}
  } else data.value={items:[],total:0}
 }catch(e:any){if(!current.signal.aborted)error.value=e.message}finally{if(!current.signal.aborted)loading.value=false}
}
watch(()=>[props.siteId,props.groupId,props.ruleId,props.modelId,props.monitorGroupId,effectiveSource.value],()=>{data.value={items:[],total:0};page.value=1;void load()},{immediate:true})
const timer=setInterval(()=>{if(!document.hidden)void load()},30000)
onUnmounted(()=>{controller?.abort();clearInterval(timer)})
async function catalog(){if(!props.ruleId)return;const job=await followJob(await operationJSON<OperationJob>(`/channel-monitor/rules/${encodeURIComponent(props.ruleId)}/models/refresh`,{method:'POST'}));if(job.failed)throw new Error('模型目录暂不可读取');await load()}
async function open(){opened.value=true;await load();if(props.ruleId&&!props.upstreamOnly&&!data.value.catalogUpdatedAt){busy.value=true;try{await catalog()}catch(e:any){error.value=e.message}finally{busy.value=false}}}
async function refresh(){if(busy.value)return;busy.value=true;error.value='';try{
 if(effectiveSource.value==='upstream'){const j=await followJob(await operationJSON<OperationJob>(`/upstream-sites/${encodeURIComponent(props.siteId)}/model-health/refresh`,{method:'POST'}));if(j.failed)throw new Error('上游状态同步失败')}
 else if(props.ruleId){const j=await followJob(await operationJSON<OperationJob>('/channel-monitor/operations',{method:'POST',body:JSON.stringify({kind:'run',ids:[props.ruleId]})}));if(j.failed)throw new Error('检测任务失败')}
 await load()
 }catch(e:any){error.value=e.message}finally{busy.value=false}}
void loadStatusSource().catch(()=>{})
</script>
<template>
 <section class="min-w-0" @click.stop>
  <button v-if="!inline" type="button" class="w-full min-w-0 rounded-lg border border-border/50 bg-background/70 px-2.5 py-2 text-left transition-colors hover:border-primary/40" aria-label="查看全部模型状态" @click="open">
   <div class="mb-1.5 flex justify-end text-xs font-medium"><span :class="color(currentStatus(summaryModel))" :title="currentCheck(summaryModel)?.message||summaryModel?.message">{{loading&&!summaryModel?'读取中':currentLabel(summaryModel)}}</span></div>
   <ModelStatusStrip :points="displayPoints(summaryModel)" :status="summaryModel?.status" hide-details hide-metrics />
  </button>
  <Teleport to="body" :disabled="inline"><div v-if="opened" :class="inline ? '' : 'fixed inset-0 z-[150] flex items-center justify-center bg-black/30 p-4 backdrop-blur-sm'" @click.self="opened=false"><section :class="[inline ? 'w-full min-w-0' : 'flex max-h-[85vh] w-full max-w-5xl flex-col overflow-hidden rounded-2xl border border-border/60 bg-card shadow-xl']" :role="inline ? undefined : 'dialog'" :aria-modal="inline ? undefined : true" aria-label="全部模型状态">
   <header :class="['flex items-start justify-between gap-4 border-b border-border/50',inline?'pb-3':'px-5 py-4']"><div class="min-w-0"><h3 class="text-base font-semibold">全部模型状态 <span v-if="multiplier!=null" class="ml-2 rounded bg-primary/10 px-2 py-0.5 text-sm text-primary">{{Number(multiplier.toFixed(4))}}×</span></h3><p class="mt-1 truncate text-sm text-muted-foreground">{{groupName}}</p></div><button v-if="!inline" type="button" class="rounded-lg px-2 py-1 text-sm text-muted-foreground hover:bg-surface" @click="opened=false">关闭</button></header>
   <div :class="['flex flex-wrap items-center justify-between gap-3 border-b border-border/50 py-3',!inline&&'px-5']"><span v-if="upstreamOnly" class="text-xs text-muted-foreground">上游模型状态</span><StatusSourceControl v-else-if="ruleId" /><span v-else class="text-xs text-muted-foreground">上游模型状态</span><button type="button" class="rounded-lg border border-border/50 px-3 py-1.5 text-sm disabled:opacity-50" :disabled="busy" @click="refresh">{{busy?'处理中…':effectiveSource==='upstream'?'同步状态':'检测配置模型'}}</button></div>
   <div :class="['flex flex-wrap gap-2 py-3',!inline&&'px-5']"><input v-model="search" placeholder="搜索模型" class="min-w-40 flex-1 rounded-lg border border-border/50 bg-background px-3 py-2 text-sm" /><select v-model="filter" class="rounded-lg border border-border/50 bg-background px-3 py-2 text-sm"><option value="all">全部状态</option><option value="healthy">正常</option><option value="failed">故障</option><option value="unknown">当前未知</option></select></div>
   <p v-if="error" class="px-5 pb-3 text-sm text-red-500">{{error}}</p>
   <div :class="inline?'pb-4':'min-h-0 flex-1 overflow-auto px-5 pb-4'">
    <template v-if="references.length"><h4 class="mb-2 text-sm font-medium text-muted-foreground">线路状态</h4><div class="mb-3 grid grid-cols-1 gap-2 sm:grid-cols-2"><article v-for="m in references" :key="m.id" class="min-w-0 rounded-lg border border-border/50 px-3 py-2.5"><div class="mb-2 flex justify-between gap-3 text-sm"><span class="min-w-0 break-words font-medium">{{m.model}}</span><span class="shrink-0 text-xs" :class="color(currentStatus(m))" :title="currentCheck(m)?.message||m.message">{{currentLabel(m)}}</span></div><ModelStatusStrip :points="displayPoints(m)" :status="m.status" :availability="m.availability" :latency-ms="m.latencyMs" hide-details hide-metrics /></article></div></template>
    <div :class="['grid grid-cols-1 gap-2',!inline&&'sm:grid-cols-2']"><article v-for="m in visible" :key="m.model" class="min-w-0 rounded-lg border border-border/50 px-3 py-2.5"><div class="mb-2 flex min-w-0 items-start justify-between gap-3 text-sm"><span class="min-w-0 break-words font-medium" :title="m.model">{{m.model}}</span><span class="shrink-0 text-xs" :class="color(currentStatus(m))" :title="currentCheck(m)?.message||m.message">{{currentLabel(m)}}</span><button v-if="selectable && m.source!=='catalog'" type="button" class="shrink-0 text-xs text-primary" @click="emit('selectModel',m.model)">设为主检测</button><ManualModelCheck v-if="ruleId && !upstreamOnly" :rule-id="ruleId" :model="m.model" :mode="m.mode" @done="load" /></div><ModelStatusStrip :points="displayPoints(m)" :status="m.status" :availability="m.availability" :latency-ms="m.latencyMs" :ping-ms="m.pingMs" :throughput="m.throughput" hide-details hide-metrics /></article></div>
    <p v-if="!visible.length" class="py-6 text-center text-sm text-muted-foreground">{{loading?'读取中…':'上游未报告该分组的模型状态'}}</p>

   </div>
   <footer :class="['flex shrink-0 items-center justify-between gap-3 border-t border-border/50 py-3 text-xs text-muted-foreground',!inline&&'px-5']"><span>共 {{filtered.length}} 个模型</span><div class="flex shrink-0 items-center gap-3"><button type="button" :disabled="page<=1" @click="page--">上一页</button><span>{{page}} / {{Math.max(1,Math.ceil(filtered.length/25))}}</span><button type="button" :disabled="page*25>=filtered.length" @click="page++">下一页</button></div></footer>
  </section></div></Teleport>
 </section>
</template>
