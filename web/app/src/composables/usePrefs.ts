import { computed, ref, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { useColorMode } from '@vueuse/core'
import { state, type Prefs } from './useFm'

/** Preferences being edited (previewed live on the Settings page). */
export const draftPrefs = ref<Prefs>({ ...state.prefs })

/** Applies theme, colors and animations from the active preferences. */
export function useApplyPrefs() {
  const route = useRoute()
  const mode = useColorMode()
  const effective = computed(() => (route.name === 'settings' ? draftPrefs.value : state.prefs))

  watchEffect(() => {
    const p = effective.value
    mode.value = p.theme === 'system' ? 'auto' : p.theme
    const root = document.documentElement
    root.style.setProperty('--fm-primary', p.primary)
    root.style.setProperty('--fm-accent', p.accent)
    root.classList.toggle('reduce-motion', p.reduceMotion)
    root.lang = p.lang
  })
}
