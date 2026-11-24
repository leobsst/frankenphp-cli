/** Focuses the first field of a side form (the header's "Ajouter…" buttons). */
export function focusField(id: string) {
  const el = document.getElementById(id)
  el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  el?.focus({ preventScroll: true })
}
