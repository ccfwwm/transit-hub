<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { KeyRound, Plus, RefreshCw, Search } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { operationJSON } from '../api/operations'
import UpstreamKeyDetail from '../components/upstream/UpstreamKeyDetail.vue'
import UpstreamKeyMigration from '../components/upstream/UpstreamKeyMigration.vue'
import type { KeyEntry, KeyInventory, KeySite } from '../types/upstreamKeys'

const inventory = ref<KeyInventory>({ sites: [], items: [], ownGroups: [] })
const siteFilter = ref('')
const platformFilter = ref('')
const groupFilter = ref('')
const connectionFilter = ref('')
const statusFilter = ref('')
const search = ref('')
const selected = ref<KeyEntry | null>(null)
const creating = ref(false)
const migrating = ref(false)
const migrationBinding = ref("")
const migrationSite = ref("")
function showMigration(site = siteFilter.value, binding = "") { migrationSite.value = site; migrationBinding.value = binding; migrating.value = true }
const busy = ref(false)
const error = ref('')
const page = ref(1)
const pageSize = 25
const ready = ref(false)
const sites = computed(() => Object.fromEntries(inventory.value.sites.map(site => [site.id, site])) as Record<string, KeySite>)
const load = async () => {
  busy.value = true
  error.value = ''
  try {
    inventory.value = await operationJSON<KeyInventory>('/upstream-keys')
    ready.value = true
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}
onMounted(load)
const groupOptions = computed(() => [...new Set(inventory.value.items.map(key => key.groupName || key.groupId).filter(Boolean))].sort())
const enabled = (key: KeyEntry) => ['active', 'enabled', '1'].includes(key.status)
const statusLabel = (key: KeyEntry) => enabled(key) ? '启用' : ({ disabled: '停用', inactive: '停用', '2': '停用', expired: '已过期', '3': '已过期', '4': '额度耗尽', quota_exhausted: '额度耗尽' }[key.status] || key.status || '未知')
const filtered = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return inventory.value.items.filter(key => {
    if (siteFilter.value && key.siteId !== siteFilter.value) return false
    if (platformFilter.value && sites.value[key.siteId]?.platform !== platformFilter.value) return false
    if (groupFilter.value && (key.groupName || key.groupId) !== groupFilter.value) return false
    if (connectionFilter.value && Boolean(key.bindings.length) !== (connectionFilter.value === 'bound')) return false
    if (statusFilter.value && statusLabel(key) !== statusFilter.value) return false
    return !query || [key.name, key.groupName, key.groupId, sites.value[key.siteId]?.name, ...key.bindings.flatMap(item => [item.account, ...item.groups])].some(name => name?.toLocaleLowerCase().includes(query))
  })
})
const pageCount = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)))
const items = computed(() => filtered.value.slice((Math.min(page.value, pageCount.value) - 1) * pageSize, Math.min(page.value, pageCount.value) * pageSize))
const failures = computed(() => inventory.value.sites.filter(site => site.error))
function date(value?: string | number | null) {
  if (!value || value === -1) return '—'
  const date = new Date(typeof value === 'number' ? value * 1000 : value)
  return Number.isFinite(date.valueOf()) ? date.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) : '—'
}
function quota(key: KeyEntry) {
  if (key.config.unlimited_quota === true || key.config.quota === 0) return '不限'
  if (typeof key.config.remain_quota === 'number') return key.config.remain_quota.toLocaleString() + ' 额度'
  if (typeof key.config.quota === 'number') return 'USD ' + key.config.quota.toLocaleString()
  return '—'
}
</script>

<template>
  <section class="min-w-0 space-y-4">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <p class="flex items-center gap-2 text-sm font-medium text-muted-foreground"><KeyRound class="size-4" />{{ filtered.length }} 个 Key</p>
      <div class="flex flex-wrap gap-2">
        <Button variant="secondary" :disabled="busy" @click="load"><RefreshCw class="size-4" :class="{ 'animate-spin': busy }" />刷新列表</Button>
        <Button variant="secondary" @click="showMigration()">命名与拆分</Button>
        <Button :disabled="!inventory.sites.length" @click="creating = true"><Plus class="size-4" />创建 Key</Button>
      </div>
    </header>
    <p v-if="error" role="alert" class="rounded-lg bg-destructive/10 px-4 py-3 text-sm text-destructive">{{ error }}</p>
    <details v-if="failures.length" class="rounded-lg border border-border bg-card px-4 py-3 text-sm">
      <summary class="cursor-pointer text-warning">{{ failures.length }} 个站点暂未返回 Key</summary>
      <ul class="mt-2 space-y-1 text-muted-foreground"><li v-for="site in failures" :key="site.id">{{ site.name }} · {{ site.error }}</li></ul>
    </details>
    <section aria-label="Key 筛选" class="flex flex-wrap items-center gap-2">
      <div class="relative w-full flex-1 basis-48"><Search class="absolute left-3 top-3 size-4 text-muted-foreground" /><Input v-model="search" placeholder="搜索名称、站点或分组" aria-label="搜索 Key" class="pl-9" @input="page = 1" /></div>
      <select v-model="siteFilter" aria-label="站点" @change="page = 1"><option value="">所有站点</option><option v-for="site in inventory.sites" :key="site.id" :value="site.id">{{ site.name }}</option></select>
      <select v-model="platformFilter" aria-label="平台" @change="page = 1"><option value="">所有平台</option><option value="sub2api">Sub2API</option><option value="newapi">New API</option></select>
      <select v-model="groupFilter" aria-label="上游分组" @change="page = 1"><option value="">所有分组</option><option v-for="group in groupOptions" :key="group">{{ group }}</option></select>
      <select v-model="connectionFilter" aria-label="对接状态" @change="page = 1"><option value="">所有对接状态</option><option value="bound">已对接</option><option value="independent">独立 Key</option></select>
      <select v-model="statusFilter" aria-label="启用状态" @change="page = 1"><option value="">所有启用状态</option><option>启用</option><option>停用</option><option>已过期</option><option>额度耗尽</option></select>
    </section>
    <section class="overflow-hidden rounded-xl border border-border bg-card">
      <div class="hidden overflow-x-auto lg:block">
        <table class="w-full min-w-[820px] text-left text-sm">
          <thead class="bg-muted/40 text-xs text-muted-foreground"><tr><th scope="col">Key</th><th scope="col">上游分组</th><th scope="col">自有 Sub2API 对接</th><th scope="col">状态</th><th scope="col">额度／到期</th><th scope="col">最近使用（北京时间）</th><th scope="col"><span class="sr-only">操作</span></th></tr></thead>
          <tbody class="divide-y divide-border/60">
            <tr v-for="key in items" :key="key.siteId + '/' + key.id" class="hover:bg-muted/20">
              <td><button class="break-all text-left font-medium text-primary hover:underline" @click="selected = key">{{ key.name }}</button><p class="mt-1 text-xs text-muted-foreground">{{ sites[key.siteId]?.name }} · {{ sites[key.siteId]?.platform === 'newapi' ? 'New API' : 'Sub2API' }}</p></td>
              <td>{{ key.groupName || key.groupId || '—' }}</td>
              <td>
                <span v-if="!key.bindings.length" class="text-muted-foreground">独立 Key</span>
                <p v-for="binding in key.bindings" v-else :key="binding.id" class="max-w-64 break-words">
                  {{ binding.groups.join(' / ') }} <span v-if="binding.update" class="text-xs text-warning">命名待更新</span>
                  <span class="block text-xs text-muted-foreground">{{ binding.account }}</span>
                </p>
              </td>
              <td><span><span v-if="enabled(key)" aria-hidden="true" class="mr-1 inline-block size-1.5 rounded-full bg-signal"></span>{{ statusLabel(key) }}</span></td>
              <td>{{ quota(key) }}<p class="mt-1 text-xs text-muted-foreground">{{ date(key.config.expires_at as string || key.config.expired_time as number) }}</p></td>
              <td class="tabular-nums text-muted-foreground">{{ date(key.lastUsed) }}</td>
              <td><Button variant="ghost" size="sm" @click="selected = key">详情</Button></td>
            </tr>
            <tr v-if="!items.length"><td colspan="7" class="py-14 text-center text-muted-foreground">{{ busy && !ready ? '读取 Key 清单…' : '当前筛选没有 Key' }}</td></tr>
          </tbody>
        </table>
      </div>
      <ul class="divide-y divide-border/60 lg:hidden" aria-label="Key 列表">
        <li v-for="key in items" :key="key.siteId + '/' + key.id" class="flex items-start justify-between gap-2 p-3">
          <div class="min-w-0">
            <button class="break-all text-left text-sm font-medium text-primary hover:underline" @click="selected = key">{{ key.name }}</button>
            <p class="mt-1 text-xs text-muted-foreground">{{ sites[key.siteId]?.name }} · {{ key.groupName || key.groupId || '—' }}</p>
            <p class="mt-1 text-xs text-foreground">{{ statusLabel(key) }} · {{ quota(key) }}</p>
          </div>
          <Button variant="ghost" size="sm" @click="selected = key">详情</Button>
        </li>
        <li v-if="!items.length" class="p-6 text-center text-sm text-muted-foreground">{{ busy && !ready ? '读取 Key 清单…' : '当前筛选没有 Key' }}</li>
      </ul>
      <footer class="flex flex-wrap items-center justify-between gap-2 border-t border-border px-4 py-3 text-sm">
        <span class="text-muted-foreground">{{ filtered.length }} 个 Key</span>
        <div class="flex items-center gap-2"><Button variant="secondary" size="sm" :disabled="page <= 1" @click="page--">上一页</Button><span class="tabular-nums">{{ Math.min(page, pageCount) }} / {{ pageCount }}</span><Button variant="secondary" size="sm" :disabled="page >= pageCount" @click="page++">下一页</Button></div>
      </footer>
    </section>
    <UpstreamKeyDetail v-if="creating || selected" :entry="selected" :inventory="inventory" :initial-site="siteFilter" @close="selected = null; creating = false" @changed="load" @migrate="(site, binding) => { selected = null; showMigration(site, binding) }" />
    <UpstreamKeyMigration v-if="migrating" :sites="inventory.sites" :initial-site="migrationSite" :initial-binding="migrationBinding" @close="migrating = false" @changed="load" />
  </section>
</template>

<style scoped>
select { @apply h-10 max-w-48 rounded-md border border-border bg-background px-3 text-sm; }
th, td { @apply p-3 align-top; }
th { @apply font-medium; }
</style>
