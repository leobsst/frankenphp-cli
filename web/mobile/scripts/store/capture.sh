#!/bin/bash
# Store screenshots of one simulator, from the demo of the screenshot build (`pnpm build:screenshots`, see the README):
# each screen in light then in dark, in English then in French, as <out>/<lang>_<device key>_<NN>_<name>_<theme>.png.
# The app is installed again first: default preferences (text size, theme), no instance but the demo.
#
# Usage: capture.sh <out dir> <App.app built for the simulator> <device id> <device key> <display|-> [screens...]
#   display: the screen of the simulator to capture (1: the folded screen of the iPhone Duo), - for the only one.
set -u
OUT=$1; BUNDLE=$2; DEV=$3; KEY=$4; DISPLAY_N=$5; shift 5
ONLY="${*:-01 02 03 04 05 06}"
APP=$(/usr/libexec/PlistBuddy -c 'Print CFBundleIdentifier' "$BUNDLE/Info.plist")
N=$(( $(date +%s) % 100000 ))

xcrun simctl boot "$DEV" 2>/dev/null; xcrun simctl bootstatus "$DEV" -b >/dev/null
xcrun simctl uninstall "$DEV" "$APP" 2>/dev/null
xcrun simctl install "$DEV" "$BUNDLE"
xcrun simctl status_bar "$DEV" override --time 9:41 --batteryState charged --batteryLevel 100 \
  --cellularMode active --cellularBars 4 --wifiMode active --wifiBars 3 --dataNetwork wifi
xcrun simctl ui "$DEV" appearance light

shot() { if [ "$DISPLAY_N" = "-" ]; then xcrun simctl io "$DEV" screenshot "$1" >/dev/null 2>&1; else xcrun simctl io "$DEV" screenshot --display="$DISPLAY_N" "$1" >/dev/null 2>&1; fi; }

for lang in en fr; do
  if [ $lang = en ]; then LOC=en_US; ADD='Add%20a%20project'; PAIR='Pair%20a%20device'; else LOC=fr_FR; ADD='Ajouter%20un%20projet'; PAIR='Appairer%20un%20appareil'; fi
  xcrun simctl terminate "$DEV" "$APP" 2>/dev/null
  xcrun simctl launch "$DEV" "$APP" -AppleLanguages "($lang)" -AppleLocale "$LOC" >/dev/null
  sleep 8
  for s in $ONLY; do
    case $s in
      01) LINK="demo/?x=1"; NAME=overview; WAIT=6 ;;
      02) LINK="demo/projets?x=1"; NAME=projects; WAIT=6 ;;
      03) LINK="demo/conteneurs?x=1"; NAME=containers; WAIT=6 ;;
      04) LINK="demo/projets?tap=$ADD"; NAME=add_project; WAIT=8 ;;
      05) LINK="demo/acces-distant?tap=$PAIR"; NAME=pairing; WAIT=9 ;;
      06) LINK="demo/ssl?x=1"; NAME=ssl; WAIT=6 ;;
    esac
    N=$((N + 1))
    xcrun simctl spawn "$DEV" defaults write "$APP" CapacitorStorage.fm-screenshot "frankenmanager://$LINK&n=$N"
    sleep "$WAIT"
    shot "$OUT/${lang}_${KEY}_${s}_${NAME}_light.png"
    xcrun simctl ui "$DEV" appearance dark
    sleep 2
    shot "$OUT/${lang}_${KEY}_${s}_${NAME}_dark.png"
    xcrun simctl ui "$DEV" appearance light
    sleep 1.5
  done
done
echo "done $KEY"
