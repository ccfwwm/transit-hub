<script setup lang="ts">
import {onMounted,ref,watch} from 'vue'
import {operationJSON} from '../api/operations'
const props=defineProps<{siteId:string;groupId:string;accountApiUrl?:string}>()
const management=ref(''),api=ref(''),custom=ref(''),busy=ref(false),error=ref(''),saved=ref(false)
const url=()=>`/upstream-sites/${encodeURIComponent(props.siteId)}/group-api-url?groupId=${encodeURIComponent(props.groupId)}`
async function load(){busy.value=true;error.value='';try{const v=await operationJSON<{managementUrl:string;apiUrl:string;customApiUrl:string}>(url());management.value=v.managementUrl;api.value=v.apiUrl;custom.value=v.customApiUrl||'';saved.value=false}catch(e:any){error.value=e.message}finally{busy.value=false}}
async function save(){busy.value=true;error.value='';saved.value=false;try{const v=await operationJSON<{apiUrl:string}>(url(),{method:'PUT',body:JSON.stringify({apiUrl:custom.value})});api.value=v.apiUrl;saved.value=true}catch(e:any){error.value=e.message}finally{busy.value=false}}
onMounted(load)
watch(()=>[props.siteId,props.groupId],load)
</script>
<template>
 <div class="mt-4 space-y-3 rounded-xl border border-border/60 p-3 text-sm" @click.stop>
  <label class="block">管理地址<input :value="management" readonly aria-label="分组管理地址" class="mt-1 w-full rounded-lg border border-border bg-surface p-2 text-muted-foreground" /></label>
  <label class="block">API 地址<input v-model.trim="custom" type="url" :placeholder="api||accountApiUrl||management" :disabled="busy" aria-label="分组 API 地址" class="mt-1 w-full rounded-lg border border-border bg-background p-2" @input="saved=false" /></label>
  <div class="flex items-center justify-between gap-3"><span v-if="error" role="alert" class="text-red-500">{{error}}</span><span v-else-if="saved" role="status" class="text-emerald-600">已保存，后台同步绑定账号</span><span v-else class="text-xs text-muted-foreground">留空使用上游默认 API 地址</span><button type="button" :disabled="busy" class="shrink-0 rounded-lg bg-primary px-3 py-2 text-primary-foreground disabled:opacity-50" @click="save">保存 API 地址</button></div>
 </div>
</template>
