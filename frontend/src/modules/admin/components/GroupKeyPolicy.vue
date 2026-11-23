<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { operationJSON, followJob, type OperationJob } from '../api/operations'
const props=withDefaults(defineProps<{siteId:string;groupId:string;groupName?:string;whitelistEnabled?:boolean;initialPolicy?:Policy}>(),{whitelistEnabled:undefined})
type Policy={allowedIPs:string[];version:number;inherited:boolean;observedIPs:string[];observedAt?:string;boundKeys:number}
const policy = ref<Policy>()
const open = ref(false), text = ref(''), inherited = ref(true), replace = ref(true)
const busy = ref(false), error = ref(''), message = ref('')
const url = computed(() => `/upstream-sites/${encodeURIComponent(props.siteId)}/group-key-policy?groupId=${encodeURIComponent(props.groupId)}`)
const label = computed(() => policy.value?.observedAt
  ? (policy.value.observedIPs.length ? policy.value.observedIPs.join(', ') : '未限制 IP')
  : props.whitelistEnabled===false ? '未限制 IP' : policy.value?.allowedIPs.join(', ') || (error.value?'读取失败':'读取中…'))
async function load() {
  policy.value = await operationJSON<Policy>(url.value)
}
async function refreshPolicy() {
  try { await load() } catch (e: any) { error.value = e.message }
}
async function show() {
  open.value = true; error.value = ''; message.value = ''; replace.value = true
  busy.value = true
  try {
    await load()
    text.value = policy.value!.allowedIPs.join('\n')
    inherited.value = policy.value!.inherited
  } catch (e: any) { error.value = e.message } finally { busy.value = false }
}
async function changeInheritance() {
  if (!inherited.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const defaults = await operationJSON<{allowedIPs: string[]}>(`/upstream-sites/${encodeURIComponent(props.siteId)}/site-key-policy`)
    text.value = defaults.allowedIPs.join('\n')
  } catch (e: any) { inherited.value = false; error.value = e.message } finally { busy.value = false }
}
function edit() { inherited.value = false; message.value = '' }
async function submit(readOnly = false) {
  if (!policy.value || busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const path = readOnly ? url.value.replace('/group-key-policy?', '/group-key-policy/refresh?') : url.value
    const result = await operationJSON<{policy: Policy; job?: OperationJob}>(path, {
      method: readOnly ? 'POST' : 'PUT',
      ...(!readOnly ? {body: JSON.stringify({
        allowedIPs: text.value.split(/[\s,]+/).filter(Boolean), version: policy.value.version,
        inherited: inherited.value, replace: replace.value,
      })} : {}),
    })
    policy.value = result.policy
    if (result.job) {
      message.value = readOnly ? '正在读取…' : '正在同步…'
      const job = await followJob(result.job)
      if (job.failed) throw new Error(job.items.filter(i => i.error).map(i => i.error).join('；'))
    }
    await load()
    if (readOnly) {
      text.value = policy.value!.observedIPs.join('\n')
      inherited.value = false
      message.value = '已读取，可直接修改'
    } else {
      text.value = policy.value!.observedAt ? policy.value!.observedIPs.join('\n') : policy.value!.allowedIPs.join('\n')
      inherited.value = policy.value!.inherited
      message.value = result.job ? '已保存并回读验证' : '已保存'
    }
    window.dispatchEvent(new Event('transithub:key-policy'))
  } catch (e: any) {
    error.value = e.message
    message.value = ''
    // A policy can have saved even when an upstream Key rejected the change.
    // Refresh its revision so a corrected retry does not use a stale version.
    await load().catch(() => {})
  } finally { busy.value = false }
}
onMounted(() => { if(props.initialPolicy)policy.value=props.initialPolicy;else void refreshPolicy(); window.addEventListener('transithub:key-policy', refreshPolicy) })
onUnmounted(() => window.removeEventListener('transithub:key-policy', refreshPolicy))
watch(() => props.initialPolicy, value=>{if(value&&!open.value)policy.value=value},{immediate:true})
watch(() => [props.siteId, props.groupId], () => {policy.value=props.initialPolicy;if(!props.initialPolicy)void refreshPolicy()})
</script>
<template>
 <div class="min-w-0" @click.stop @keydown.stop>
  <button type="button" class="flex max-w-full items-center gap-1 rounded px-1 py-0.5 text-[11px] text-muted-foreground hover:bg-primary/10 hover:text-primary" :title="`配置 IP：${policy?.allowedIPs.join(', ')||'读取中'}；${policy?.observedAt?'上次回读：'+label:'尚未回读上游'}；点击配置`" @click="show"><span class="shrink-0">IP</span><span class="truncate font-mono">{{label}}</span><span v-if="policy&&!policy.inherited" class="shrink-0 text-primary">独立</span><span class="shrink-0">✎</span></button>
  <Teleport to="body"><div v-if="open" class="fixed inset-0 z-[200] flex items-center justify-center bg-black/40 p-4" @click.self="!busy&&(open=false)"><section role="dialog" aria-modal="true" aria-label="分组 Key 白名单" class="w-full max-w-md space-y-4 rounded-xl border border-border bg-surface p-5 shadow-xl">
   <div class="flex items-center justify-between gap-3"><h3 class="font-semibold">{{groupName||'分组'}} · Key 白名单</h3><button type="button" :disabled="busy" aria-label="关闭分组白名单" @click="open=false">✕</button></div>
   <label class="flex items-center gap-2 text-sm"><input v-model="inherited" type="checkbox" :disabled="busy" @change="changeInheritance" />继承站点默认 IP</label>
   <textarea v-model="text" rows="3" :disabled="busy" @input="edit" class="w-full rounded-lg border border-border bg-background p-3 font-mono text-sm disabled:opacity-50" aria-label="分组允许的 IP 和 CIDR" />
   <label class="flex items-center gap-2 text-sm"><input v-model="replace" type="checkbox" :disabled="busy" />整体覆盖上游 IP</label>
   <div v-if="policy?.observedAt" class="break-words text-xs text-muted-foreground">上次回读地址：<span class="font-mono">{{policy.observedIPs.length?policy.observedIPs.join(', '):'未限制 IP'}}</span></div>
   <p v-if="error" role="alert" class="break-words text-sm text-red-500">{{error}}</p><p v-if="message" role="status" class="text-sm text-primary">{{message}}</p>
   <div class="flex justify-end gap-3"><button v-if="policy?.boundKeys" type="button" class="rounded-lg border border-border px-3 py-2 text-sm" :disabled="busy" @click="submit(true)">读取实际 IP</button><button type="button" class="rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50" :disabled="busy||!policy" @click="submit()">{{busy?'处理中…':policy?.boundKeys?'保存并同步':'保存分组策略'}}</button></div>
  </section></div></Teleport>
 </div>
</template>
