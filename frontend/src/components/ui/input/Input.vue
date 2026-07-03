<script setup lang="ts">
import { computed } from 'vue'
import { cn } from '@/lib/utils'

const props = withDefaults(
  defineProps<{
    modelValue: string
    type?: string
    placeholder?: string
    autocomplete?: string
  }>(),
  {
    type: 'text',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const value = computed({
  get: () => props.modelValue,
  set: (nextValue: string) => emit('update:modelValue', nextValue),
})
</script>

<template>
  <input
    v-model="value"
    :type="type"
    :placeholder="placeholder"
    :autocomplete="autocomplete"
    :class="cn('h-11 w-full rounded-xl border border-border/70 bg-surface px-4 text-sm text-foreground outline-none transition placeholder:text-muted-foreground focus:border-primary')"
  />
</template>
