import { reactive } from 'vue'

/**
 * Ce que l'application qui monte le dashboard lui fournit : le nom et l'adresse de l'instance pilotée, et, s'il y en a
 * plusieurs (l'app mobile), la page qui permet d'en changer.
 */
export const shell = reactive({
  /** Nom affiché de l'instance ; vide : le nom de la machine, lu par l'API. */
  name: '',
  /** Adresse affichée de l'instance ; vide : son adresse sur le réseau local, lue par l'API. */
  address: '',
  /** Route de la liste des instances, null s'il n'y en a qu'une. */
  switchTo: null as string | null,
})
