#!/usr/bin/env sh
# Generates short, looping H.264 test clips with FFmpeg's built-in sources,
# so the demo needs no downloaded (or copyrighted) video files.
# Usage: ./generate-clips.sh [output-dir]
set -eu

OUT="${1:-$(dirname "$0")/clips}"
FONT="${FONT:-/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf}"
DURATION="${DURATION:-30}"
SIZE="${SIZE:-1280x720}"   # smaller clips = less CPU for the demo on tiny instances
mkdir -p "$OUT"

label() {
  # Camera name and the clip's running time in the top-right corner (the
  # viewer's own overlays sit top-left and along the bottom).
  echo "drawtext=fontfile=$FONT:text='$1':x=w-tw-24:y=24:fontsize=28:fontcolor=white:box=1:boxcolor=black@0.55:boxborderw=8,drawtext=fontfile=$FONT:text='%{pts\:hms}':x=w-tw-24:y=74:fontsize=22:fontcolor=white:box=1:boxcolor=black@0.55:boxborderw=6"
}

encode() { # name lavfi-source label
  if [ -f "$OUT/$1.mp4" ]; then echo "exists: $OUT/$1.mp4"; return; fi
  echo "generating $OUT/$1.mp4"
  ffmpeg -hide_banner -loglevel error -y \
    -f lavfi -i "$2" -t "$DURATION" \
    -vf "$(label "$3"),format=yuv420p" \
    -c:v libx264 -preset veryfast -profile:v baseline -g 50 -keyint_min 50 -sc_threshold 0 -b:v 800k \
    -movflags +faststart "$OUT/$1.mp4"
}

encode cam1 "testsrc2=size=$SIZE:rate=25"                        "CAM 1 · Test pattern"
encode cam2 "mandelbrot=size=$SIZE:rate=25"                      "CAM 2 · Mandelbrot"
encode cam3 "life=size=640x360:rate=25:mold=10:ratio=0.1:death_color=#0b1d2a:life_color=#38bdf8,scale=$SIZE:flags=neighbor" "CAM 3 · Game of Life"
encode cam4 "smptehdbars=size=$SIZE:rate=25"                     "CAM 4 · Color bars"
