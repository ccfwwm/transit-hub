<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {monitorStatusSource,statusSourceBusy,loadStatusSource,saveStatusSource,type StatusSource} from '../api/statusSource'
const error=ref('')
onMounted(()=>{void loadStatusSource().catch(e=>{error.value=e.message})})
async function change(source:StatusSource){error.value='';try{await saveStatusSource(source)}catch(e:any){error.value=e.message}}
</script>
<template><div class="flex flex-wrap items-center gap-3"><span class="text-sm text-muted-foreground">{{monitorStatusSource==='upstream'?'获取上游状态':'主动检测 · 每组一个模型'}}</span><button type="button" role="switch" aria-label="启用主动检测" :aria-checked="monitorStatusSource==='active'" :disabled="statusSourceBusy" class="inline-flex items-center gap-2 rounded-md px-2 py-1 text-sm disabled:opacity-50" @click="change(monitorStatusSource==='active'?'upstream':'active')"><span :class="['relative h-5 w-9 rounded-full transition-colors',monitorStatusSource==='active'?'bg-primary':'bg-slate-300 dark:bg-slate-600']"><span :class="['absolute top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform',monitorStatusSource==='active'?'left-0.5 translate-x-4':'left-0.5']" /></span>主动检测</button><span v-if="error" class="text-sm text-red-500">{{error}}</span></div></template>
