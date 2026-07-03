<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useI18n } from 'vue-i18n'
import { Mail, KeyRound } from 'lucide-vue-next'
import { loginWithEmail, storeAccessToken } from './api/auth'

const { t } = useI18n()
const router = useRouter()

const email = ref('')
const password = ref('')
const isLoading = ref(false)
const statusKey = ref<string | null>(null)
const errorKey = ref<string | null>(null)

const handleLogin = async () => {
  isLoading.value = true
  statusKey.value = null
  errorKey.value = null

  try {
    const response = await loginWithEmail({
      email: email.value,
      password: password.value,
    })
    storeAccessToken(response.accessToken)
    statusKey.value = 'auth.login.success'
    await router.push('/admin')
  } catch (error) {
    errorKey.value = error instanceof Error ? error.message : 'auth.errors.unknown'
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-background p-4 relative overflow-hidden">
    <!-- Background abstract -->
    <div class="absolute inset-0 -z-10 overflow-hidden">
      <div class="absolute top-0 left-1/2 -translate-x-1/2 w-[800px] h-[400px] bg-primary/10 blur-[100px] rounded-full" />
    </div>

    <div class="w-full max-w-md">
      <div class="bg-surface-elevated border border-border/50 rounded-[2rem] p-8 shadow-2xl backdrop-blur-xl relative overflow-hidden">
        <div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-primary via-accent to-primary" />
        
        <div class="text-center mb-8">
          <div class="inline-flex h-12 w-12 items-center justify-center rounded-xl bg-primary/10 border border-primary/20 mb-4">
            <span class="text-2xl font-black text-primary leading-none">T</span>
          </div>
          <h2 class="text-2xl font-bold tracking-tight text-foreground">{{ t('auth.login.title') }}</h2>
          <p class="text-sm text-muted-foreground mt-2">{{ t('auth.login.subtitle') }}</p>
        </div>

        <form @submit.prevent="handleLogin" class="space-y-5">
          <div class="space-y-2">
            <label class="text-sm font-medium text-foreground">{{ t('auth.login.email') }}</label>
            <div class="relative">
              <Mail class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
              <Input 
                v-model="email"
                type="email" 
                :placeholder="t('auth.login.emailPlaceholder')" 
                class="pl-10 h-12 bg-surface border-border/50 focus:border-primary"
                autocomplete="email"
                :disabled="isLoading"
                required
              />
            </div>
          </div>

          <div class="space-y-2">
            <label class="text-sm font-medium text-foreground">{{ t('auth.login.password') }}</label>
            <div class="relative">
              <KeyRound class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
              <Input 
                v-model="password"
                type="password" 
                :placeholder="t('auth.login.passwordPlaceholder')" 
                class="pl-10 h-12 bg-surface border-border/50 focus:border-primary"
                autocomplete="current-password"
                :disabled="isLoading"
                required
              />
            </div>
          </div>

          <p
            v-if="statusKey"
            class="rounded-xl border border-signal/20 bg-signal/10 px-4 py-3 text-sm font-medium text-signal"
          >
            {{ t(statusKey) }}
          </p>

          <p
            v-if="errorKey"
            class="rounded-xl border border-warning/20 bg-warning/10 px-4 py-3 text-sm font-medium text-warning"
          >
            {{ t(errorKey) }}
          </p>

          <Button 
            type="submit" 
            class="w-full h-12 text-base font-bold mt-2 shadow-glow"
            :disabled="isLoading"
          >
            {{ isLoading ? t('auth.login.submitting') : t('auth.login.submit') }}
          </Button>
        </form>
      </div>
    </div>
  </div>
</template>
