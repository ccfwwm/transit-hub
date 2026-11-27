<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'

const props = defineProps<{ candidates: string[]; exactCatalog?: boolean; disabled?: boolean }>()
const models = defineModel<string[]>({ required: true })
const { t } = useI18n()
const query = ref('')
const options = computed(() => [...new Set([...models.value, ...props.candidates])]
  .filter(model => model.toLowerCase().includes(query.value.trim().toLowerCase())))
const newModel = computed(() => query.value.trim())
const canAdd = computed(() => !props.exactCatalog && !props.disabled && newModel.value.length > 0 &&
  newModel.value.length <= 256 && ![...models.value, ...props.candidates].includes(newModel.value))

function toggle(model: string) {
  models.value = models.value.includes(model) ? models.value.filter(value => value !== model) : [...models.value, model]
}

function addModel() {
  if (!canAdd.value) return
  toggle(newModel.value)
  query.value = ''
}
</script>

<template>
  <div class="space-y-2">
    <div class="flex items-center gap-2">
      <input
        v-model="query" :disabled="disabled" maxlength="256"
        :aria-label="t(exactCatalog ? 'admin.modelSettings.searchModels' : 'admin.modelSettings.searchOrAdd')"
        :placeholder="t(exactCatalog ? 'admin.modelSettings.searchModels' : 'admin.modelSettings.searchOrAdd')"
        class="h-9 min-w-0 flex-1 rounded-lg border border-border/60 bg-surface px-3 text-sm placeholder:text-muted-foreground"
        @keydown.enter.stop.prevent="addModel"
      />
      <Button v-if="!exactCatalog" size="sm" variant="secondary" :disabled="!canAdd" @click="addModel">
        {{ t('admin.modelSettings.addModel') }}
      </Button>
    </div>
    <div class="max-h-56 space-y-1 overflow-y-auto rounded-lg border border-border/60 p-1.5">
      <label v-for="model in options" :key="model" class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-sm hover:bg-surface">
        <input type="checkbox" :checked="models.includes(model)" :disabled="disabled" class="h-4 w-4 shrink-0 accent-primary" @change="toggle(model)" />
        <span class="min-w-0 break-all">{{ model }}</span>
      </label>
      <p v-if="!options.length" class="px-2 py-2 text-sm text-muted-foreground">{{ t('admin.modelSettings.noModels') }}</p>
    </div>
    <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-xs text-muted-foreground">
      <span>{{ t('admin.modelSettings.modelCount', { count: models.length }) }}</span>
      <span v-if="query.trim() && options.length">{{ t('admin.modelSettings.selectMatchingModels') }}</span>
    </div>
  </div>
</template>
