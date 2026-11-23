<script setup lang="ts">
import type { KeyUsageSummary, UsagePoint } from '../../types/upstreamKeys'

const props = defineProps<{ usage?: KeyUsageSummary; loading: boolean; lifetime?: number | null; unit: string }>()
const format = (number: number) => number.toLocaleString('zh-CN', { maximumFractionDigits: 8 })
function amount(point?: UsagePoint) {
  if (!point || point.amount == null || (point.amount === 0 && props.usage?.state !== 'complete')) return '—'
  return format(point.amount) + ' ' + props.unit
}
</script>

<template>
  <div class="text-xs tabular-nums leading-relaxed" title="当前 Key 的上游消费，轮换历史见详情">
    <p v-if="loading" class="text-muted-foreground">读取消费中…</p>
    <p v-else-if="!usage || usage.state === 'unavailable'" class="text-muted-foreground">消费明细不可用</p>
    <template v-else>
      <p class="font-medium">今日 {{ amount(usage.today) }}<span v-if="usage.today.requests || usage.state === 'complete'" class="font-normal text-muted-foreground"> · {{ format(usage.today.requests) }} 次</span></p>
      <p class="text-muted-foreground">近 7 天 {{ amount(usage.week) }}<span v-if="usage.state === 'partial'" class="ml-1 text-warning" title="上游日志缺少 Key ID 或超出本次查询范围">部分数据</span></p>
    </template>
    <p v-if="typeof lifetime === 'number'" class="text-muted-foreground">累计 {{ format(lifetime) }} {{ unit }}</p>
  </div>
</template>
