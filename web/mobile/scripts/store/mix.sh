#!/bin/bash
# Cuts each light/dark pair along the diagonal (light above, dark below), with a thin light line on the cut,
# into <out>/<locale>/<lang>_<device>_<NN>_<name>.png (needs ImageMagick 7). Usage: mix.sh <raw dir> <out dir>
set -eu
RAW=$1; OUT=$2
for light in "$RAW"/*_light.png; do
  base=$(basename "$light" _light.png)
  dark="$RAW/${base}_dark.png"
  [ -f "$dark" ] || { echo "missing $dark"; continue; }
  lang=${base%%_*}
  [ "$lang" = en ] && locale=en-US || locale=fr-FR
  mkdir -p "$OUT/$locale"
  read -r W H < <(magick identify -format "%w %h\n" "$light")
  magick "$light" "$dark" \( -size ${W}x${H} xc:black -fill white -draw "polygon $W,0 $W,$H 0,$H" \) \
    -alpha off -compose over -composite -stroke 'rgba(255,255,255,0.55)' -strokewidth 6 -draw "line $W,0 0,$H" \
    -strip "$OUT/$locale/$base.png"
done
echo mixed
