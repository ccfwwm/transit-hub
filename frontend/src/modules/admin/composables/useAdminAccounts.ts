import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import type { AdminAccount } from '../types/adminAccounts'
import {
  listAdminAccounts,
  getCurrentAdminAccount,
  switchAdminAccount,
  updateAdminAccount,
} from '../api/adminAccounts'
import { markWorkspaceActive, resetWorkspaceCheck } from '@/lib/workspaceGuard'

const accounts = ref<AdminAccount[]>([])
const currentAccount = ref<AdminAccount | null>(null)
const isLoading = ref(false)
const isSwitching = ref(false)
const errorKey = ref('')

export function useAdminAccounts() {
  const router = useRouter()

  const hasAccounts = computed(() => accounts.value.length > 0)
  const hasCurrentAccount = computed(() => currentAccount.value !== null)

  const loadAccounts = async () => {
    isLoading.value = true
    errorKey.value = ''
    try {
      accounts.value = await listAdminAccounts()
    } catch (err) {
      errorKey.value = err instanceof Error ? err.message : 'admin.adminAccounts.errors.request'
    } finally {
      isLoading.value = false
    }
  }

  const loadCurrentAccount = async (): Promise<boolean> => {
    try {
      currentAccount.value = await getCurrentAdminAccount()
      return true
    } catch {
      currentAccount.value = null
      return false
    }
  }

  const switchAccount = async (id: string) => {
    isSwitching.value = true
    errorKey.value = ''
    try {
      currentAccount.value = await switchAdminAccount(id)
      accounts.value = accounts.value.map(a => ({
        ...a,
        current: a.id === id,
      }))
      markWorkspaceActive()
      await router.push('/admin')
    } catch (err) {
      // 切换失败时重置 workspace 缓存，下次导航会重新验证后端状态，
      // 防止前端缓存一个实际已失效的 workspace。
      resetWorkspaceCheck()
      errorKey.value = err instanceof Error ? err.message : 'admin.adminAccounts.errors.request'
    } finally {
      isSwitching.value = false
    }
  }

  const renameAccount = async (id: string, displayName: string) => {
    errorKey.value = ''
    try {
      const updated = await updateAdminAccount(id, displayName)
      accounts.value = accounts.value.map(a => a.id === id ? updated : a)
      if (currentAccount.value?.id === id) {
        currentAccount.value = updated
      }
    } catch (err) {
      errorKey.value = err instanceof Error ? err.message : 'admin.adminAccounts.errors.request'
    }
  }

  return {
    accounts,
    currentAccount,
    isLoading,
    isSwitching,
    errorKey,
    hasAccounts,
    hasCurrentAccount,
    loadAccounts,
    loadCurrentAccount,
    switchAccount,
    renameAccount,
  }
}
