<script setup lang="ts">
const props = defineProps<{
  label: string
  width: number
  min: number
  max: number
  controls?: string
}>()
const emit = defineEmits<{ resize: [width: number]; reset: [] }>()
let drag: { pointer: number; x: number; width: number } | undefined

function resize(width: number) {
  emit('resize', Math.max(props.min, Math.min(props.max, Math.round(width))))
}

function begin(event: PointerEvent) {
  if (!event.isPrimary || event.button !== 0) return
  const handle = event.currentTarget as HTMLElement
  handle.focus({ preventScroll: true })
  handle.setPointerCapture(event.pointerId)
  drag = { pointer: event.pointerId, x: event.clientX, width: props.width }
}

function move(event: PointerEvent) {
  if (drag?.pointer === event.pointerId) resize(drag.width + event.clientX - drag.x)
}

function end(event: PointerEvent) {
  if (drag?.pointer === event.pointerId) drag = undefined
}

function onKeydown(event: KeyboardEvent) {
  const step = event.shiftKey ? 32 : 8
  switch (event.key) {
    case 'ArrowLeft': resize(props.width - step); break
    case 'ArrowRight': resize(props.width + step); break
    case 'Home': resize(props.min); break
    case 'End': resize(props.max); break
    default: return
  }
  event.preventDefault()
}
</script>

<template>
  <div
    role="separator"
    tabindex="0"
    aria-orientation="vertical"
    :aria-label="label"
    :aria-controls="controls"
    :aria-valuemin="min"
    :aria-valuemax="max"
    :aria-valuenow="width"
    :title="label + '，双击恢复默认'"
    class="group/resize flex touch-none cursor-col-resize select-none items-center justify-center outline-none hover:bg-primary/10 focus-visible:bg-primary/15"
    @pointerdown.prevent="begin"
    @pointermove="move"
    @lostpointercapture="end"
    @keydown="onKeydown"
    @dblclick.stop="emit('reset')"
  >
    <span aria-hidden="true" class="pointer-events-none w-px bg-border group-hover/resize:w-0.5 group-hover/resize:bg-primary group-focus-visible/resize:w-0.5 group-focus-visible/resize:bg-primary" />
  </div>
</template>
