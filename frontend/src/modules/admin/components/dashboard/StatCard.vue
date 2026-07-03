<script setup lang="ts">
import { computed, type Component } from 'vue'
import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-vue-next'
import type { DashboardColorToken } from '../../types/dashboard'
import { DELTA_TEXT_CLASSES, METRIC_ICON_CLASSES, type DeltaDirection } from '../../utils/dashboard'

const props = defineProps<{
  label: string
  value: string
  icon: Component
  color: DashboardColorToken
  deltaDirection: DeltaDirection
  deltaText: string
  deltaCaption: string
  clickable?: boolean
}>()

const emit = defineEmits<{
  (event: 'click'): void
}>()

const iconClass = computed(() => METRIC_ICON_CLASSES[props.color])
const deltaClass = computed(() => DELTA_TEXT_CLASSES[props.deltaDirection])
const deltaIcon = computed(() => {
  if (props.deltaDirection === 'up') return ArrowUpRight
  if (props.deltaDirection === 'down') return ArrowDownRight
  return Minus
})
</script>

<template>
  <div
    class="bg-card border border-border/50 rounded-2xl p-6 shadow-sm"
    :class="{ 'cursor-pointer transition-shadow hover:shadow-md hover:border-primary/30': clickable }"
    :role="clickable ? 'button' : undefined"
    :tabindex="clickable ? 0 : undefined"
    @click="clickable && emit('click')"
    @keydown.enter="clickable && emit('click')"
  >
    <div class="flex items-start justify-between">
      <div class="min-w-0">
        <p class="text-sm font-medium text-muted-foreground">{{ label }}</p>
        <p class="mt-2 text-3xl font-bold text-foreground truncate">{{ value }}</p>
      </div>
      <div :class="['p-3 rounded-xl shrink-0', iconClass]">
        <component :is="icon" class="w-6 h-6" />
      </div>
    </div>
    <div class="mt-4 flex items-center gap-1.5 text-xs">
      <span v-if="deltaText" :class="['inline-flex items-center gap-0.5 font-semibold', deltaClass]">
        <component :is="deltaIcon" class="w-3.5 h-3.5" />
        {{ deltaText }}
      </span>
      <span class="text-muted-foreground">{{ deltaCaption }}</span>
    </div>
  </div>
</template>
