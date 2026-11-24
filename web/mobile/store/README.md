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

The app is useless without a FrankenManager instance, and the reviewer has none: App Review (guideline 2.1) needs a way in. `metadata/review_information/notes.txt` expects a **demo instance** reachable from the Internet (`DASHBOARD_ACCESS=internet`, a public name with its own certificate, a password): replace `<DEMO_INSTANCE_URL>` and `<DEMO_PASSWORD>` before submitting, and keep that instance up during the review. A built-in demo mode (simulated data, as in the dashboard prototype) would remove that dependency.

Contact information (first name, last name, phone, email) is entered in App Store Connect only: it is not stored here.

## Icon

`ios/App/App/Assets.xcassets/AppIcon.appiconset/AppIcon-512@2x.png` (1024 × 1024, no transparency), with its dark and tinted variants. Nothing to upload separately: App Store Connect takes it from the build.

## Screenshots (`screenshots/<locale>/`)

The app runs on iPhone and iPad, so both sets are required, portrait, 1 to 10 per locale:

| Device | Size (pixels) |
|---|---|
| iPhone 6.9" | 1320 × 2868 |
| iPad 13" | 2064 × 2752 |

Apple scales them down for the smaller devices. Use fictional projects only: never a real machine's domains.

## Version

The version comes from the last `mobile-vX.Y.Z` tag (or `APP_VERSION`), the build number from the commit count (`scripts/app-version.mjs`): tag `mobile-v1.0.0` before the first archive.
