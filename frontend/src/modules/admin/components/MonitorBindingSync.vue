<script setup lang="ts">
import {ref} from 'vue'
import {operationJSON,followJob,type OperationJob} from '../api/operations'
const props=defineProps<{groups:{groupId:string;groupName:string}[]}>()
const emit=defineEmits<{done:[]}>()
interface Item {accountId:string;accountName:string;connectionId?:string;siteName?:string;groupName?:string;status:string;message?:string}
interface Result {matched:number;skipped:number;items:Item[]}
const sites=ref<{id:string;name:string;metrics:{groups:{id:string;name:string}[]}}[]>([]),siteId=ref(''),upstreamGroup=ref('')
async function show(){open.value=true;try{sites.value=await operationJSON('/upstream-sites')}catch(e:any){error.value=e.message}}
const open=ref(false),busy=ref(false),error=ref(''),result=ref<Result>(),apiUrl=ref(''),apiKey=ref(''),group=ref(''),manual=ref(false)
async function sync(byKey=false){
 if(busy.value)return
 busy.value=true;error.value='';result.value=undefined
 try{
  if(byKey){result.value=await operationJSON<Result>('/channel-monitor/bindings/key',{method:'POST',body:JSON.stringify({apiUrl:apiUrl.value,apiKey:apiKey.value,ownGroupIds:group.value?[group.value]:[],siteId:siteId.value,groupId:upstreamGroup.value})});apiKey.value=''}
  else{const job=await followJob(await operationJSON<OperationJob>('/channel-monitor/bindings/sync',{method:'POST'}));if(job.failed)throw Error(job.items.find(i=>i.error)?.error||'同步失败');result.value=job.items[0]?.result}
  emit('done')
 }catch(e:any){error.value=e.message}finally{busy.value=false}
}
function close(){if(busy.value)return;open.value=false;apiKey.value=''}
</script>
<template>
 <button type="button" class="h-10 rounded-xl border border-blue-500/20 bg-blue-500/10 px-3 text-sm font-medium text-blue-600 dark:text-blue-300" @click="show">同步监控</button>
 <Teleport to="body"><div v-if="open" class="fixed inset-0 z-[210] flex items-center justify-center bg-black/40 p-4" @click.self="close"><section role="dialog" aria-modal="true" aria-label="同步监控绑定" class="flex max-h-[85vh] w-full max-w-2xl flex-col gap-4 overflow-auto rounded-xl border border-border bg-surface p-5 shadow-xl">
  <div class="flex justify-between"><h3 class="font-semibold">同步监控</h3><button type="button" aria-label="关闭同步监控" :disabled="busy" @click="close">✕</button></div>
  <div class="flex gap-2"><button type="button" :disabled="busy" class="rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50" @click="sync()">{{busy?'同步中…':'同步已有渠道'}}</button><button type="button" :disabled="busy" class="rounded-lg border border-border px-3 py-2 text-sm" @click="manual=!manual">通过 API 和 Key 接入</button></div>
  <form v-if="manual" class="space-y-3 rounded-lg border border-border p-3" @submit.prevent="sync(true)">
   <input v-model.trim="apiUrl" type="url" required :disabled="busy" placeholder="API 地址" aria-label="接入监控 API 地址" class="w-full rounded-lg border border-border bg-background p-2 text-sm" />
   <input v-model.trim="apiKey" type="password" autocomplete="off" required :disabled="busy" placeholder="API Key" aria-label="接入监控 API Key" class="w-full rounded-lg border border-border bg-background p-2 font-mono text-sm" />
   <select v-model="group" :disabled="busy" aria-label="接入监控自有分组" class="w-full rounded-lg border border-border bg-background p-2 text-sm"><option value="">自动沿用自有渠道分组</option><option v-for="g in props.groups" :key="g.groupId" :value="g.groupId">{{g.groupName}}</option></select>
   <div class="flex gap-2"><select v-model="siteId" :disabled="busy" aria-label="手动指定上游站点" class="min-w-0 flex-1 rounded-lg border border-border bg-background p-2 text-sm" @change="upstreamGroup='' "><option value="">自动识别上游分组</option><option v-for="site in sites" :key="site.id" :value="site.id">{{site.name}}</option></select><select v-if="siteId" v-model="upstreamGroup" :disabled="busy" aria-label="手动指定上游分组" class="min-w-0 flex-1 rounded-lg border border-border bg-background p-2 text-sm"><option value="">自动匹配 Key 分组</option><option v-for="g in sites.find(s=>s.id===siteId)?.metrics.groups||[]" :key="g.id" :value="g.id">{{g.name}}</option></select></div>
   <button type="submit" :disabled="busy" class="rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50">识别并接入</button>
  </form>
  <p v-if="error" role="alert" class="text-sm text-red-500">{{error}}</p>
  <template v-if="result"><p role="status" class="text-sm">匹配 {{result.matched}} · 未匹配 {{result.skipped}}</p><div class="min-h-0 space-y-2 overflow-auto"><div v-for="(item,index) in result.items" :key="item.accountId||index" class="rounded-lg border border-border p-3 text-sm"><div class="flex justify-between gap-3"><span class="min-w-0 truncate">{{item.accountName||item.accountId}}</span><span class="shrink-0" :class="item.status==='matched'?'text-emerald-600':'text-amber-600'">{{item.status==='matched'?'已接入':item.status==='failed'?'处理失败':'未匹配'}}</span></div><div v-if="item.siteName" class="mt-1 text-muted-foreground">{{item.siteName}} · {{item.groupName}}</div><div v-if="item.message" class="mt-1 break-words text-xs text-muted-foreground">{{item.message}}</div></div></div></template>
 </section></div></Teleport>
</template>
