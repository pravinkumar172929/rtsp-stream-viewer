#!/bin/sh
# Container entrypoint. With DEMO_STREAMS=true it also starts a MediaMTX RTSP
# server and loops the bundled test clips into it, so the deployed app has
# working streams out of the box: rtsp://localhost:8554/cam1 ... cam4.
set -eu

if [ "${DEMO_STREAMS:-false}" = "true" ]; then
  echo "[entrypoint] starting MediaMTX and demo publishers"
  mediamtx /app/streams/mediamtx.yml &

  # Wait until MediaMTX accepts connections (max ~10s).
  i=0
  until nc -z 127.0.0.1 8554 2>/dev/null || [ $i -ge 50 ]; do i=$((i + 1)); sleep 0.2; done

  /app/streams/publish.sh /app/streams/clips rtsp://127.0.0.1:8554 &

  : "${PRESET_STREAMS:=Test pattern=rtsp://localhost:8554/cam1,Mandelbrot=rtsp://localhost:8554/cam2,Game of Life=rtsp://localhost:8554/cam3,Color bars=rtsp://localhost:8554/cam4}"
  export PRESET_STREAMS
fi

exec /app/server
