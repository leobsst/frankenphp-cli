#!/bin/bash
# All the store screenshots (store/screenshots/): screenshot build of the app for the simulator, the six screens on
# the four simulators in both themes and both languages (capture.sh), the light/dark diagonal (mix.sh), then the hero
# image (hero.mjs). Run from web/mobile: `pnpm store:screenshots`. About 15 minutes.
#
# Needs Xcode with the simulators below (names as in `xcrun simctl list devices`), ImageMagick 7 and the Chromium of
# Playwright (`pnpm exec playwright install chromium`). Leaves the iOS project on the screenshot build: run
# `pnpm ios` before an archive.
set -euo pipefail
cd "$(dirname "$0")/../.."
HERE=scripts/store
WORK=$(mktemp -d)
RAW=$WORK/raw; mkdir -p "$RAW"

# key | simulator | screen to capture (1: the folded screen of the iPhone Duo)
DEVICES=(
  "iphone63|iPhone 17 Pro|-"
  "iphone69|iPhone 17 Pro Max|-"
  "iphoneduo|iPhone Duo|1"
  "ipad13|iPad Pro 13-inch (M5)|-"
)

pnpm build:screenshots
xcodebuild -project ios/App/App.xcodeproj -scheme App -configuration Debug -destination 'generic/platform=iOS Simulator' \
  -derivedDataPath "$WORK/build" CODE_SIGNING_ALLOWED=NO build -quiet
BUNDLE=$WORK/build/Build/Products/Debug-iphonesimulator/App.app

for entry in "${DEVICES[@]}"; do
  IFS='|' read -r key name display <<< "$entry"
  id=$(xcrun simctl list devices available | grep -F "    $name (" | head -1 | sed -E 's/.*\(([0-9A-F-]{36})\).*/\1/')
  [ -n "$id" ] || { echo "Simulator not found: $name" >&2; exit 1; }
  bash "$HERE/capture.sh" "$RAW" "$BUNDLE" "$id" "$key" "$display"
done

bash "$HERE/mix.sh" "$RAW" store/screenshots
node "$HERE/hero.mjs" "$RAW" store/screenshots
echo "Raw shots kept in $RAW"
