<script setup lang="ts">
import { computed } from 'vue'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 rounded-xl text-sm font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-accent',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-surface-line',
        destructive: 'bg-red-500 text-white hover:bg-red-600 dark:bg-red-600 dark:hover:bg-red-700',
        ghost: 'bg-transparent text-foreground hover:bg-surface-elevated',
      },
      size: {
        default: 'h-10 px-4 py-2',
        sm: 'h-9 px-3',
        lg: 'h-11 px-6',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

const props = withDefaults(
  defineProps<{
    type?: 'button' | 'submit' | 'reset'
    variant?: VariantProps<typeof buttonVariants>['variant']
    size?: VariantProps<typeof buttonVariants>['size']
  }>(),
  {
    type: 'button',
    variant: 'default',
    size: 'default',
  },
)

const classes = computed(() => cn(buttonVariants({ variant: props.variant, size: props.size })))
</script>

<template>
  <button :type="type" :class="classes">
    <slot />
  </button>
</template>
