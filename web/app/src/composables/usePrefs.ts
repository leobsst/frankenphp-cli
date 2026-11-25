import { computed, ref, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { useColorMode } from '@vueuse/core'
import { state, TEXT_SCALES, type Prefs, type TextSize } from './useFm'
import { setLanguage } from '../i18n'
import { shell } from '../lib/shell'

/** Preferences being edited (previewed live on the Settings page). */
export const draftPrefs = ref<Prefs>({ ...state.prefs })

/** Applies theme, colors, animations and language (with `<html lang>`) from the active preferences. */
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
    setLanguage(p.language)
    const scale = textScale(p.textSize)
    // CSS zoom (main.css) only when the application does not zoom natively.
    root.style.setProperty('--fm-zoom', shell.setZoom?.(scale) ? '1' : String(scale))
  })
}

/** Interface scale of a text size; `auto`: the one of the device (shell.autoScale). */
export function textScale(size: TextSize) {
  return size === 'auto' ? shell.autoScale : TEXT_SCALES[size] ?? 1
}
