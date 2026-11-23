import { shallowRef } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import { authUnauthorizedErrorKey, getAccessToken } from './modules/auth/api/auth'
import { getCurrentAdminAccount } from './modules/admin/api/adminAccounts'
import {
  resetWorkspaceCheck,
  markWorkspaceActive,
  isWorkspaceChecked,
  isWorkspaceActive,
  setWorkspaceChecked,
} from './lib/workspaceGuard'

const routes = [
  {
    path: '/',
    redirect: '/login'
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('./modules/auth/LoginPage.vue')
  },
  {
    path: '/register',
    redirect: '/login'
  },
  {
    path: '/admin',
    component: () => import('./modules/admin/layout/AdminLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        name: 'AdminDashboard',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/DashboardView.vue')
      },
      {
        path: 'upstream',
        name: 'AdminUpstream',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/UpstreamView.vue')
      },
      {
        path: 'upstream-keys',
        name: 'AdminUpstreamKeys',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/UpstreamKeysView.vue')
      },
      {
        path: 'group-rates',
        name: 'AdminGroupRates',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/GroupRatesView.vue')
      },
      {
        path: 'group-rate-campaigns',
        name: 'AdminGroupRateCampaigns',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/GroupRateCampaignsView.vue')
      },
      {
        path: 'status-monitor',
        name: 'AdminStatusMonitor',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/StatusMonitorView.vue')
      },
      {
        path: 'settings',
        name: 'AdminSettings',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/SettingsView.vue')
      },
      {
        path: 'accounts',
        name: 'AdminAccounts',
        component: () => import('./modules/admin/views/AdminAccountsView.vue')
      }
    ]
  }
]

export const router = createRouter({
  history: createWebHistory(),
  routes
})

const recoveryKey = 'transithub.route-recovery'
export const navigationError = shallowRef<{ path: string; message: string } | null>(null)

router.onError((error, to) => {
  const message = String(error?.message || error)
  const staleAssets = /dynamically imported module|Loading chunk|Importing a module script failed|Unable to preload CSS|error loading dynamically imported/i.test(message)
  if (staleAssets) {
    try {
      const previous = Number(sessionStorage.getItem(recoveryKey) || 0)
      if (Date.now() - previous > 20_000) {
        sessionStorage.setItem(recoveryKey, String(Date.now()))
        window.location.replace(router.resolve(to.fullPath).href)
        return
      }
    } catch {
      // Storage may be unavailable in a restricted browser context.
    }
  }
  navigationError.value = { path: to.fullPath, message: staleAssets ? 'admin.navigation.updateFailed' : 'admin.navigation.loadFailed' }
})

router.afterEach((_to, _from, failure) => {
  if (!failure) navigationError.value = null
})

router.beforeEach(async (to) => {
  if (to.matched.some((route) => route.meta.requiresAuth) && !getAccessToken()) {
    return { path: '/login' }
  }

  if (to.matched.some((route) => route.meta.requiresWorkspace)) {
    if (!isWorkspaceChecked()) {
      try {
        await getCurrentAdminAccount(AbortSignal.timeout(8000))
        setWorkspaceChecked(true, true)
      } catch (error) {
        if (error instanceof Error && error.message === 'admin.adminAccounts.errors.noCurrentAccount') {
          setWorkspaceChecked(true, false)
        } else if (error instanceof Error && error.message === authUnauthorizedErrorKey) {
          return { path: '/login' }
        } else {
          throw error
        }
      }
    }
    if (!isWorkspaceActive()) {
      return { name: 'AdminAccounts' }
    }
  }

  return true
})

export { resetWorkspaceCheck, markWorkspaceActive }
