<script setup lang="ts">
import {onMounted,ref,computed} from 'vue'
import type {MonitorProfile,MonitorTemplate} from '../types/upstream'
import {operationJSON} from '../api/operations'
const props=defineProps<{modelValue?:MonitorProfile;disabled?:boolean}>()
const emit=defineEmits<{'update:modelValue':[MonitorProfile]}>()
const templates=ref<MonitorTemplate[]>([]),error=ref('')
const value=computed(()=>props.modelValue||{template:'auto',path:''})
const selected=computed(()=>templates.value.find(t=>t.template===value.value.template))
function select(id:string){const t=templates.value.find(t=>t.template===id);if(t)emit('update:modelValue',{template:t.template,path:t.path,detailPath:t.detailPath,pricingPath:t.pricingPath})}
function update(key:'path'|'detailPath'|'pricingPath',v:string){emit('update:modelValue',{...value.value,[key]:v})}
onMounted(async()=>{try{templates.value=await operationJSON<MonitorTemplate[]>('/upstream-monitor-templates')}catch(e:any){error.value=e.message}})
</script>
<template><fieldset :disabled="disabled" class="space-y-3 rounded-lg border border-border/50 bg-surface p-4"><legend class="px-1 text-sm font-medium">上游监控接口</legend><label class="block space-y-1 text-sm"><span>返回格式模板</span><select aria-label="监控返回格式模板" :value="value.template" class="w-full rounded-md border border-border/50 bg-background px-3 py-2" @change="select(($event.target as HTMLSelectElement).value)"><option v-if="!templates.length" value="auto">自动识别</option><option v-for="t in templates" :key="t.template" :value="t.template">{{t.name}}</option></select></label><template v-if="value.template!=='auto'"><label class="block space-y-1 text-sm"><span>状态路径</span><input :value="value.path" aria-label="监控状态路径" placeholder="/api/status" class="w-full rounded-md border border-border/50 bg-background px-3 py-2 font-mono text-xs" required @input="update('path',($event.target as HTMLInputElement).value)" /></label><label v-if="selected?.detailPath" class="block space-y-1 text-sm"><span>详情路径（可留空）</span><input :value="value.detailPath||''" aria-label="监控详情路径" :placeholder="selected.detailPath" class="w-full rounded-md border border-border/50 bg-background px-3 py-2 font-mono text-xs" @input="update('detailPath',($event.target as HTMLInputElement).value)" /></label><label v-if="selected?.pricingPath" class="block space-y-1 text-sm"><span>模型分组目录路径（可留空）</span><input :value="value.pricingPath||''" aria-label="监控模型目录路径" :placeholder="selected.pricingPath" class="w-full rounded-md border border-border/50 bg-background px-3 py-2 font-mono text-xs" @input="update('pricingPath',($event.target as HTMLInputElement).value)" /></label></template></fieldset></template>
