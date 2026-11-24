/** Place le focus sur le premier champ d'un formulaire latéral (boutons « Ajouter… » de l'en-tête). */
export function focusField(id: string) {
  const el = document.getElementById(id)
  el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  el?.focus({ preventScroll: true })
}
