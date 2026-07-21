import { createApp } from 'vue'
import App from './App.vue'
import './styles/globals.css'
import { i18n } from './i18n'
import { router } from './router'

const staleChunkReloadKey = 'transit-hub:stale-chunk-reload'
const reloadForStaleChunk = () => {
  if (window.sessionStorage.getItem(staleChunkReloadKey)) return
  window.sessionStorage.setItem(staleChunkReloadKey, '1')
  window.location.reload()
}

window.addEventListener('vite:preloadError', (event) => {
  event.preventDefault()
  reloadForStaleChunk()
})
router.onError((error) => {
  if (/dynamically imported module|Importing a module script failed/i.test(String(error))) {
    reloadForStaleChunk()
  }
})

const app = createApp(App)

app.use(i18n)
app.use(router)
app.mount('#app')

