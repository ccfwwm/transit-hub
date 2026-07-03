<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LayoutDashboard, Network, Settings, LogOut, Globe, Moon, Sun, Percent, Megaphone, ChevronDown, ArrowRightLeft } from 'lucide-vue-next'
import { useDark, useToggle } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useAdminAccounts } from '../composables/useAdminAccounts'
import { clearAccessToken } from '@/modules/auth/api/auth'
import { getSystemVersion } from '../api/system'
import type { SystemVersionResponse } from '../api/system'

const route = useRoute()
const router = useRouter()

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
  { name: t('admin.menu.groupRates'), path: '/admin/group-rates', icon: Percent },
  { name: t('admin.menu.groupRateCampaigns'), path: '/admin/group-rate-campaigns', icon: Megaphone },
  { name: t('admin.menu.settings'), path: '/admin/settings', icon: Settings },
])

const handleLogout = () => {
  showUserMenu.value = false
  clearAccessToken()
  router.push('/login')
}
</script>

<template>
  <div class="h-screen flex overflow-hidden bg-background text-foreground">
    <!-- Sidebar: 工作区选择页不显示 -->
    <aside v-if="!isWorkspaceSelectionPage" class="w-64 border-r border-border/40 bg-surface-elevated flex flex-col">
      <div class="h-16 flex items-center px-6 border-b border-border/40">
        <div class="flex items-center gap-2">
          <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 border border-primary/20">
            <span class="text-lg font-black text-primary leading-none">T</span>
          </div>
          <span class="text-xl font-bold tracking-tight text-foreground">TransitHub</span>
        </div>
      </div>

      <nav class="flex-1 py-6 px-4 space-y-2 overflow-y-auto">
        <router-link
          v-for="item in menuItems"
          :key="item.path"
          :to="item.path"
          class="flex items-center gap-3 px-3 py-2.5 rounded-xl transition-colors"
          :class="[
            route.path === item.path
              ? 'bg-primary text-primary-foreground font-medium shadow-md shadow-primary/20'
              : 'text-muted-foreground hover:bg-surface-line hover:text-foreground'
          ]"
        >
          <component :is="item.icon" class="w-5 h-5" />
          {{ item.name }}
        </router-link>
      </nav>

      <div class="p-4 border-t border-border/40">
        <button
          @click="handleLogout"
          class="flex items-center gap-3 px-3 py-2.5 w-full rounded-xl text-muted-foreground hover:bg-surface-line hover:text-red-400 transition-colors"
        >
          <LogOut class="w-5 h-5" />
          {{ t('admin.menu.signOut') }}
        </button>
      </div>
    </aside>

    <!-- Main Content -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- Header: 工作区选择页不显示业务导航头 -->
      <header v-if="!isWorkspaceSelectionPage" class="h-16 shrink-0 border-b border-border/40 bg-surface/50 backdrop-blur-md flex items-center justify-between px-6 sticky top-0 z-10">
        <h1 class="text-lg font-semibold">{{ route.path === '/admin' ? t('admin.menu.dashboard') : menuItems.find(m => m.path === route.path)?.name }}</h1>

        <div class="flex items-center gap-4">
          <div class="flex items-center gap-2">
            <!-- 版本号展示 -->
            <span
              v-if="versionInfo"
              class="flex items-center gap-1 h-7 px-2 rounded-md text-xs font-medium text-muted-foreground"
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
      <main class="flex-1 overflow-auto" :class="isWorkspaceSelectionPage ? '' : 'p-6'">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(10px);
}

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
