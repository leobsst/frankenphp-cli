import { reactive } from 'vue'

/**
 * What the application mounting the dashboard provides to it: the name and address of the managed instance and, if
 * there are several (the mobile app), the page used to switch between them.
 */
export const shell = reactive({
  /** Displayed instance name; empty: the machine name, read from the API. */
  name: '',
  /** Displayed instance address; empty: its address on the local network, read from the API. */
  address: '',
  /** Route of the instance list, null if there is only one. */
  switchTo: null as string | null,
})
