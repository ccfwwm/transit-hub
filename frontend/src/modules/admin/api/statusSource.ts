import { ref } from 'vue'
import { operationJSON } from './operations'
export type StatusSource = 'upstream' | 'active'
export const monitorStatusSource = ref<StatusSource>('upstream')
export const statusSourceBusy = ref(false)
let pending:Promise<void>|undefined
export async function loadStatusSource(){
 if(pending)return pending
 pending=operationJSON<{source:StatusSource}>('/channel-monitor/status-source').then(v=>{monitorStatusSource.value=v.source}).finally(()=>{pending=undefined})
 return pending
}
export async function saveStatusSource(source:StatusSource){
 statusSourceBusy.value=true
 try{const v=await operationJSON<{source:StatusSource}>('/channel-monitor/status-source',{method:'PATCH',body:JSON.stringify({source})});monitorStatusSource.value=v.source}finally{statusSourceBusy.value=false}
}
