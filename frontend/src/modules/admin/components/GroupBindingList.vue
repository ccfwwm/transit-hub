<script setup lang="ts">
import { Plus } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import type { MySiteMappingOwnGroupOption, RealConnection } from '../types/mySites'

const props = defineProps<{
  connections: RealConnection[]
  ownGroups: MySiteMappingOwnGroupOption[]
  selectedId: string
  canAdd: boolean
  disabled?: boolean
}>()
const emit = defineEmits<{ select: [id: string] }>()

function ownGroupName(id: string) {
  return props.ownGroups.find(group => group.id === id)?.groupName ?? `分组 #${id}`
}
</script>

<template>
  <section aria-label="已有关联" class="space-y-3 rounded-xl border border-border/60 bg-surface/50 p-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h4 class="text-sm font-semibold">已绑定的自有分组（{{ connections.length }}）</h4>
      <Button v-if="canAdd" type="button" size="sm" :variant="selectedId ? 'default' : 'secondary'" :disabled="disabled" @click="emit('select', '')">
        <Plus class="h-4 w-4" />
        新增独立对接
      </Button>
    </div>
    <p v-if="!connections.length" class="text-sm text-muted-foreground">此上游分组暂无绑定</p>
    <ul v-else class="max-h-56 space-y-2 overflow-y-auto">
      <li v-for="connection in connections" :key="connection.id">
        <button
          type="button"
          :aria-pressed="selectedId === connection.id"
          :disabled="disabled"
          :class="selectedId === connection.id ? 'border-primary/50 bg-primary/10' : 'border-border/60 bg-card hover:border-primary/40'"
          class="flex w-full items-center justify-between gap-3 rounded-lg border px-3 py-2 text-left disabled:opacity-60"
          @click="emit('select', connection.id)"
        >
          <span class="min-w-0 break-words text-sm">
            <span class="font-medium">{{ (connection.ownGroupIds ?? []).map(ownGroupName).join(' / ') || '未分配自有分组' }}</span>
            <span v-if="connection.ownGroupIds?.length > 1" class="ml-2 text-xs text-warning">原共享绑定</span>
            <span class="mt-0.5 block text-xs text-muted-foreground">
              {{ connection.adminAccountName || '仅关联 Key' }}<template v-if="connection.upstreamKeyId"> · Key ID {{ connection.upstreamKeyId }}</template>
            </span>
          </span>
          <span class="shrink-0 text-xs text-primary">{{ selectedId === connection.id ? '正在配置' : '配置' }}</span>
        </button>
      </li>
    </ul>
  </section>
</template>
