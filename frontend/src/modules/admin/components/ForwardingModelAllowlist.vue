<script setup lang="ts">
import { computed, ref } from 'vue'
import { ChevronDown } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import ModelAllowlistPicker from './ModelAllowlistPicker.vue'
import { getForwardingModelSettings, listBindingModels, updateForwardingModelSettings } from '../api/mySites'
import type { ForwardingModelSettings } from '../api/mySites'

const props = defineProps<{ connectionId: string; siteId: string; groupId: string; disabled?: boolean }>()
const { t, te } = useI18n()
const expanded = ref(false)
const loading = ref(false)
const saving = ref(false)
const saved = ref<ForwardingModelSettings>()
const candidates = ref<string[]>([])
const enabled = ref(false)
const models = ref<string[]>([])
const error = ref('')
const notice = ref('')
const isMedia = computed(() => saved.value?.mediaModels !== undefined)
const changed = computed(() => saved.value && (enabled.value !== saved.value.enabled ||
  enabled.value && (models.value.length !== saved.value.models.length || models.value.some(model => !saved.value?.models.includes(model)))))
const canSave = computed(() => !!saved.value && !props.disabled && !loading.value && !saving.value &&
  !!changed.value && (isMedia.value ? candidates.value.length > 0 : !enabled.value || models.value.length > 0))

function setSaved(settings: ForwardingModelSettings) {
  saved.value = settings
  enabled.value = settings.enabled
  models.value = [...settings.models]
}

function showError(err: unknown) {
  const message = err instanceof Error ? err.message : ''
  error.value = message ? te(message) ? t(message) : message : t('admin.mySites.errors.request')
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const settings = await getForwardingModelSettings(props.connectionId)
    setSaved(settings)
    if (settings.mediaModels) {
      candidates.value = settings.mediaModels
    } else {
      void listBindingModels(props).then(result => {
        candidates.value = result.items
      }).catch(() => {})
    }
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

function toggleExpanded() {
  expanded.value = !expanded.value
  if (expanded.value && !saved.value && !loading.value) void load()
}

async function save() {
  if (!canSave.value) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    setSaved(await updateForwardingModelSettings(props.connectionId, { enabled: enabled.value, models: models.value }))
    notice.value = t('admin.modelSettings.saved')
  } catch (err) {
    showError(err)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="rounded-xl border border-border/60 bg-surface">
    <button type="button" :aria-expanded="expanded" :disabled="disabled" class="flex w-full items-center justify-between gap-2 px-4 py-3 text-left text-sm font-medium" @click="toggleExpanded">
      {{ t('admin.modelSettings.accountTitle') }}
      <ChevronDown :class="['h-4 w-4 shrink-0 text-muted-foreground', expanded && 'rotate-180']" />
    </button>
    <div v-if="expanded" class="space-y-3 border-t border-border/50 p-4">
      <p v-if="loading" class="text-sm text-muted-foreground">{{ t('admin.modelSettings.loading') }}</p>
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
      <Button v-if="!saved && !loading" size="sm" variant="secondary" :disabled="disabled" @click="load">{{ t('admin.modelSettings.retry') }}</Button>
      <template v-if="saved">
        <label v-if="!isMedia" class="flex items-center gap-2 text-sm">
          <input v-model="enabled" type="checkbox" class="h-4 w-4 accent-primary" :disabled="disabled || saving" />
          {{ t('admin.modelSettings.onlySelected') }}
        </label>
        <p v-if="isMedia" class="text-sm text-muted-foreground">
          {{ t(candidates.length ? 'admin.modelSettings.mediaHelp' : 'admin.modelSettings.mediaEmpty') }}
        </p>
        <ModelAllowlistPicker v-if="(enabled && !isMedia) || (isMedia && candidates.length)" v-model="models" :candidates="candidates" :exact-catalog="isMedia" :disabled="disabled || saving" />
        <div class="flex flex-wrap items-center gap-3">
          <Button size="sm" :disabled="!canSave" @click="save">{{ t(saving ? 'admin.modelSettings.saving' : 'admin.modelSettings.saveAccount') }}</Button>
          <span v-if="notice && !changed" role="status" class="text-sm text-muted-foreground">{{ notice }}</span>
          <span v-else-if="enabled && !isMedia && !models.length" class="text-sm text-muted-foreground">{{ t('admin.modelSettings.selectAtLeastOne') }}</span>
        </div>
      </template>
    </div>
  </section>
</template>
