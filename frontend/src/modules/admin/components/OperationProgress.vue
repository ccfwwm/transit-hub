<script setup lang="ts">
import { onMounted } from 'vue'
import { operationJSON, followJob, type OperationJob, operationJobs, retryJob } from '../api/operations'
onMounted(async()=>{try{const jobs=await operationJSON<OperationJob[]>('/channel-monitor/operations');operationJobs.value=jobs;for(const job of jobs){if(job.status==='running')void followJob(job).catch(()=>{})}}catch{}})
const labels: Record<string, string> = { membership_priority: '账号变动自动优先级', key_security_bindings: 'Key 绑定发现', key_security: 'Key 安全', key_security_scan: 'Key 白名单发现', monitor: '渠道操作', health_preview: '连接预览', health_probe: '模型预检', health_refresh: '状态同步', model_catalog: '模型目录' }
</script>
<template>
  <details v-if="operationJobs.length" class="rounded-xl border border-border/50 bg-surface px-3 py-2 text-sm">
    <summary class="cursor-pointer font-medium">任务进度<span v-if="operationJobs.some(job=>job.status==='running')" class="ml-2 text-xs text-primary">后台执行中</span></summary>
    <div v-for="job in operationJobs.slice(0, 5)" :key="job.id" class="mt-3 border-t border-border/40 pt-2">
      <div class="flex items-center gap-3"><span>{{ labels[job.kind] || job.kind }}</span><span>{{ job.completed + job.failed }} / {{ job.total }}</span><span :class="job.failed ? 'text-red-500' : 'text-muted-foreground'">{{ job.status === 'running' ? '后台执行中' : job.failed ? `${job.failed} 项失败` : '已完成' }}</span><button v-if="job.failed" type="button" class="text-primary" @click="retryJob(job.id)">重试失败项</button></div>
      <details v-if="job.items.some(item => item.error || item.result)" class="mt-2 text-xs text-muted-foreground"><summary class="cursor-pointer">逐项结果</summary><div class="max-h-40 overflow-auto"><div v-for="item in job.items" :key="item.id" class="mt-1 break-all">{{ item.id }} · {{ item.status }} · {{ item.error || item.result?.message || item.result?.status || (item.result?.success ? '检测通过' : '') }}</div></div></details>
    </div>
  </details>
</template>
