# App Store listing

Everything App Store Connect asks for the FrankenManager app, in English (`en-US`) and French (`fr-FR`). The `metadata/` folder follows the layout of [fastlane deliver](https://docs.fastlane.tools/actions/deliver/), so a future publishing workflow can upload it as is; until then, copy each file into the matching App Store Connect field.

## Text fields (`metadata/<locale>/`)

| File | App Store Connect field | Limit |
|---|---|---|
| `name.txt` | Name | 30 characters |
| `subtitle.txt` | Subtitle | 30 characters |
| `promotional_text.txt` | Promotional Text (editable without a new version) | 170 characters |
| `description.txt` | Description | 4,000 characters |
| `keywords.txt` | Keywords (comma-separated, no spaces needed) | 100 bytes |
| `release_notes.txt` | What's New in This Version | 4,000 characters |
| `support_url.txt` | Support URL | — |
| `marketing_url.txt` | Marketing URL | — |
| `privacy_url.txt` | Privacy Policy URL | — |

Shared: `copyright.txt`, `primary_category.txt` (Developer Tools), `secondary_category.txt` (Utilities).

The privacy policy is [`PRIVACY.md`](PRIVACY.md) (English, then French), linked from `privacy_url.txt` once this folder is on `main`.

## App Privacy

Answer **"No, we do not collect data from this app"**: the app has no account, analytics, advertising, crash reporting or third-party SDK sending data; it only talks to the user's own instances, and data that never leaves the device (or goes only to a server the user runs) is not "collected" in Apple's sense.

## Age rating

Every question **None / No** → **4+**. The app has no unrestricted web browser: site links open in Safari.

## Export compliance

The app only uses the encryption built into iOS (HTTPS, Keychain): exempt. `ITSAppUsesNonExemptEncryption` is `false` in `Info.plist`, so App Store Connect does not ask again at each upload.

## App Review

The app needs a FrankenManager instance, and the reviewer has none: the built-in **demo mode** is the way in (guideline
2.1). "Try the demo" on the first screen opens a simulated instance, with fictional projects and services, that runs
entirely on the device: no server to keep up during the review, no account or password to hand over.
`metadata/review_information/notes.txt` walks the reviewer through it.

Contact information (first name, last name, phone, email) is entered in App Store Connect only: it is not stored here.

## Icon

`ios/App/App/Assets.xcassets/AppIcon.appiconset/AppIcon-512@2x.png` (1024 × 1024, no transparency), with its dark and tinted variants. Nothing to upload separately: App Store Connect takes it from the build.

## Screenshots (`screenshots/<locale>/`)

The app runs on iPhone and iPad, so both sets are required, portrait, 1 to 10 per locale. Six per device and locale,
taken from the demo mode (fictional data only) in the iOS Simulator, with the native tab bar on iPhone:

| Device | Simulator | Size (pixels) | Files |
|---|---|---|---|
| iPhone 6.9" | iPhone 17 Pro Max | 1320 × 2868 | `iphone69_0N_*.png` |
| iPad 13" | iPad Pro 13-inch (M5) | 2064 × 2752 | `ipad13_0N_*.png` |

`01_overview`, `02_projects`, `03_containers`, `04_add_project`, `05_pairing`, `06_ssl`, in `en-US` and `fr-FR`.
fastlane deliver tells the device from the image size; the number sets the order. Apple scales them down for the
smaller devices. To take them again, see "Screenshot builds" in [`../README.md`](../README.md). Never use a real
machine's domains.

## Version

The version comes from the last `mobile-vX.Y.Z` tag (or `APP_VERSION`), the build number from the commit count (`scripts/app-version.mjs`): tag `mobile-v1.0.0` before the first archive.
