# FrankenManager mobile app

An iOS and Android app that manages several FrankenManager instances through their remote access. Same stack as the
dashboard (Vue 3, Nuxt UI, Vite), in a Capacitor 8 shell. The app is in French, like the dashboard.

It is a member of the `web/` pnpm workspace: the pages, components and store come from `web/app`
(`@frankenmanager/app`, imported through the `@app` alias), shared with the dashboard embedded in the binary
(`web/embedded`). This package only holds what is specific to the app:

| Path | Role |
|---|---|
| `src/main.ts`, `src/App.vue`, `src/router.ts` | Entry point, root component (toasts, splash screen, Android back button, pairing links), routes |
| `src/DashboardLayout.vue` | Dashboard layout on the chosen instance; fills `shell` (`web/app/src/lib/shell.ts`) with its name, address and the way back to the list |
| `src/pages/InstancesPage.vue`, `InstanceForm.vue`, `PairPage.vue` | Instance list, sign-in with the password, QR code pairing |
| `src/lib/instances.ts` | Instance list (Capacitor Preferences) and their tokens (secure store) |
| `src/lib/secureStore.ts` | Keychain / Keystore access (`SecureStore` plugin of the app) |
| `src/lib/pairing.ts` | Pairing link parsing, QR code scanner, `frankenmanager://` links |
| `src/lib/nativeTabBar.ts`, `src/lib/nativeSegments.ts`, `src/lib/overlays.ts` | Native iOS tab bar and page tabs, hidden while a modal, the menu or a toast is open |
| `src/lib/splash.ts`, `src/lib/display.ts` | Splash screen hand-off, automatic text size of the iPad app on a Mac |
| `ios/`, `android/` | Native projects, with the app's own plugins (`NativeTabBarPlugin.swift`, `NativeSegmentsPlugin.swift`, `SecureStorePlugin.swift`, `SecureStorePlugin.java`) |
| `scripts/` | Version and build number, iOS signing, extra icons |
| `assets/` | Sources of the icons and splash screens |

## What the app does

- **Instances**: the list of machines with their state (stack running or stopped, number of domains, unreachable,
  session expired). An instance is added by scanning the pairing QR code of its dashboard, or with its address and
  the dashboard password. The daemon answers a session token (Bearer), listed in the devices of the dashboard.
- **Dashboard**: every page of the dashboard, on the chosen instance. The instance block of the sidebar
  ("Changer d’instance") goes back to the list. Destructive actions ask for the password again (`X-FM-Password`).
- **Native tab bar on iPhone**: `ios/App/App/NativeTabBarPlugin.swift` lays a real `UITabBar` over the web view,
  Liquid Glass from iOS 26 on (classic blurred look before). It replaces the HTML bar (Aperçu, Projets, Services,
  Réglages, with SF Symbols), follows the open page, takes the primary colour of the app, and hides while a modal or
  the menu is open. On Android, in the browser and on iPad (sidebar), the bar stays in HTML.
- **Native page tabs on iPhone**: `ios/App/App/NativeSegmentsPlugin.swift` pins a `UISegmentedControl` below the
  header, the content scrolling under it; it scrolls sideways when the labels are long. The pages show it through
  `PageTabs` (`web/app`, `shell.pageTabs`) at phone width; on Android and in the browser it is drawn in HTML, and
  from the tablet up the tabs stay underlined.
- **Text size and zoom**: the size of Settings is a CSS zoom of the page on every platform (the native `pageZoom`
  of the WebView kept the layout at the unzoomed width: the iPad app on a Mac overflowed its window). Pinching is
  allowed (`zoomEnabled`, `ignoresViewportScaleLimits`); `maximum-scale=1` only stops iOS zooming in on a field.
- **Demo mode**: "Try the demo" on the instance list (see below).
- Left out of the app: updating FrankenManager (`sudo frankenmanager update` on the machine). Changing the dashboard
  password signs the app out too: sign in again, or pair again.

## Demo mode

"Try the demo" ("Essayer la démo"), on the empty instance list and below the list, adds a **Demo** instance and opens
it: the whole app can be tried without a machine running FrankenManager, App Review included. The instance shows a
Demo badge and "Simulated data" instead of an address; it is removed like any other (⋯ › Remove from the app), and
added again fresh.

- **Seam**: its address uses the reserved `demo:` scheme (`demo://frankenmanager`). `main.ts` registers a client for
  that scheme (`registerClientScheme`, `web/app/src/lib/api.ts`), so `client(inst)` returns the demo client and the
  store (`useFm.ts`) and every page run unchanged. The store opens its streams through `client.stream()`: an
  `EventSource` for a daemon, an in-memory stand-in (`src/demo/stream.ts`) for the demo.
- **Fake daemon** (`src/demo/server.ts`): answers the routes the store calls with fictional data (`src/demo/fixtures.ts`:
  shop.test, api.test, legacy.test, studio.test, front.test, docs.test, their containers, Redis keys, databases, S3
  buckets, LAN DNS devices, sessions), typed from the generated API types. Actions are operations that go from queued
  to running to succeeded, then change the data (add a project, switch PHP, stop or start the stack or a container,
  toggle a service, create a bucket…). Stats vary, the log stream emits plausible lines, and the event stream
  announces operations, containers and a simulated pairing. Data is English like a daemon's; the interface texts
  stay in the catalogs.
- **No network, no secrets**: nothing leaves the phone; the demo instance has no token and never touches the secure
  store; destructive actions ask for no password (the demo is treated as a local connection). Remote access shows as
  open on a fictional address (`192.168.1.20`). The data resets when the app restarts.
- In the browser, `pnpm --dir web mobile:dev` then "Try the demo" works the same.

### Screenshot builds

`pnpm build:screenshots` (inside `web/mobile`: `vite build --mode screenshots`, then `cap sync ios`) adds a hook for
the App Store screenshots, left out of the other builds: a link opens a page of the demo, in a language, after tapping
buttons by their label. In the iOS Simulator, write it to the app preferences (no confirmation prompt, unlike
`xcrun simctl openurl`). The preferences domain is the app identifier: `IOS_BUNDLE_ID` when set, otherwise
`dev.frankenmanager.mobile`.

```bash
APP_ID=dev.frankenmanager.mobile   # or your IOS_BUNDLE_ID
xcrun simctl launch <device> $APP_ID -AppleLanguages '(en)' -AppleLocale en_US   # interface language of the series
xcrun simctl spawn <device> defaults write $APP_ID CapacitorStorage.fm-screenshot \
  'frankenmanager://demo/projets?tap=Add%20a%20project&n=1'   # change n to replay the same link
xcrun simctl io <device> screenshot shot.png
```

Launch the app in the language of the series (`-AppleLanguages`): the `lang` parameter of the link is not enough
for a whole series. The taps match the labels of that language. The iPhone Duo simulator (runtime
`iOS-27-2-iPhone19-4`) has two screens: `xcrun simctl io <device> screenshot --display=1` takes the folded one.

`pnpm store:screenshots` (`scripts/store/screenshots.sh`, about 15 minutes) takes the whole set of `store/screenshots/`
this way: it builds the screenshot variant for the simulator, captures the six screens in both themes and both
languages on the iPhone 17 Pro, iPhone 17 Pro Max, iPhone Duo and iPad Pro 13-inch (M5) simulators (`capture.sh`, the
app installed again for default preferences), cuts each pair along the diagonal (`mix.sh`) and renders the hero image
(`hero.mjs`, from `hero.html`). It needs ImageMagick 7 and the Chromium of Playwright (`pnpm exec playwright install
chromium`), and leaves the iOS project on the screenshot build: run `pnpm ios` before an archive.

The shots of `store/screenshots/` were taken this way (iPhone 17 Pro Max, iPhone 17 Pro, iPhone Duo and iPad Pro
13-inch simulators, status bar set with `xcrun simctl status_bar … override --time 9:41`). Each one mixes the two
themes: the app follows the system theme, so one shot is taken in light, then `xcrun simctl ui <device> appearance
dark` and a second shot of the same screen, a second later. The two are then cut along the diagonal (light above,
dark below), with a thin light line on the cut:

```bash
W=1206; H=2622   # size of the shots
magick light.png dark.png \( -size ${W}x${H} xc:black -fill white -draw "polygon $W,0 $W,$H 0,$H" \) \
  -alpha off -compose over -composite -stroke 'rgba(255,255,255,0.55)' -strokewidth 6 -draw "line $W,0 0,$H" \
  -strip mixed.png
```

## Session tokens

The instance list (name, address, session id, daemon version) is not secret and stays in Capacitor Preferences. The
session tokens are kept in the secure storage of the system, by a small plugin written in the app (`SecureStore`,
registered by `MainViewController.swift` and `MainActivity.java`):

- **iOS**: Keychain, generic password items of the app, `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` (never
  synced to iCloud nor restored on another device).
- **Android**: an AES-256 key generated in the Android Keystore encrypts each token (AES/GCM); the ciphertext is kept
  in a private file left out of backups (`res/xml/backup_rules.xml`, `data_extraction_rules.xml`).
- In the browser (`pnpm dev`), there is no secure storage: tokens go to `sessionStorage`.

The demo app (0.1.0) stored the tokens in Preferences: at first launch, they are moved to the secure store, then
removed from Preferences.

## Pairing with a QR code

The dashboard of an instance shows a pairing QR code (remote access on, `DASHBOARD_ACCESS=lan` or `internet`). It
holds a link:

```
frankenmanager://pair?v=1&url=<https base URL of the remote access>&code=<one-time code>&name=<host name>&ca=<SHA-256 of the root CA>
```

- In the app, "Appairer avec un QR code" (instance list, add form) opens the camera (`@capacitor/barcode-scanner`).
- With the system camera, the link opens the app through the `frankenmanager` URL scheme (`CFBundleURLTypes` in
  `ios/App/App/Info.plist`, intent filter in `android/app/src/main/AndroidManifest.xml`).

Both lead to the pairing page: it refuses anything that is not a `frankenmanager://pair` link of version 1 or whose
`url` is not HTTPS, shows the host name, the address and the fingerprint of the root CA (to compare with the CA
installed on the phone), then exchanges the code: `POST <url>/api/v1/auth/pair` with `{"code", "device"}`, answered
like a sign-in (`{"token", "session"}`). An invalid, expired or used code answers `401`, too many attempts `429`. The
instance and its token are then stored, replacing the session of an instance already at that address, and the
dashboard opens.

The phone still has to trust the certificate of the remote access (below): the app goes through the system TLS
checks, the fingerprint is only shown.

## On the daemon side

The remote access of the daemon answers the CORS requests of the app (`capacitor://localhost` on iOS,
`https://localhost` on Android, `http://localhost:<port>` for the dev server), without
`Access-Control-Allow-Credentials`: the app signs in with a Bearer token, the cookie never crosses origins. Once:

```bash
frankenmanager daemon password                        # dashboard password
frankenmanager configuration set DASHBOARD_ACCESS lan # access from the local network, HTTPS on 9443
frankenmanager trust-ca on                            # share the CA, to install on the phone
```

### Trusting the certificate

The remote access presents an mkcert certificate. The phone must trust the FrankenManager CA:

- **iOS**: open the page of the `trust-ca` QR code in Safari, install the profile (Settings › Profile Downloaded),
  then Settings › General › About › Certificate Trust Settings: turn full trust on.
- **Android**: install the certificate (Settings › Security › Encryption & credentials › Install a certificate ›
  CA certificate). The app accepts the user CAs (`res/xml/network_security_config.xml`).

## Build and run

From `web/` (one install and one lockfile for the whole workspace):

```bash
pnpm --dir web install
pnpm --dir web mobile:dev       # in the browser: http://localhost:5174 (the remote access must answer)
pnpm --dir web mobile:build     # web bundle in web/mobile/dist, then type check
pnpm --dir web mobile:ios       # iOS signing, version, build, cap sync ios
pnpm --dir web mobile:android   # version, build, cap sync android
```

Inside `web/mobile`, the same commands are `pnpm dev`, `pnpm build`, `pnpm ios`, `pnpm android`, plus `pnpm sync`
(both platforms), `pnpm assets` (icons and splash screens) and `pnpm deploy` (assets, then iOS and Android).

`pnpm ios` and `pnpm android` prepare the native project without opening Xcode or Android Studio: run the app from
the IDE afterwards (`npx cap open ios`, `npx cap open android`), or from the command line for Android. The mobile app
is not part of `make web`, which only builds the embedded dashboard.

### Environment variables

Signing and version settings are personal and never committed: they come from the environment, or from
`web/mobile/.env.local` (git-ignored). `.env.example` lists them:

| Variable | Use |
|---|---|
| `IOS_TEAM_ID` | Apple signing team (Xcode › Settings › Accounts, or developer.apple.com › Membership) |
| `IOS_BUNDLE_ID` | iOS app identifier, unique to your team (default `dev.frankenmanager.mobile`) |
| `ANDROID_APPLICATION_ID` | Identifier of the installed Android app (default `dev.frankenmanager.mobile`) |
| `ANDROID_KEYSTORE`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` | Signing of the Android release build |
| `APP_VERSION`, `APP_BUILD` | Version and build number (see below) |

### iOS

```bash
cp web/mobile/.env.example web/mobile/.env.local   # then IOS_TEAM_ID=… and IOS_BUNDLE_ID=… (or export them)
pnpm --dir web mobile:ios                          # signing, version, build, cap sync ios
open web/mobile/ios/App/App.xcodeproj              # or npx cap open ios
```

`pnpm ios` writes `ios/signing.local.xcconfig` (git-ignored), which `ios/signing.xcconfig` includes over its default
values, in Debug and Release. `pnpm ios:signing` does it alone. A free Apple account is enough for your own iPhone
(the app then expires after 7 days), with an app identifier of your own (an identifier belongs to one team only).

Do not pick the team in the Signing & Capabilities tab of Xcode: it would write it into `project.pbxproj`, which is
committed, where it would win over the local file. Plug the iPhone in, choose it as destination, then Run. At first
launch, iOS asks for local network access: accept it. On the iPhone, approve the developer in Settings › General ›
VPN & Device Management, and turn the developer mode on.

### Android

```bash
pnpm --dir web mobile:android   # version, build, cap sync android
npx cap open android            # from web/mobile, then Run on the plugged-in phone (USB debugging on)
```

Without Android Studio (Gradle needs a JDK, the one of Android Studio is fine), from `web/mobile`:

```bash
export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
pnpm android:install   # build, cap sync, install the debug app on the plugged-in phone
pnpm android:apk       # or only the APK: android/app/build/outputs/apk/debug/app-debug.apk
```

Nothing to configure for testing: the debug build is signed by the machine's debug key (`~/.android/debug.keystore`,
outside the repository). The release build (`./gradlew assembleRelease`) is signed with `ANDROID_KEYSTORE` when set,
unsigned otherwise; `*.jks` and `*.keystore` files are git-ignored. The app needs Android 8.0 or later (API 26, for
the QR code scanner).

## Version and build number

One source for iOS, Android and the app (shown at the bottom of the instance list), `scripts/app-version.mjs`:

| | Order of precedence |
|---|---|
| Version (`CFBundleShortVersionString`, `versionName`) | `APP_VERSION`, otherwise the last `mobile-vX.Y.Z` git tag, otherwise `package.json` |
| Build (`CFBundleVersion`, `versionCode`) | `APP_BUILD`, otherwise the git commit count (increasing, as the stores require) |

The script runs before every native command (`pnpm ios`, `android`, `android:apk`, `android:install`, `sync`, and
so `deploy`); `pnpm version:sync` runs it alone. It writes `ios/version.local.xcconfig` and
`android/version.local.properties`, git-ignored: a build started from Xcode or Android Studio takes the last written
values. A version must look like `X.Y.Z`. To release one: `git tag -a mobile-v0.2.0 -m "Mobile app 0.2.0"`, then
`pnpm ios` or `pnpm android`.

## Icons and splash screens

The sources are in `assets/`: `icon-only.png`, `icon-foreground.png` and `icon-background.png` (1024 px),
`splash.png` and `splash-dark.png` (2732 px). After changing them:

```bash
pnpm assets       # from web/mobile: capacitor-assets generate, then scripts/assets-extras.mjs
```

The generator makes the iOS, Android and web icons and screens; `assets-extras.mjs` adds what it cannot do, and
would erase on each generation: the Android adaptive icon foreground and its monochrome layer, the dark and tinted
iOS icons, real WebP images and the web manifest fields.

At launch, the native screen stays until the page has painted its identical HTML splash screen, with a spinner
(`index.html`, images of `public/splash/`). It fades out when the app is ready: right away for the instance list,
after the first read of the instance for the dashboard, 10 seconds at most (`src/lib/splash.ts`).

The launch screen of Android 12 and later (icon on a plain background) takes its colours from
`res/values/splash.xml` and `res/values-night/splash.xml`. The Liquid Glass icon of iOS 26 is made in Apple's Icon
Composer (`.icon` file), which the generator does not produce.

## Git hygiene

Committed: the sources, the native projects (as generated by Capacitor, plus the app's own plugins and settings) and
the icon sources. Ignored: `node_modules/`, `dist/`, the web assets copied into the native projects
(`ios/App/App/public`, `android/app/src/main/assets/public`), Xcode and Gradle build outputs, `.env` / `.env.local`,
the local signing and version files, and signing keys (`*.jks`, `*.keystore`, `*.p12`, `*.mobileprovision`, `*.p8`).

## Limits

- No biometric lock of the app yet.
- No API versioning: the app expects a daemon with the routes of this repository.
- Not built by the CI nor published to the stores yet.
