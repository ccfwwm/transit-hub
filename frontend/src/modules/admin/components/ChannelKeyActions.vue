<script setup lang="ts">
import { ref,watch } from 'vue'
import { operationJSON, followJob, type OperationJob } from '../api/operations'
const props = withDefaults(defineProps<{ connectionId: string; disabled?: boolean;whitelistEnabled?:boolean }>(),{whitelistEnabled:undefined})
const whitelistOn=ref(props.whitelistEnabled??true)
watch(()=>props.whitelistEnabled,value=>{if(value!=null)whitelistOn.value=value})
const busy = ref(false), message = ref(''), error = ref(false)
const buttonClass = 'h-6 whitespace-nowrap rounded px-1.5 text-[11px] font-medium disabled:opacity-50'
async function run(action: 'sync' | 'rotate') {
  busy.value = true; message.value = ''; error.value = false
  try {
    const job = await followJob(await operationJSON<OperationJob>(`/my-sites/key-security/${action}`, { method: 'POST', body: JSON.stringify({ connectionIds: [props.connectionId] }) }))
    if (job.failed) throw new Error(job.items.find(v => v.error)?.error || '操作失败，可在任务进度中重试')
    message.value = action === 'rotate' ? job.items[0]?.result?.message || 'Key 已轮换' : '白名单已校对'
    window.dispatchEvent(new Event('transithub:key-policy'))
  } catch (e: any) { error.value = true; message.value = e.message } finally { busy.value = false }
}
async function toggleWhitelist(){
 busy.value=true;message.value='';error.value=false
 try{const result=await operationJSON<{enabled:boolean;job:OperationJob}>(`/my-sites/real-connections/${encodeURIComponent(props.connectionId)}/key-whitelist`,{method:'PUT',body:JSON.stringify({enabled:!whitelistOn.value})});whitelistOn.value=result.enabled;const job=await followJob(result.job);if(job.failed)throw new Error(job.items.find(v=>v.error)?.error||'同步失败，请重试');message.value=whitelistOn.value?'白名单已启用':'白名单已关闭';window.dispatchEvent(new Event('transithub:key-policy'))}catch(e:any){error.value=true;message.value=e.message}finally{busy.value=false}
}
</script>
<template>
  <div class="min-w-0">
    <div class="flex items-center gap-1">
      <button type="button" :disabled="busy || disabled" :class="[buttonClass, 'bg-cyan-500/10 text-cyan-700 dark:text-cyan-300']" @click="run('sync')">{{ busy ? '处理中…' : '同步白名单' }}</button>
      <button type="button" :disabled="busy || disabled" :aria-label="whitelistOn ? '关闭账号白名单' : '启用账号白名单'" :class="[buttonClass, whitelistOn ? 'bg-teal-500/10 text-teal-700 dark:text-teal-300' : 'bg-slate-500/10 text-slate-500']" @click="toggleWhitelist">{{ whitelistOn ? '白名单开' : '白名单关' }}</button>
      <button type="button" :disabled="busy || disabled" :class="[buttonClass, 'bg-orange-500/10 text-orange-700 dark:text-orange-300']" @click="run('rotate')">轮换 Key</button>
      <slot />
    </div>
    <p v-if="message" role="status" :title="message" class="mt-0.5 truncate text-[11px]" :class="error ? 'text-red-500' : 'text-emerald-600'">{{ message }}</p>
  </div>
</template>
