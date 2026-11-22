<script setup lang="ts">
import { computed } from 'vue'
export interface StatusPoint {status:string;checkedAt?:string|null;latencyMs?:number|null;pingMs?:number|null;throughput?:number|null}
const props=defineProps<{points?:StatusPoint[];status?:string;latencyMs?:number|null;pingMs?:number|null;availability?:number|null;throughput?:number|null;hideDetails?:boolean;hideMetrics?:boolean}>()
const emit=defineEmits<{details:[]}>()
const samples=computed(()=>props.points?.slice(-60)||[])
const rate=computed(()=>{if(props.availability!=null)return props.availability.toFixed(1)+'%';const known=samples.value.filter(p=>['healthy','failed','auto_paused'].includes(p.status));return known.length?(100*known.filter(p=>p.status==='healthy').length/known.length).toFixed(1)+'%':'—'})
const bars=computed(()=>{const actual=samples.value.length?samples.value:props.status&&props.status!=='unknown'?[{status:props.status}]:[];return [...Array.from({length:60-actual.length},()=>({status:'unknown'})),...actual]})
function tone(status:string){return status==='healthy'?'bg-emerald-500':status==='failed'||status==='auto_paused'?'bg-red-500':status==='warning'||status==='balance_paused'?'bg-amber-400':'bg-slate-200 dark:bg-slate-600'}
function title(p:StatusPoint){return ({healthy:'正常',failed:'故障',warning:'降级',unknown:'未报告',auto_paused:'故障'} as Record<string,string>)[p.status]||p.status}
function height(status:string){return status==='failed'||status==='auto_paused'?'35%':status==='warning'||status==='balance_paused'?'65%':'100%'}
</script>
<template><div class="min-w-0 space-y-1.5"><div v-if="!hideMetrics" class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-xs text-muted-foreground"><span class="font-mono">{{rate}}</span><div class="flex items-center gap-3"><span v-if="latencyMs!=null" class="font-mono">{{(latencyMs/1000).toFixed(2)}}s</span><span v-if="pingMs!=null" class="font-mono">{{pingMs}}ms</span><span v-if="throughput!=null" class="font-mono">{{throughput.toFixed(1)}} t/s</span><button v-if="!hideDetails" type="button" class="text-primary" @click="emit('details')">详情 ›</button></div></div><div class="grid h-5 w-full min-w-0 items-end gap-[2px] overflow-hidden" style="grid-template-columns:repeat(60,minmax(0,1fr))" aria-label="模型状态时间线"><span v-for="(point,i) in bars" :key="i" :title="title(point)" :class="['min-w-0 rounded-[1px]',tone(point.status)]" :style="{height:height(point.status),opacity:point.status==='unknown'?0.45:1}" /></div></div></template>
