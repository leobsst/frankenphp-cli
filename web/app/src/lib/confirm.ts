import { reactive } from 'vue'
import { setConfirmer } from './api'

/** État de la demande de mot de passe (ConfirmPassword.vue), branchée sur le client d'API. */
export const confirmState = reactive({
  open: false,
  action: '',
  resolve: null as ((password: string | null) => void) | null,
})

setConfirmer(
  (action) =>
    new Promise((resolve) => {
      confirmState.action = action
      confirmState.resolve = resolve
      confirmState.open = true
    }),
)

export function answerConfirm(password: string | null) {
  confirmState.resolve?.(password)
  confirmState.resolve = null
  confirmState.open = false
}
