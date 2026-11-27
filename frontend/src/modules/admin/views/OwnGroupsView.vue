<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { Search } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { getMySiteMappingOptions, getOwnGroupSettings, updateOwnGroupSettings } from '../api/mySites'
import type { OwnGroupSettings } from '../api/mySites'
import ModelAllowlistPicker from '../components/ModelAllowlistPicker.vue'
import type { MySiteMappingOwnGroupOption } from '../types/mySites'

const { t, te } = useI18n()
const groups = ref<MySiteMappingOwnGroupOption[]>([])
const platform = ref('')
const query = ref('')
const selected = ref<MySiteMappingOwnGroupOption>()
const saved = ref<OwnGroupSettings>()
const status = ref<OwnGroupSettings['status']>('active')
const filterEnabled = ref(false)
const models = ref<string[]>([])
const loadingGroups = ref(true)
const loadingSettings = ref(false)
const saving = ref(false)
const result = ref('')
const listError = ref('')
const settingsError = ref('')
const editor = ref<HTMLElement>()
let selection = 0

const visible = computed(() => groups.value.filter(group =>
  `${group.groupName} ${group.platform}`.toLowerCase().includes(query.value.trim().toLowerCase())))
const allowlistChanged = computed(() => saved.value?.allowlist &&
  (filterEnabled.value !== saved.value.allowlist.enabled || JSON.stringify(models.value) !== JSON.stringify(saved.value.allowlist.models)))
const emptyAllowlist = computed(() => filterEnabled.value && models.value.length === 0)
const dirty = computed(() => !!saved.value && (status.value !== saved.value.status || !!allowlistChanged.value))
const canSave = computed(() => !!saved.value && !saving.value && !loadingSettings.value &&
  dirty.value && !emptyAllowlist.value)

function errorMessage(err: unknown) {
  const message = err instanceof Error ? err.message : ''
  return message ? te(message) ? t(message) : message : t('admin.mySites.errors.request')
}

function setSettings(settings: OwnGroupSettings) {
  saved.value = settings
  status.value = settings.status
  filterEnabled.value = settings.allowlist?.enabled ?? false
  models.value = [...(settings.allowlist?.models ?? [])]
}

async function loadGroups() {
  loadingGroups.value = true
  listError.value = ''
  try {
    const options = await getMySiteMappingOptions()
    platform.value = options.platform
    groups.value = options.ownGroups
    if (platform.value === 'sub2api' && groups.value[0]) void selectGroup(groups.value[0])
  } catch (err) {
    listError.value = errorMessage(err)
  } finally {
    loadingGroups.value = false
  }
}

async function selectGroup(group: MySiteMappingOwnGroupOption, scroll = false) {
  if (saving.value) return
  if (selected.value?.id === group.id && (saved.value || loadingSettings.value)) return
  if (dirty.value && selected.value?.id !== group.id && !window.confirm(t('admin.modelSettings.discardChanges'))) return
  const request = ++selection
  selected.value = group
  saved.value = undefined
  loadingSettings.value = true
  settingsError.value = ''
  result.value = ''
  if (scroll && window.matchMedia('(max-width: 1023px)').matches) {
    void nextTick(() => editor.value?.scrollIntoView({ block: 'nearest' }))
  }
  try {
    const settings = await getOwnGroupSettings(group.id)
    if (request === selection) {
      setSettings(settings)
      group.status = settings.status
    }
  } catch (err) {
    if (request === selection) settingsError.value = errorMessage(err)
  } finally {
    if (request === selection) loadingSettings.value = false
  }
}

async function save() {
  if (!selected.value || !saved.value || !canSave.value) return
  const group = selected.value
  const previous = saved.value
  saving.value = true
  result.value = ''
  settingsError.value = ''
  try {
    const settings = await updateOwnGroupSettings(group.id, {
      ...(previous.status !== status.value && { status: status.value }),
      ...(allowlistChanged.value && { allowlist: { enabled: filterEnabled.value, models: models.value } }),
    })
    setSettings({ ...settings, candidates: previous.candidates })
    group.status = settings.status
    result.value = t('admin.modelSettings.saved')
  } catch (err) {
    settingsError.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}

onMounted(loadGroups)
</script>

<template>
  <div class="mx-auto max-w-6xl p-4 sm:p-6">
    <p v-if="loadingGroups" class="text-sm text-muted-foreground">{{ t('admin.modelSettings.loading') }}</p>
    <div v-else-if="listError" class="space-y-3">
      <p role="alert" class="text-sm text-destructive">{{ listError }}</p>
      <Button size="sm" variant="secondary" @click="loadGroups">{{ t('admin.modelSettings.retry') }}</Button>
    </div>
    <p v-else-if="platform !== 'sub2api'" class="text-sm text-muted-foreground">{{ t('admin.modelSettings.workspaceUnsupported') }}</p>
    <div v-else class="grid items-start gap-4 lg:grid-cols-[minmax(250px,1fr)_minmax(0,1.55fr)]">
      <div class="overflow-hidden rounded-xl border border-border/60 bg-card">
        <label class="relative block border-b border-border/60">
          <Search class="pointer-events-none absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
          <input v-model="query" :aria-label="t('admin.modelSettings.groupSearch')" :placeholder="t('admin.modelSettings.groupSearch')" class="h-10 w-full bg-transparent pl-9 pr-3 text-sm placeholder:text-muted-foreground" />
        </label>
        <div class="max-h-72 space-y-1 overflow-y-auto p-1.5 lg:max-h-[65vh]">
          <button
            v-for="group in visible" :key="group.id" type="button" :disabled="saving"
            :aria-pressed="selected?.id === group.id"
            :class="['flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2.5 text-left', selected?.id === group.id ? 'bg-primary/10 text-primary' : 'hover:bg-surface']"
            @click="selectGroup(group, true)"
          >
            <span class="min-w-0">
              <span class="block break-words text-sm font-medium">{{ group.groupName }}</span>
              <span v-if="group.platform" class="block text-xs text-muted-foreground">{{ group.platform }}</span>
            </span>
            <span :class="['shrink-0 text-xs', group.status === 'inactive' ? 'text-muted-foreground' : 'text-emerald-700 dark:text-emerald-400']">
              {{ t(group.status === 'inactive' ? 'admin.modelSettings.inactive' : 'admin.modelSettings.active') }}
            </span>
          </button>
          <p v-if="!visible.length" class="p-3 text-sm text-muted-foreground">{{ t('admin.modelSettings.groupEmpty') }}</p>
        </div>
      </div>
      <section v-if="selected" ref="editor" class="min-w-0 scroll-mt-4 rounded-xl border border-border/60 bg-card p-4 sm:p-5">
        <h2 class="break-words text-base font-semibold">{{ selected.groupName }}</h2>
        <p v-if="loadingSettings" class="mt-3 text-sm text-muted-foreground">{{ t('admin.modelSettings.loading') }}</p>
        <p v-if="settingsError" role="alert" class="mt-3 text-sm text-destructive">{{ settingsError }}</p>
        <Button v-if="!loadingSettings && !saved" class="mt-3" size="sm" variant="secondary" @click="selectGroup(selected)">{{ t('admin.modelSettings.retry') }}</Button>
        <form v-if="saved" class="mt-4 space-y-4" @submit.prevent="save">
          <label class="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-surface px-3 py-2.5 text-sm font-medium">
            {{ t('admin.modelSettings.groupEnabled') }}
            <input v-model="status" type="checkbox" true-value="active" false-value="inactive" :disabled="saving" class="h-4 w-4 accent-primary" />
          </label>
          <fieldset v-if="saved.allowlist" class="space-y-3" :disabled="saving">
            <legend class="text-sm font-medium">{{ t('admin.modelSettings.groupAllowlist') }}</legend>
            <p class="text-xs text-muted-foreground">{{ t('admin.modelSettings.groupHelp') }}</p>
            <label class="flex items-center gap-2 text-sm">
              <input v-model="filterEnabled" type="checkbox" class="h-4 w-4 accent-primary" />
              {{ t('admin.modelSettings.onlySelected') }}
            </label>
            <ModelAllowlistPicker v-if="filterEnabled" :key="selected.id" v-model="models" :candidates="saved.candidates ?? []" :disabled="saving" />
          </fieldset>
          <p v-else class="text-sm text-muted-foreground">{{ t('admin.modelSettings.groupUnsupported') }}</p>
          <div class="flex flex-wrap items-center gap-3">
            <Button type="submit" :variant="dirty ? 'default' : 'secondary'" :disabled="!canSave">{{ t(saving ? 'admin.modelSettings.saving' : 'admin.modelSettings.saveGroup') }}</Button>
            <span v-if="result && !dirty" role="status" class="text-sm text-muted-foreground">{{ result }}</span>
            <span v-else-if="emptyAllowlist" class="text-sm text-muted-foreground">{{ t('admin.modelSettings.selectAtLeastOne') }}</span>
          </div>
        </form>
      </section>
    </div>
  </div>
</template>
