<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Button } from '@/components/ui/button'
import { followJob, operationJSON, type OperationJob } from '../../api/operations'
import { keyJobMessage } from '../../api/keyRotation'
import { syncKeyWhitelist } from '../../api/keyWhitelist'
import type { KeyEntry } from '../../types/upstreamKeys'

const props = defineProps<{
  siteId: string
  name: string
  platform: string
  connectionIds: string[]
  entry?: KeyEntry
}>()
const emit = defineEmits<{ close: []; changed: [] }>()
const title = computed(() => props.entry ? 'Key 白名单' : '站点白名单')
const sitePath = '/upstream-sites/' + encodeURIComponent(props.siteId) + '/site-key-policy'
const keyPath = '/upstream-keys/' + encodeURIComponent(props.siteId) + '/' + encodeURIComponent(props.entry?.id ?? '')
const field = props.platform === 'newapi' ? 'allow_ips' : 'ip_whitelist'
type SitePolicy = { allowedIPs: string[]; inherited: boolean; version: number }
const policy = ref<SitePolicy>()
const addresses = ref('')
const inherited = ref(false)
const busy = ref(false)
const loaded = ref(false)
const error = ref('')
const message = ref('')

function applyPolicy(result: SitePolicy) {
  policy.value = result
  addresses.value = result.allowedIPs.join('\n')
  inherited.value = result.inherited
}

async function run(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await action()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function read() {
  if (props.entry) {
    const result = await operationJSON<KeyEntry>(keyPath)
    const ips = result.config[field]
    addresses.value = Array.isArray(ips) ? ips.join('\n') : String(ips ?? '')
  } else {
    applyPolicy(await operationJSON<SitePolicy>(sitePath))
  }
  loaded.value = true
}

async function synchronize() {
  const job = props.entry
    ? await syncKeyWhitelist({ connectionIds: props.connectionIds })
    : await followJob(await operationJSON<OperationJob>('/my-sites/key-security/site-whitelist', {
      method: 'POST',
      body: JSON.stringify({ siteId: props.siteId }),
    }))
  const skipped = job.items.filter(item => item.result?.skipped).length
  message.value = `已同步 ${job.total - job.failed - skipped} 个 Key，${job.failed} 个失败${skipped ? `，${skipped} 个跳过` : ''}`
  window.dispatchEvent(new Event('transithub:key-policy'))
  emit('changed')
  const problems = keyJobMessage(job)
  if (problems) error.value = problems
}

onMounted(() => run(read))

function save(apply: boolean) {
  return run(async () => {
    const allowedIPs = addresses.value.split(/[\s,]+/).filter(Boolean)
    if (props.entry) {
      await operationJSON(keyPath, {
        method: 'PATCH',
        body: JSON.stringify({ config: { [field]: field === 'allow_ips' ? allowedIPs.join('\n') : allowedIPs } }),
      })
      await read()
      window.dispatchEvent(new Event('transithub:key-policy'))
      emit('changed')
    } else if (policy.value) {
      applyPolicy(await operationJSON<SitePolicy>(sitePath, {
        method: 'PUT',
        body: JSON.stringify({ allowedIPs, inherited: inherited.value, version: policy.value.version }),
      }))
      window.dispatchEvent(new Event('transithub:key-policy'))
    }
    message.value = '白名单已保存'
    if (apply) await synchronize()
  })
}
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[140] flex items-center justify-center bg-black/40 p-4" @click.self="!busy && emit('close')">
      <section role="dialog" aria-modal="true" :aria-label="title" class="max-h-[90vh] w-full max-w-lg space-y-4 overflow-y-auto rounded-xl border border-border/60 bg-card p-5 shadow-xl">
        <header>
          <h3 class="font-semibold">{{ title }}</h3>
          <p class="mt-1 break-words text-sm text-muted-foreground">{{ name }}</p>
        </header>
        <p v-if="!entry" class="text-sm text-muted-foreground">站点默认 IP 用于继承默认策略的分组。分组和 Key 可设置专用 IP。</p>
        <label v-if="policy" class="flex items-center gap-2 text-sm">
          <input v-model="inherited" type="checkbox" :disabled="busy" />继承工作区默认 IP
        </label>
        <label class="block space-y-1 text-sm">
          <span>允许的 IP 和 CIDR</span>
          <textarea v-model="addresses" rows="4" :disabled="busy || !loaded || inherited" class="w-full rounded-lg border border-border bg-background p-3 font-mono disabled:opacity-60" />
        </label>
        <p v-if="entry" class="text-xs text-muted-foreground">保存自定义 IP 后，此 Key 使用自己的白名单。留空会取消 IP 限制。</p>
        <p v-if="message" role="status" class="text-sm text-primary">{{ message }}</p>
        <p v-if="error" role="alert" class="whitespace-pre-line break-words text-sm text-destructive">{{ error }}</p>
        <div class="flex flex-wrap justify-end gap-2">
          <Button variant="ghost" :disabled="busy" @click="emit('close')">关闭</Button>
          <Button v-if="!loaded && !busy" variant="secondary" @click="run(read)">重新读取</Button>
          <Button v-if="!entry || connectionIds.length" variant="secondary" :disabled="busy || !loaded" @click="run(synchronize)">{{ entry ? '同步白名单' : '同步本站白名单' }}</Button>
          <Button :disabled="busy || !loaded" @click="save(false)">{{ entry ? '保存自定义白名单' : '保存站点策略' }}</Button>
          <Button v-if="!entry" :disabled="busy || !loaded" @click="save(true)">保存并同步</Button>
        </div>
      </section>
    </div>
  </Teleport>
</template>
