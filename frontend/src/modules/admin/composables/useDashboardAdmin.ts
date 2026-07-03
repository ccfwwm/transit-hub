import { ref } from 'vue'
import { authUnauthorizedErrorKey } from '@/modules/auth/api/auth'
import {
  getDashboardAdminStatus,
  loginDashboardAdmin,
  logoutDashboardAdmin,
} from '../api/dashboardAdmin'
import type { DashboardAdminLoginForm, DashboardAdminStatus } from '../types/dashboardAdmin'

/**
 * 管理仪表盘 admin 账户的登录门禁状态：
 * 进入仪表盘时检查是否已登录 admin；未登录则打开登录弹窗；并提供登录/退出能力。
 */
export function useDashboardAdmin() {
  const status = ref<DashboardAdminStatus>({ authenticated: false })
  const isChecking = ref(false)
  const isSubmitting = ref(false)
  const isModalOpen = ref(false)
  const errorKey = ref<string | null>(null)

  // TransitHub 登录态失效时请求层已统一跳转登录页（见 auth.ts 的 handleAuthExpired），
  // 这里只需要识别该错误并提前退出，不再重复实现跳转。
  const handleUnauthorized = (message: string): boolean => message === authUnauthorizedErrorKey

  // 进入仪表盘后检查 admin 登录状态：已登录则不弹窗，未登录则弹窗让登录。
  const checkStatus = async () => {
    if (isChecking.value) return
    isChecking.value = true
    errorKey.value = null
    try {
      const next = await getDashboardAdminStatus()
      status.value = next
      isModalOpen.value = !next.authenticated
    } catch (error) {
      const message = error instanceof Error ? error.message : 'admin.dashboard.adminAuth.errors.unknown'
      if (handleUnauthorized(message)) return
      // 状态接口失败时也打开弹窗，让用户可以尝试登录。
      status.value = { authenticated: false }
      isModalOpen.value = true
      errorKey.value = message
    } finally {
      isChecking.value = false
    }
  }

  const submitLogin = async (form: DashboardAdminLoginForm): Promise<boolean> => {
    if (isSubmitting.value) return false
    isSubmitting.value = true
    errorKey.value = null
    try {
      const next = await loginDashboardAdmin(form)
      status.value = next
      isModalOpen.value = false
      return true
    } catch (error) {
      const message = error instanceof Error ? error.message : 'admin.dashboard.adminAuth.errors.unknown'
      if (handleUnauthorized(message)) return false
      errorKey.value = message
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  // 退出当前 admin 账户后重新打开弹窗，提示重新登录。
  const logout = async () => {
    errorKey.value = null
    try {
      await logoutDashboardAdmin()
    } catch (error) {
      const message = error instanceof Error ? error.message : 'admin.dashboard.adminAuth.errors.unknown'
      if (handleUnauthorized(message)) return
      errorKey.value = message
    } finally {
      status.value = { authenticated: false }
      isModalOpen.value = true
    }
  }

  const openModal = () => {
    isModalOpen.value = true
  }

  const closeModal = () => {
    isModalOpen.value = false
  }

  return {
    status,
    isChecking,
    isSubmitting,
    isModalOpen,
    errorKey,
    checkStatus,
    submitLogin,
    logout,
    openModal,
    closeModal,
  }
}
