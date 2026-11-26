import type { CapacitorConfig } from '@capacitor/cli'

const config: CapacitorConfig = {
  // Default identifier: the one of each developer comes from the environment (IOS_BUNDLE_ID, ANDROID_APPLICATION_ID,
  // see .env.example), applied by ios/signing.xcconfig and android/app/build.gradle.
  appId: 'dev.frankenmanager.mobile',
  appName: 'FrankenManager',
  webDir: 'dist',
  // Requests go through the WebView (fetch, EventSource): the daemon answers the app's origins (capacitor://localhost
  // on iOS, https://localhost on Android) with the CORS headers.
  // The WebView itself does not scroll: each screen has its own scrolling container (with its bounce), so the header
  // stays in place instead of being dragged along by the bounce of the whole page.
  ios: { contentInset: 'never', scrollEnabled: false },
  // Pinch zoom (accessibility): the viewport limits it on iOS only to avoid the zoom on a focused field, and the iOS
  // app ignores that limit for the user (MainViewController.swift).
  zoomEnabled: true,
  plugins: {
    // The native screen stays until the page has painted its identical HTML splash screen (src/lib/splash.ts), which
    // adds the spinner: no blank screen between the two.
    SplashScreen: {
      launchAutoHide: false,
      backgroundColor: '#4F1895',
      androidScaleType: 'CENTER_CROP',
      showSpinner: false,
    },
    LocalNotifications: {
      smallIcon: 'ic_stat_notification',
      iconColor: '#7B3FC4',
    },
  },
}

export default config
