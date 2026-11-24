import { computed, ref, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { useColorMode } from '@vueuse/core'
import { state, type Prefs } from './useFm'

/** Préférences en cours d'édition (prévisualisées en direct sur la page Paramètres). */
export const draftPrefs = ref<Prefs>({ ...state.prefs })

/** Applique thème, couleurs et animations à partir des préférences actives. */
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
