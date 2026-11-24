<script setup lang="ts">
import { onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fr } from '@nuxt/ui/locale'
import { useToast } from '@nuxt/ui/composables'
import { App as CapApp } from '@capacitor/app'
import { Capacitor } from '@capacitor/core'
import ConfirmPassword from '@app/components/ConfirmPassword.vue'
import { bindNotifier, state } from '@app/composables/useFm'
import { useApplyPrefs } from '@app/composables/usePrefs'
import { hideSplash } from './lib/splash'

const router = useRouter()

// The splash screen stays until the app is ready: right away for the instance list, after the first read of the
// instance for the dashboard (successful or not). 10 seconds at most, whatever happens.
const route = useRoute()
watch([() => route.matched.length, () => route.meta.dashboard, () => state.loaded], ([matched, dashboard, loaded]) => {
  if (matched && (!dashboard || loaded)) hideSplash()
}, { immediate: true })
setTimeout(hideSplash, 10_000)

// Store notifications (operation progress).
const toast = useToast()
bindNotifier(
  (t) => (t.id && toast.toasts.value.some((x) => x.id === t.id) ? toast.update(t.id, t) : toast.add(t)),
  (id) => toast.remove(id),
)
useApplyPrefs()

/**
 * Reka (under the Nuxt UI toasts) suspends their timer while the toast area has the focus or the window lost it,
 * and resumes it on focusout, pointerleave or window.focus. On iOS these events are missing: the ✕ button removed
 * from the DOM fires no focusout, coming back to the foreground fires no focus. The pause then stayed for good, and
 * no toast disappeared any more. It is lifted after each touch and on return to the foreground.
 */
function resumeToasts() {
  const viewport = document.querySelector<HTMLElement>('ol[data-slot="viewport"]')
  if (!viewport) return
  const active = document.activeElement
  if (active instanceof HTMLElement && viewport.contains(active)) active.blur()
  viewport.dispatchEvent(new PointerEvent('pointerleave'))
  viewport.dispatchEvent(new MouseEvent('mouseleave'))
  window.dispatchEvent(new Event('focus'))
}

onMounted(() => {
  document.addEventListener('touchend', () => setTimeout(resumeToasts, 50), { capture: true, passive: true })
  void CapApp.addListener('appStateChange', ({ isActive }) => { if (isActive) setTimeout(resumeToasts, 50) })
  // Android back button: go back, or quit from the instance list.
  if (Capacitor.getPlatform() === 'android') {
    void CapApp.addListener('backButton', ({ canGoBack }) => {
      if (canGoBack && router.currentRoute.value.path !== '/instances') router.back()
      else void CapApp.exitApp()
    })
  }
})
</script>

<template>
  <!-- Notifications below the top safe area (notch, status bar), not behind it. -->
  <UApp :locale="fr" :toaster="{ position: 'top-center', max: 3, ui: { viewport: 'top-[calc(1rem+var(--sat))]' } }">
    <RouterView />
    <ConfirmPassword />
  </UApp>
</template>
