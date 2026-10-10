#!/usr/bin/env sh
# Starts local test RTSP streams for development:
#   rtsp://localhost:8554/cam1 ... cam4
# Uses a local `mediamtx` binary if installed, otherwise the Docker image.
# Requires ffmpeg. Stop with Ctrl+C.
set -eu
cd "$(dirname "$0")/../streams"

./generate-clips.sh

if command -v mediamtx >/dev/null 2>&1; then
  mediamtx mediamtx.yml &
  MTX_PID=$!
  trap 'trap - INT TERM EXIT; kill $MTX_PID 2>/dev/null; kill 0' INT TERM EXIT
else
  docker rm -f rtsp-viewer-mediamtx >/dev/null 2>&1 || true
  docker run -d --name rtsp-viewer-mediamtx -p 8554:8554 \
    -v "$PWD/mediamtx.yml:/mediamtx.yml:ro" bluenviron/mediamtx:1.9.3 >/dev/null
  trap 'trap - INT TERM EXIT; docker rm -f rtsp-viewer-mediamtx >/dev/null; kill 0' INT TERM EXIT
fi

sleep 1
./publish.sh ./clips rtsp://localhost:8554
