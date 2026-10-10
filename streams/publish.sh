#!/usr/bin/env sh
# Publishes every clip in a loop to an RTSP server (MediaMTX) as
# rtsp://HOST:PORT/<clip-name>. Restarts a publisher if it ever exits.
# Usage: ./publish.sh [clips-dir] [rtsp-base-url]
set -eu

CLIPS="${1:-$(dirname "$0")/clips}"
BASE="${2:-${RTSP_PUBLISH_BASE:-rtsp://localhost:8554}}"

for clip in "$CLIPS"/*.mp4; do
  name="$(basename "$clip" .mp4)"
  (
    while true; do
      ffmpeg -hide_banner -loglevel error -re -stream_loop -1 -i "$clip" \
        -c copy -f rtsp -rtsp_transport tcp "$BASE/$name" || true
      echo "publisher for $name exited, restarting in 2s" >&2
      sleep 2
    done
  ) &
  echo "publishing $clip -> $BASE/$name"
done
wait
