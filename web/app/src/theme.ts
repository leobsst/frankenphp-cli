/**
 * Nuxt UI theme shared by the embedded dashboard and the mobile app (passed to the `ui()` Vite plugin of each).
 *
 * Besides the mockup colours, it keeps the overlays out of the safe areas (notch, Dynamic Island, status bar, home
 * indicator, Android cutouts and gesture bar) through the --sat/--sab/--sal/--sar variables of main.css, and keeps
 * the toasts above the bottom navigation (--fm-bottom-inset). The classes are listed here in full so that Tailwind
 * finds them (main.css scans web/app/src).
 */
/**
 * Input fields on phones: text-sm instead of the text-base of the md / lg / xl sizes, closer to the rest of the
 * interface. iOS zooms in on a focused field under 16px: the maximum-scale of the viewport prevents it (index.html of
 * each app), pinching stays possible.
 */
const phoneFieldText = { compoundVariants: [{ size: ['md', 'lg', 'xl'], class: { base: 'max-md:text-sm/5' } }] }

export const uiTheme = {
  colors: { primary: 'brand', info: 'accent', success: 'brand', warning: 'amber', error: 'red', neutral: 'sage' as 'zinc' },
  input: phoneFieldText,
  textarea: phoneFieldText,
  select: phoneFieldText,
  selectMenu: phoneFieldText,
  inputMenu: phoneFieldText,
  inputNumber: phoneFieldText,
  inputTags: phoneFieldText,
  // Active navigation item: pale background + primary text (mockup).
  navigationMenu: {
    compoundVariants: [{ variant: 'pill', active: true, highlight: false, class: { link: 'before:bg-primary/10 font-semibold' } }],
  },
  modal: {
    variants: {
      // Full screen: the content (header and footer buttons included) stays inside the safe areas.
      fullscreen: { true: { content: 'pt-(--sat) pb-(--sab) ps-(--sal) pe-(--sar)' } },
    },
    compoundVariants: [
      {
        // Centred dialog: its size leaves the safe areas free (status bar, home indicator, side cutout), and it is
        // centred between them.
        scrollable: false,
        fullscreen: false,
        class: {
          content: 'top-[calc(50%_+_(var(--sat)_-_var(--sab))_/_2)] left-[calc(50%_+_(var(--sal)_-_var(--sar))_/_2)] w-[calc(100vw_/_var(--fm-zoom,1)_-_2rem_-_var(--sal)_-_var(--sar))] max-h-[calc(100dvh_/_var(--fm-zoom,1)_-_2rem_-_var(--sat)_-_var(--sab))] sm:max-h-[calc(100dvh_/_var(--fm-zoom,1)_-_4rem_-_var(--sat)_-_var(--sab))]',
        },
      },
      {
        // Scrollable dialog: the overlay scrolls, its padding covers the safe areas.
        scrollable: true,
        fullscreen: false,
        class: { overlay: 'pt-[calc(1rem_+_var(--sat))] pb-[calc(1rem_+_var(--sab))] ps-[calc(1rem_+_var(--sal))] pe-[calc(1rem_+_var(--sar))] sm:pt-[calc(2rem_+_var(--sat))] sm:pb-[calc(2rem_+_var(--sab))]' },
      },
    ],
  },
  slideover: {
    // Edge panels: the side touching a screen edge gets that edge's inset, plus the top and bottom ones.
    compoundVariants: [
      { side: 'left', inset: false, class: { content: 'pt-(--sat) pb-(--sab) ps-(--sal)' } },
      { side: 'right', inset: false, class: { content: 'pt-(--sat) pb-(--sab) pe-(--sar)' } },
      { side: 'top', inset: false, class: { content: 'pt-(--sat) ps-(--sal) pe-(--sar)' } },
      { side: 'bottom', inset: false, class: { content: 'pb-(--sab) ps-(--sal) pe-(--sar)' } },
      { side: ['left', 'right'], inset: true, class: { content: 'top-[calc(1rem_+_var(--sat))] bottom-[calc(1rem_+_var(--sab))]' } },
      { side: 'top', inset: true, class: { content: 'top-[calc(1rem_+_var(--sat))]' } },
      { side: 'bottom', inset: true, class: { content: 'bottom-[calc(1rem_+_var(--sab))]' } },
    ],
  },
  toaster: {
    // Below the status bar at the top; above the bottom bar (HTML or native) and the home indicator at the bottom;
    // clear of a landscape notch on the sides.
    variants: {
      position: {
        'top-left': { viewport: 'left-[calc(1rem_+_var(--sal))]' },
        'top-right': { viewport: 'right-[calc(1rem_+_var(--sar))]' },
        'bottom-left': { viewport: 'left-[calc(1rem_+_var(--sal))]' },
        'bottom-right': { viewport: 'right-[calc(1rem_+_var(--sar))]' },
        // Centred: between the side insets, its width leaving them free.
        'top-center': { viewport: 'left-[calc(50%_+_(var(--sal)_-_var(--sar))_/_2)] w-[calc(100%_-_2rem_-_var(--sal)_-_var(--sar))]' },
        'bottom-center': { viewport: 'left-[calc(50%_+_(var(--sal)_-_var(--sar))_/_2)] w-[calc(100%_-_2rem_-_var(--sal)_-_var(--sar))]' },
      },
    },
    compoundVariants: [
      { position: ['top-left', 'top-center', 'top-right'], class: { viewport: 'top-[calc(1rem_+_var(--sat))]' } },
      { position: ['bottom-left', 'bottom-center', 'bottom-right'], class: { viewport: 'bottom-[calc(1rem_+_var(--fm-bottom-inset))]' } },
    ],
  },
}

/**
 * Icon bundling for the `ui()` Vite plugin of the embedded dashboard and the mobile app: every Lucide icon the code
 * names is inlined in the build, so the dashboard works offline (PWA, mobile app, network without Internet access).
 *
 * Nuxt UI's default scan only reads web/embedded or web/mobile (the Vite root), and only their .vue files: the shared
 * pages of web/app and the icons named in .ts files (navigation, composables, toasts) were missing from the bundle and
 * fetched from the Iconify API at runtime. The patterns are relative to the Vite root, a sibling of web/app. Icons
 * must be written in full (`i-lucide-…`), never built from parts, for the scan to find them.
 */
export const uiIcons = {
  clientBundle: {
    scan: { globInclude: ['src/**/*.{vue,ts}', '../app/src/**/*.{vue,ts}'] },
  },
}

/**
 * Size of the CSS (Nuxt UI generates the classes of every variant of every component it knows): only the components
 * used in the sources of the application and of web/app (`@frankenmanager/app`), and only the colours of uiTheme
 * (no `secondary`). Tailwind itself only generates the utilities found in the sources.
 */
export const uiCss = {
  theme: { colors: ['primary', 'info', 'success', 'warning', 'error'] },
  scanPackages: ['@frankenmanager/app'],
  experimental: { componentDetection: true },
}
