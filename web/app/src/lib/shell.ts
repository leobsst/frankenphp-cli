import { markRaw, reactive, type Component } from 'vue'

/**
 * What the application mounting the dashboard provides to it: the name and address of the managed instance and the
 * page used to switch between instances (the mobile app and the embedded dashboard both manage several).
 */
export const shell = reactive({
  /** Displayed instance name; empty: the machine name, read from the API. */
  name: '',
  /** Displayed instance address; empty: its address on the local network, read from the API. */
  address: '',
  /** Route of the instance list, null if there is only one. */
  switchTo: null as string | null,
  /**
   * Interface scale of the `auto` text size. 1 everywhere, except where the app knows the system shrinks it: the iPad
   * app running on a Mac (macOS draws it at 77 %).
   */
  autoScale: 1,
  /** Settings card of the application itself (the mobile app: the Face ID lock), shown in Settings. */
  settingsCard: null as Component | null,
  /** Native tabs of the pages on a phone (the iPhone app: a segmented control below the header); null: drawn in HTML. */
  pageTabs: null as PageTabsHost | null,
  /**
   * Saves a text file through the application (the mobile app, whose WebView ignores downloads: the share sheet of the
   * system). Resolves true once done, false when cancelled, null when the system does not tell. Null: a browser
   * download.
   */
  saveText: null as ((name: string, text: string) => Promise<boolean | null>) | null,
})

/** A tab of a page: its label and the value it selects. */
export interface PageTab {
  label: string
  value: string
}

/**
 * Native page tabs. A page shows its tabs under its own `owner` key, and they follow its selection when it shows them
 * again; `hide` only removes them if they are still the ones of that owner (the next page may already have shown its
 * own).
 */
export interface PageTabsHost {
  show(owner: object, items: PageTab[], selected: string, onSelect: (value: string) => void): void
  hide(owner: object): void
}

/** Registers the application's settings card (not made reactive: a component). */
export function setSettingsCard(card: Component | null) {
  shell.settingsCard = card && markRaw(card)
}
