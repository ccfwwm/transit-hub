<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { X, Plus, Trash2, RefreshCw } from 'lucide-vue-next'
import { operationJSON, retryJob, type OperationJob } from '../api/operations'
const emit = defineEmits<{close:[]}>()
type Entry = {host:string;manual:boolean;references:string[];active:boolean}
type Policy = {enabled:boolean;items:Entry[];status:string;error?:string;targetUrl?:string;jobs:OperationJob[]}
const data = ref<Policy>({enabled:false,items:[],status:'loading',jobs:[]})
const host = ref(''), search = ref(''), error = ref(''), busy = ref(false), loading = ref(true)
const items = computed(()=>data.value.items.filter(v=>v.host.includes(search.value.trim().toLowerCase())))
const statusLabel = computed(()=>({loading:'读取中',unconfigured:'未配置',synced:'已生效',applying:'等待 Sub2API 确认',pending:'等待生效',failed:'同步失败',offline:'同步程序离线'}[data.value.status] || data.value.status))
let timer: ReturnType<typeof setInterval> | undefined
let stopped = false
let reading = false
let requestVersion = 0

async function load() {
  if (reading || busy.value) return
  reading = true
  const version = ++requestVersion
  try {
    const result = await operationJSON<Policy>('/my-sites/domain-allowlist')
    if (!stopped && version === requestVersion) data.value = result
  } catch (e: unknown) {
    if (!stopped && version === requestVersion) {
      error.value = e instanceof Error ? e.message : '读取域名配置失败'
    }
  } finally {
    reading = false
    loading.value = false
  }
}

async function change(method: 'POST' | 'DELETE', value: string) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  requestVersion++
  try {
    const result = await operationJSON<Policy>('/my-sites/domain-allowlist', {
      method,
      body: JSON.stringify({ host: value }),
    })
    if (!stopped) {
      host.value = ''
      data.value = result
    }
  } catch (e: unknown) {
    if (!stopped) error.value = e instanceof Error ? e.message : '保存域名配置失败'
  } finally {
    busy.value = false
  }
}
async function retry(id:string){busy.value=true;try{await retryJob(id);await load()}catch(e:any){error.value=e.message}finally{busy.value=false}}
onMounted(()=>{void load();timer=setInterval(()=>{if(!document.hidden)void load()},5000)})
onBeforeUnmount(()=>{stopped=true;if(timer)clearInterval(timer)})
</script>
<template>
 <Teleport to="body">
  <div class="fixed inset-0 z-[130] flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm" @click.self="emit('close')">
   <section role="dialog" aria-modal="true" aria-label="域名白名单" class="flex max-h-[90vh] w-full max-w-3xl flex-col rounded-2xl border border-border/70 bg-card shadow-2xl">
    <header class="flex items-center justify-between gap-3 border-b border-border/50 p-5"><div class="flex flex-wrap items-center gap-3"><h2 class="font-semibold">域名白名单</h2><span :class="['rounded px-2 py-1 text-xs',data.status==='synced'?'bg-emerald-500/10 text-emerald-600':data.status==='failed'||data.status==='offline'?'bg-red-500/10 text-red-500':'bg-surface text-muted-foreground']">{{statusLabel}}</span></div><button type="button" aria-label="关闭" class="rounded p-1 hover:bg-surface" @click="emit('close')"><X class="h-5 w-5" /></button></header>
    <div class="min-h-0 space-y-4 overflow-y-auto p-5">
     <p v-if="error || data.error" role="alert" class="rounded-lg bg-red-500/10 p-3 text-sm text-red-500">{{error || data.error}}</p>
     <p v-if="!loading && !data.enabled" class="text-sm text-muted-foreground">当前工作区尚未配置服务器域名同步。</p>
     <template v-if="data.enabled">
      <form class="flex flex-wrap gap-2" @submit.prevent="change('POST',host)"><input v-model="host" aria-label="新增域名" placeholder="api.example.com" required class="h-10 min-w-40 flex-1 rounded-lg border border-border/60 bg-surface px-3 text-sm" /><button type="submit" :disabled="busy" class="inline-flex items-center gap-1 rounded-lg bg-primary px-3 text-sm text-primary-foreground disabled:opacity-50"><Plus class="h-4 w-4" />添加域名</button></form>
      <p class="text-sm text-muted-foreground">域名变更由 Sub2API 热加载，确认后显示为已生效。仍被账号引用的域名会保留。</p>
      <div class="flex items-center gap-3"><input v-model="search" aria-label="搜索域名" placeholder="搜索域名" class="h-9 min-w-0 flex-1 rounded-lg border border-border/60 bg-surface px-3 text-sm" /><span class="shrink-0 text-sm text-muted-foreground">{{items.length}} 个</span><button type="button" aria-label="刷新域名" @click="load"><RefreshCw class="h-4 w-4" /></button></div>
      <div class="overflow-x-auto rounded-lg border border-border/60"><table class="w-full text-sm"><thead class="bg-surface text-left text-muted-foreground"><tr><th class="px-3 py-2 font-medium">域名</th><th class="px-3 py-2 font-medium">来源</th><th class="px-3 py-2 font-medium">状态</th><th class="px-3 py-2"><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="item in items" :key="item.host" class="border-t border-border/40"><td class="px-3 py-2 font-mono">{{item.host}}</td><td class="max-w-52 px-3 py-2" :title="item.references.join('\n')"><span v-if="item.manual" class="mr-1 rounded bg-primary/10 px-1.5 py-0.5 text-xs text-primary">手动</span><span class="text-xs text-muted-foreground">{{item.references.slice(0,1).join('')}}{{item.references.length>1?' 等 '+item.references.length+' 项':''}}</span></td><td class="whitespace-nowrap px-3 py-2" :class="item.active?'text-emerald-600':'text-amber-600'">{{item.active?'已生效':'待生效'}}</td><td class="px-3 py-2"><button v-if="item.manual" type="button" :aria-label="'删除手动域名 '+item.host" :disabled="busy" class="rounded p-1 text-red-500 hover:bg-red-500/10 disabled:opacity-50" @click="change('DELETE',item.host)"><Trash2 class="h-4 w-4" /></button></td></tr></tbody></table></div>
     </template>
     <section v-if="data.jobs.length" class="space-y-2 border-t border-border/50 pt-4"><h3 class="text-sm font-medium">API 地址同步</h3><div v-for="job in data.jobs.slice(0,5)" :key="job.id" class="flex items-center justify-between gap-3 rounded-lg bg-surface px-3 py-2 text-sm"><span :class="job.failed?'text-red-500':'text-foreground'">{{job.status==='running'?'同步中':job.failed?'部分失败':'同步完成'}} · {{job.completed}}/{{job.total}}</span><span v-if="job.failed" class="min-w-0 flex-1 truncate text-xs text-red-500" :title="job.items.find(v=>v.error)?.error">{{job.items.find(v=>v.error)?.error}}</span><button v-if="job.failed" type="button" :disabled="busy" class="shrink-0 text-primary" @click="retry(job.id)">重试失败项</button></div></section>
    </div>
   </section>
  </div>
 </Teleport>
</template>
