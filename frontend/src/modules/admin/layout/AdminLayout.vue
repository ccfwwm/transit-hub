<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { navigationError } from '@/router'
import { LayoutDashboard, Network, Settings, LogOut, Globe, Moon, Sun, Percent, Megaphone, ChevronDown, ArrowRightLeft, Activity, KeyRound, PanelLeft } from 'lucide-vue-next'
import { useDark, useStorage, useToggle } from '@vueuse/core'
import OperationProgress from '../components/OperationProgress.vue'
import { resetOperationWorkspace } from '../api/operations'
import { useI18n } from 'vue-i18n'
import { useAdminAccounts } from '../composables/useAdminAccounts'
import { clearAccessToken } from '@/modules/auth/api/auth'
import { getSystemVersion } from '../api/system'
import type { SystemVersionResponse } from '../api/system'

const route = useRoute()
const router = useRouter()
const content = ref<HTMLElement | null>(null)
const refreshVersion = ref(0)
const sidebarVisible = useStorage('transithub.sidebar.visible', true)
const pageKey = computed(() => `${String(route.name)}:${refreshVersion.value}`)

function selectMenu(event: MouseEvent, path: string, navigate: (event: MouseEvent) => Promise<unknown>) {
  if (route.path === path && event.button === 0 && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && !event.defaultPrevented) {
    refreshVersion.value++
  }
  void navigate(event)
}

watch(pageKey, () => content.value?.scrollTo({ top: 0 }), { flush: 'post' })
const retryNavigation = () => {
  if (navigationError.value) void router.push(navigationError.value.path).catch(() => {})
}

const isDark = useDark({
  selector: 'html',
  attribute: 'class',
  valueDark: 'dark',
  valueLight: '',
})
const toggleDark = useToggle(isDark)

const { t, locale } = useI18n()
const toggleLocale = () => {
  locale.value = locale.value === 'zh-CN' ? 'en-US' : 'zh-CN'
}

const { currentAccount, loadCurrentAccount } = useAdminAccounts()
watch(() => currentAccount.value?.id, (value, previous) => {
  if (previous && value !== previous) resetOperationWorkspace()
})

// 版本信息：开源版仅用于纯展示，不依赖授权/更新服务
const versionInfo = ref<SystemVersionResponse | null>(null)

const loadVersionInfo = async () => {
  try {
    versionInfo.value = await getSystemVersion()
  } catch {
    // 版本信息加载失败不阻塞页面
  }
}

// 工作区选择页不显示侧边栏和业务菜单
const isWorkspaceSelectionPage = computed(() => route.name === 'AdminAccounts')

const showUserMenu = ref(false)
const userMenuRef = ref<HTMLElement | null>(null)

const toggleUserMenu = () => {
  showUserMenu.value = !showUserMenu.value
}

const handleClickOutside = (e: MouseEvent) => {
  if (userMenuRef.value && !userMenuRef.value.contains(e.target as Node)) {
    showUserMenu.value = false
  }
}

onMounted(() => {
  void loadCurrentAccount()
  void loadVersionInfo()
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})

const goToAccounts = () => {
  showUserMenu.value = false
  router.push('/admin/accounts')
}

const menuItems = computed(() => [
  { name: t('admin.menu.dashboard'), path: '/admin', icon: LayoutDashboard },
  { name: t('admin.menu.upstream'), path: '/admin/upstream', icon: Network },
  { name: t('admin.menu.upstreamKeys'), path: '/admin/upstream-keys', icon: KeyRound },
  { name: t('admin.menu.groupRates'), path: '/admin/group-rates', icon: Percent },
  { name: t('admin.menu.groupRateCampaigns'), path: '/admin/group-rate-campaigns', icon: Megaphone },
  { name: t('admin.menu.statusMonitor'), path: '/admin/status-monitor', icon: Activity },
  { name: t('admin.menu.settings'), path: '/admin/settings', icon: Settings },
])

const handleLogout = () => {
  showUserMenu.value = false
  resetOperationWorkspace()
  clearAccessToken()
  router.push('/login')
}
</script>

<template>
  <div class="h-dvh w-full flex overflow-hidden bg-background text-foreground">
    <!-- Sidebar: 工作区选择页不显示 -->
    <aside v-if="!isWorkspaceSelectionPage && sidebarVisible" id="admin-sidebar" class="w-12 shrink-0 border-r border-border/40 bg-surface-elevated flex flex-col md:w-48">
      <div class="h-16 flex items-center justify-center px-2 md:justify-start md:px-6 border-b border-border/40">
        <div class="flex items-center gap-2">
          <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 border border-primary/20">
            <span class="text-lg font-black text-primary leading-none">T</span>
          </div>
          <span class="hidden text-xl font-bold tracking-tight text-foreground md:inline">TransitHub</span>
        </div>
      </div>

      <nav class="flex-1 py-4 px-1.5 space-y-2 overflow-y-auto md:px-4 md:py-6">
        <router-link
          v-for="item in menuItems"
          :key="item.path"
          :to="item.path"
          custom
          v-slot="{ href, navigate, isExactActive }"
        >
          <a
            :href="href"
            :title="item.name"
            :aria-label="item.name"
            :aria-current="isExactActive ? 'page' : undefined"
            class="flex items-center justify-center gap-3 px-2 py-2.5 rounded-xl transition-colors md:justify-start md:px-3"
            :class="isExactActive ? 'bg-primary text-primary-foreground font-medium shadow-md shadow-primary/20' : 'text-muted-foreground hover:bg-surface-line hover:text-foreground'"
            @click="selectMenu($event, item.path, navigate)"
          >
            <component :is="item.icon" class="w-5 h-5" />
            <span class="hidden md:inline">{{ item.name }}</span>
          </a>
        </router-link>
      </nav>

      <div class="p-1.5 md:p-4 border-t border-border/40">
        <button
          @click="handleLogout"
          :title="t('admin.menu.signOut')"
          :aria-label="t('admin.menu.signOut')"
          class="flex items-center justify-center md:justify-start gap-3 px-2 md:px-3 py-2.5 w-full rounded-xl text-muted-foreground hover:bg-surface-line hover:text-red-400 transition-colors"
        >
          <LogOut class="w-5 h-5" />
          <span class="hidden md:inline">{{ t('admin.menu.signOut') }}</span>
        </button>
      </div>
    </aside>

    <!-- Main Content -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- Header: 工作区选择页不显示业务导航头 -->
      <header v-if="!isWorkspaceSelectionPage" class="h-12 shrink-0 border-b border-border/40 bg-surface/50 backdrop-blur-md flex items-center justify-between gap-1 px-2 md:px-4 sticky top-0 z-10">
        <div class="flex min-w-0 items-center gap-2">
          <button type="button" class="rounded-md p-2 text-muted-foreground hover:bg-surface-elevated hover:text-foreground" aria-controls="admin-sidebar" :aria-expanded="sidebarVisible" :aria-label="sidebarVisible ? '收起导航栏' : '展开导航栏'" :title="sidebarVisible ? '收起导航栏' : '展开导航栏'" @click="sidebarVisible = !sidebarVisible"><PanelLeft class="size-4" /></button>
          <h1 class="min-w-0 truncate text-base font-semibold">{{ route.path === '/admin' ? t('admin.menu.dashboard') : menuItems.find(m => m.path === route.path)?.name }}</h1>
        </div>

        <div class="flex shrink-0 items-center gap-1 md:gap-4">
          <OperationProgress :workspace-id="currentAccount?.id" />
          <div class="flex items-center gap-2">
            <!-- 版本号展示 -->
            <span
              v-if="versionInfo"
              class="hidden items-center gap-1 h-7 px-2 rounded-md text-xs font-medium text-muted-foreground sm:flex"
              :title="t('admin.system.version', { version: versionInfo.version })"
            >
              v{{ versionInfo.version }}
            </span>

            <button @click="toggleLocale" class="flex h-9 w-9 items-center justify-center rounded-full hover:bg-surface-elevated text-muted-foreground hover:text-foreground transition-colors" :title="t('admin.layout.toggleLanguage')">
              <Globe class="h-4 w-4" />
            </button>
            <button @click="toggleDark()" class="flex h-9 w-9 items-center justify-center rounded-full hover:bg-surface-elevated text-muted-foreground hover:text-foreground transition-colors" :title="t('admin.layout.toggleTheme')">
              <Moon v-if="!isDark" class="h-4 w-4" />
              <Sun v-else class="h-4 w-4" />
            </button>
          </div>

          <div ref="userMenuRef" class="relative">
            <button
              @click="toggleUserMenu"
              class="flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-surface-elevated transition-colors"
            >
              <div class="w-8 h-8 rounded-full bg-gradient-to-tr from-primary to-accent shrink-0"></div>
              <span v-if="currentAccount" class="text-sm font-medium text-foreground max-w-[120px] truncate hidden sm:inline">{{ currentAccount.displayName }}</span>
              <ChevronDown class="h-3.5 w-3.5 text-muted-foreground" />
            </button>

            <transition name="dropdown">
              <div
                v-if="showUserMenu"
                class="absolute right-0 top-full mt-2 w-56 rounded-xl border border-border/60 bg-surface-elevated shadow-lg py-1 z-50"
              >
                <div v-if="currentAccount" class="px-3 py-2.5 border-b border-border/40">
                  <div class="text-sm font-medium text-foreground truncate">{{ currentAccount.displayName }}</div>
                  <div class="text-xs text-muted-foreground truncate">{{ currentAccount.platform }} · {{ currentAccount.identity }}</div>
                </div>
                <button
                  @click="goToAccounts"
                  class="flex w-full items-center gap-2.5 px-3 py-2.5 text-sm text-muted-foreground hover:bg-surface-line hover:text-foreground transition-colors"
                >
                  <ArrowRightLeft class="h-4 w-4" />
                  {{ t('admin.layout.switchWorkspace') }}
                </button>
                <button
                  @click="handleLogout"
                  class="flex w-full items-center gap-2.5 px-3 py-2.5 text-sm text-muted-foreground hover:bg-surface-line hover:text-red-400 transition-colors"
                >
                  <LogOut class="h-4 w-4" />
                  {{ t('admin.menu.signOut') }}
                </button>
              </div>
            </transition>
          </div>
        </div>
      </header>

      <!-- Content Area -->
      <main ref="content" class="flex-1 overflow-auto" :class="isWorkspaceSelectionPage ? '' : route.name === 'AdminStatusMonitor' ? 'p-2 md:p-3' : 'p-3 md:p-6'">
        <div v-if="navigationError" role="alert" class="mb-3 flex items-center gap-3 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm">
          <span>{{ t(navigationError.message) }}</span>
          <button type="button" class="font-medium text-primary" @click="retryNavigation">{{ t('admin.navigation.retry') }}</button>
        </div>
        <router-view v-slot="{ Component }">
          <component :is="Component" :key="pageKey" />
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
