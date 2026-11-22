<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { Check, ChevronDown, RefreshCw, Search } from 'lucide-vue-next'
import { listBindingModels } from '../api/mySites'
const props = defineProps<{modelValue:string;siteId:string;groupId:string;connectionId?:string;keyId?:string;disabled?:boolean}>()
const emit = defineEmits<{ 'update:modelValue':[value:string] }>()
const items=ref<string[]>([]),loading=ref(false),warning=ref(''),search=ref(''),opened=ref(false),active=ref(0),limit=ref(200)
const placement=ref<'top'|'bottom'>('bottom'),menuHeight=ref(240)
const root=ref<HTMLElement>(),searchInput=ref<HTMLInputElement>(),trigger=ref<HTMLButtonElement>()
const controlId=`primary-model-${Math.random().toString(36).slice(2)}`
const filtered=computed(()=>items.value.filter(m=>m.toLowerCase().includes(search.value.trim().toLowerCase())))
const choices=computed(()=>[{value:'',label:'沿用分组设置'},...filtered.value.slice(0,limit.value).map(value=>({value,label:value})),...(search.value.trim()&&!items.value.includes(search.value.trim())?[{value:search.value.trim(),label:`使用模型 ID：${search.value.trim()}`}]:[])])
let request=0
async function load(){const current=++request;loading.value=true;warning.value='';try{const result=await listBindingModels({siteId:props.siteId,groupId:props.groupId,connectionId:props.connectionId,keyId:props.keyId});if(current!==request)return;items.value=result.items;warning.value=result.warning||''}catch{if(current===request)warning.value='模型列表读取失败，可刷新或输入模型 ID'}finally{if(current===request)loading.value=false}}
async function open(){if(props.disabled)return;let top=8,bottom=window.innerHeight-8;let parent=root.value?.parentElement;while(parent){if(/auto|scroll|hidden/.test(getComputedStyle(parent).overflowY)){const bounds=parent.getBoundingClientRect();top=Math.max(top,bounds.top);bottom=Math.min(bottom,bounds.bottom)}parent=parent.parentElement}const bounds=trigger.value?.getBoundingClientRect();if(bounds){const above=bounds.top-top,below=bottom-bounds.bottom;placement.value=below<280&&above>below?'top':'bottom';menuHeight.value=Math.max(80,Math.min(240,(placement.value==='top'?above:below)-58))}opened.value=true;search.value='';active.value=Math.max(0,choices.value.findIndex(m=>m.value===props.modelValue));await nextTick();searchInput.value?.focus();scrollActive()}
function close(){opened.value=false}
function choose(value:string){emit('update:modelValue',value);close();trigger.value?.focus()}
function scrollActive(){void nextTick(()=>root.value?.querySelector(`#${controlId}-option-${active.value}`)?.scrollIntoView({block:'nearest'}))}
function keydown(e:KeyboardEvent){if(e.key==='Escape'){e.preventDefault();e.stopPropagation();close();trigger.value?.focus();return}if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();if(!opened.value){void open();return}active.value=(active.value+(e.key==='ArrowDown'?1:-1)+choices.value.length)%choices.value.length;scrollActive()}else if(e.key==='Enter'&&opened.value){e.preventDefault();choose(choices.value[active.value]?.value||'')}else if(e.key==='Tab')close()}
function outside(e:PointerEvent){if(root.value&&!root.value.contains(e.target as Node))close()}
watch(search,()=>{limit.value=200;active.value=search.value.trim()?1:0})
watch(()=>[props.siteId,props.groupId,props.connectionId,props.keyId],()=>{items.value=[];close();void load()},{immediate:true})
watch(()=>props.disabled,value=>{if(value)close()})
onMounted(()=>document.addEventListener('pointerdown',outside))
onBeforeUnmount(()=>{request++;document.removeEventListener('pointerdown',outside)})
</script>
<template>
 <div ref="root" class="relative space-y-2" data-primary-model-picker @keydown="keydown" @focusout="e=>{if(!root?.contains(e.relatedTarget as Node))close()}">
  <div class="flex items-center justify-between gap-3"><label :for="controlId" class="text-sm font-medium">主检测模型</label><button type="button" :disabled="loading||disabled" class="inline-flex shrink-0 items-center gap-1 text-xs text-primary disabled:opacity-50" @click="load"><RefreshCw :class="['h-3 w-3',loading&&'animate-spin']" />{{loading?'读取中…':'刷新模型列表'}}</button></div>
  <button :id="controlId" ref="trigger" type="button" role="combobox" aria-label="主检测模型" aria-haspopup="listbox" :aria-expanded="opened" :aria-controls="controlId+'-list'" :disabled="disabled" class="flex h-10 w-full min-w-0 items-center justify-between gap-3 rounded-lg border border-border/60 bg-surface px-3 text-left text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary disabled:opacity-50" @click="opened?close():open()"><span class="min-w-0 truncate" :title="modelValue">{{modelValue||'沿用分组设置'}}</span><ChevronDown :class="['h-4 w-4 shrink-0 text-muted-foreground',opened&&'rotate-180']" /></button>
  <div v-if="opened" :class="['absolute left-0 right-0 z-30 overflow-hidden rounded-lg border border-border bg-card shadow-lg',placement==='top'?'bottom-full mb-1':'top-full mt-1']">
   <div class="relative border-b border-border/50"><Search class="absolute left-3 top-3 h-4 w-4 text-muted-foreground" /><input ref="searchInput" v-model="search" aria-label="搜索主检测模型" aria-autocomplete="list" :aria-controls="controlId+'-list'" :aria-activedescendant="controlId+'-option-'+active" placeholder="搜索或输入模型 ID" class="h-10 w-full bg-transparent pl-9 pr-3 text-sm outline-none" /></div>
   <div :id="controlId+'-list'" role="listbox" aria-label="主检测模型列表" :style="{maxHeight:menuHeight+'px'}" class="overflow-y-auto overscroll-contain p-1"><button v-for="(model,index) in choices" :id="controlId+'-option-'+index" :key="model.value" type="button" role="option" :aria-selected="model.value===modelValue" :class="['flex w-full items-center justify-between gap-3 rounded px-3 py-2 text-left text-sm',active===index?'bg-primary/10 text-primary':'hover:bg-surface']" @pointermove="active=index" @click="choose(model.value)"><span class="min-w-0 break-all">{{model.label}}</span><Check v-if="model.value===modelValue" class="h-4 w-4 shrink-0" /></button><p v-if="loading" class="px-3 py-2 text-xs text-muted-foreground">读取中…</p></div>
   <button v-if="filtered.length>limit" type="button" class="w-full border-t border-border/50 py-2 text-sm text-primary" @click="limit+=200">加载更多模型</button>
  </div>
  <p v-if="warning" class="text-xs text-amber-600">{{warning}}</p>
 </div>
</template>
