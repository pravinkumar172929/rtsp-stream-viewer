# RTSP Stream Viewer

Add RTSP camera URLs and watch them live in the browser, one stream or a whole grid at once.

**Stack:** React + TypeScript (Vite) frontend · Go backend · FFmpeg · WebSockets · MediaMTX for test streams

- **Live demo:** `<your-deployment-url>`
- **Test streams on the demo:** click **Add all** under *Demo streams*, or add `rtsp://localhost:8554/cam1` … `cam4` (these are served by an RTSP server running next to the backend).
- No login needed.

---

## Features

- Add any `rtsp://` / `rtsps://` URL (credentials in the URL work, and are masked in the UI and in logs)
- Multi-stream grid: auto-fit, or 1–4 fixed columns; responsive down to phone width
- Per-stream play/pause, fullscreen (button or double-click), PNG snapshot, remove
- Pause all / play all
- Live status for every tile: connecting, live, reconnecting, error (with the reason and a retry button)
- Stats per tile: resolution, bitrate, number of viewers sharing the stream
- Stream list and layout are remembered (localStorage)
- Backend health indicator, with a hint when a sleeping free-tier backend is waking up

## Architecture

```
                      ┌──────────────────────────── Go backend ───────────────────────────────┐
 RTSP camera  ──RTSP──▶  Hub (one per unique URL)                                             │
 / MediaMTX            │    FFmpeg: RTSP ─▶ MPEG-1 video in MPEG-TS on stdout                 │
                      │    reader: splits stdout into 188-byte-aligned chunks                 │
                      │    fan-out: non-blocking send to every subscriber's buffered channel  │
                      │                                                                        │
                      │  Manager: map[url]*Hub, limits, stats       HTTP: /healthz /api/*     │
                      └──────────────┬─────────────────────────────────────────────────────────┘
                                     │ WebSocket /ws/stream?url=…
                                     │   text frames:   {"type":"status","state":"live",…}
                                     │   binary frames: MPEG-TS packets
                      ┌──────────────▼───────── React frontend ─────────┐
                      │ StreamTile ─▶ StreamSession (plain TS class)     │
                      │   WebSocket + JSMpeg (WASM decode, WebGL render) │
                      └──────────────────────────────────────────────────┘
```

### Request flow

1. The user adds a URL. The frontend validates it and opens `GET /ws/stream?url=<rtsp-url>`.
2. The backend normalizes the URL and asks the **Manager** for the **Hub** of that URL. The first viewer creates the hub, which starts FFmpeg.
3. FFmpeg pulls RTSP (over TCP), scales the video to at most 960px wide, encodes MPEG-1 at 25 fps with one keyframe per second, and writes MPEG-TS to stdout.
4. The hub reads stdout, cuts it into chunks made of whole 188-byte TS packets, and sends each chunk to every subscriber without blocking.
5. Each WebSocket handler forwards chunks as binary frames and status changes as JSON text frames.
6. In the browser, `StreamSession` feeds binary frames into JSMpeg, which decodes in WebAssembly and draws on a WebGL canvas.

### Key design decisions

**One FFmpeg process per camera, not per viewer.** The hub pattern means 50 browsers watching the same camera cost one RTSP connection and one transcode. When the last viewer leaves, the hub waits `IDLE_TIMEOUT` (5s), so a quick pause/resume or a page reload reuses the running process, and then stops FFmpeg.

**MPEG-1 + JSMpeg instead of H.264 + Media Source Extensions.** I chose MPEG-1 because:

- it plays the same everywhere, including iOS Safari (no MSE on iPhone);
- it works with any camera codec (H.264, H.265, MJPEG), because the stream is re-encoded;
- every keyframe carries a sequence header, so a viewer who joins mid-stream gets a picture within one second, with no init segment to cache or replay;
- latency is low (no B-frames, `-flush_packets 1`, no player-side buffering).

The cost is server CPU, since every camera is transcoded once, and MPEG-1 needs more bandwidth than H.264 for the same quality. The tradeoffs section covers the alternative.

**Slow clients can't stall anyone else.** Each subscriber has a 256-chunk buffer. Sends never block. A viewer that falls 64 chunks behind in a row is disconnected (close code 4003) and its browser reconnects fresh.

**Errors are reported in-band.** Browsers don't expose the HTTP status of a failed WebSocket handshake to JavaScript, so the server always accepts the upgrade and then reports problems as a JSON status plus an application close code:

| Code | Meaning | Frontend behavior |
|---|---|---|
| 4000 | Source failed permanently | Shows the reason and a Retry button |
| 4001 | Invalid URL | Same |
| 4002 | Server at stream/viewer limit | Same |
| 4003 | Client too slow | Reconnects automatically |
| other (1006, 1001…) | Network drop, backend restart | Reconnects with backoff (1s → 10s, up to 8 tries) |

**Two layers of retry.**

- *Backend:* if FFmpeg exits, the hub restarts it with exponential backoff (1s → 15s) while keeping viewers connected and informed (`reconnecting` status with the reason). A source that has never been live is given up after `MAX_RETRIES` failures, or immediately for errors that retrying can't fix (401, 404, unresolvable host). A source that *was* live (a camera rebooting) is retried for as long as anyone is watching.
- *Frontend:* if the WebSocket itself drops, the tile reconnects with backoff.

**FFmpeg errors become human messages.** stderr is captured and classified, for example: "authentication failed: check the username and password in the URL", "connection refused: is the RTSP server running on that host and port?", or "stream not found on the server: check the path in the URL". A watchdog kills FFmpeg if it produces no output for `STALL_TIMEOUT` (15s), and `-timeout` makes unreachable hosts fail in about 10s instead of hanging.

**Pause = disconnect.** A live stream has nothing worth buffering, so pausing closes the WebSocket, which frees bandwidth and, if nobody else is watching, the FFmpeg process. The last frame stays on screen as a still. Resume reconnects.

**Browser resource limits.** Each tile uses one WebGL context. On teardown the context is released explicitly (`WEBGL_lose_context`), and the grid is capped at 12 tiles to stay under the ~16 contexts browsers allow per page.

**No database.** Server state is in memory, and the user's stream list lives in their browser. See *Scaling further* for when that would change.

## Project structure

```
backend/
  cmd/server/main.go          entry point, graceful shutdown
  internal/config/            env-based configuration
  internal/stream/
    hub.go                    per-URL FFmpeg lifecycle + fan-out
    manager.go                URL → hub registry, limits, stats
    ffmpeg.go                 FFmpeg args, packet-aligned reader, stall watchdog
    errors.go                 FFmpeg stderr → user-facing errors
    url.go                    validation, normalization, credential redaction
  internal/server/
    server.go                 routes, CORS, static frontend
    ws.go                     WebSocket protocol
frontend/
  src/lib/StreamSession.ts    WebSocket + JSMpeg player, reconnects, stats
  src/components/             AddStreamForm, StreamTile, icons
  src/hooks/                  stream list (localStorage), backend health/presets
  public/vendor/jsmpeg.min.js JSMpeg decoder (MIT, phoboslab/jsmpeg)
streams/                      MediaMTX config, test clip generator, publisher
docker/entrypoint.sh          starts demo streams (optional) + backend
Dockerfile                    builds frontend + backend + demo streams into one image
```

## Running locally

### Option A: Docker (one command)

```bash
docker compose up --build
```

Open http://localhost:8080 and click **Add all** under *Demo streams*. The container runs the backend, serves the frontend, and runs MediaMTX with four looping test streams. These are also reachable from your machine, for example `ffplay rtsp://localhost:8554/cam1`.

### Option B: Run the parts separately (for development)

Requirements: Go 1.23+, Node 20+, FFmpeg, and either [MediaMTX](https://github.com/bluenviron/mediamtx/releases) or Docker.

```bash
# terminal 1: test RTSP streams at rtsp://localhost:8554/cam1..cam4
make streams        # or: ./scripts/dev-streams.sh

# terminal 2: backend on :8080
make backend        # or: cd backend && go run ./cmd/server

# terminal 3: frontend on :5173 (proxies /api and /ws to :8080)
make frontend       # or: cd frontend && npm install && npm run dev
```

Open http://localhost:5173.

To publish your own video file as a test stream:

```bash
ffmpeg -re -stream_loop -1 -i my-video.mp4 -c copy -f rtsp rtsp://localhost:8554/mystream
```

### Tests

```bash
make test           # go vet + go test -race, frontend type-check + build
```

Backend tests cover URL validation and redaction, TS packet alignment, error classification, backoff, hub sharing and limits, fatal-error shutdown, and the WebSocket protocol end to end (with `httptest`).

## Deployment

The backend needs a host that allows long-lived WebSocket connections and lets you run FFmpeg, so a container platform rather than serverless functions.

### Backend on Railway (or Render)

1. Create a new project from this GitHub repo. `railway.json` / `render.yaml` point the platform at the root `Dockerfile`.
2. Environment variables:
   - `DEMO_STREAMS=true` runs the bundled MediaMTX test streams inside the container.
   - `ALLOWED_ORIGINS=https://<your-frontend>.vercel.app` (`*` also works).
3. The platform provides `PORT`. Health check path: `/healthz`.

This image also serves the frontend, so the backend URL alone is already a complete working demo.

> Render's free plan has 0.1 CPU, which is not enough to transcode video smoothly. Use Railway or a paid Render instance, or lower `OUTPUT_MAX_WIDTH` / `VIDEO_BITRATE`.

### Frontend on Vercel

1. Import the repo in Vercel and set **Root Directory** to `frontend` (the Vite preset is detected; `vercel.json` is included).
2. Add the environment variable `VITE_BACKEND_URL=https://<your-backend-host>` (https; the app derives `wss://` from it).
3. Deploy.

## Configuration (backend environment variables)

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `ALLOWED_ORIGINS` | `*` | Comma-separated origins allowed for CORS and WebSocket |
| `STATIC_DIR` | *(empty)* | Serve the built frontend from this directory |
| `PRESET_STREAMS` | *(empty)* | `Name=rtsp://…,Other=rtsp://…` shown as one-click demo streams |
| `DEMO_STREAMS` | `true` in Docker | Start MediaMTX + test streams in the container |
| `FFMPEG_PATH` | `ffmpeg` | FFmpeg binary |
| `RTSP_TRANSPORT` | `tcp` | `tcp`, `udp` or `auto` (TCP is the most reliable through NAT/cloud) |
| `OUTPUT_MAX_WIDTH` | `960` | Downscale wider sources (aspect ratio kept) |
| `OUTPUT_FPS` | `25` | 24, 25, 30, 50 or 60 (MPEG-1 constraint) |
| `VIDEO_BITRATE` | `1000k` | MPEG-1 target bitrate |
| `MAX_STREAMS` | `16` | Max concurrent unique sources (FFmpeg processes) |
| `MAX_CLIENTS` | `200` | Max concurrent WebSocket viewers |
| `MAX_RETRIES` | `5` | Failures before giving up on a source that never went live |
| `IDLE_TIMEOUT` | `5s` | Keep FFmpeg running after the last viewer leaves |
| `CONNECT_TIMEOUT` | `10s` | RTSP socket timeout |
| `STALL_TIMEOUT` | `15s` | Restart FFmpeg if no output for this long |
| `LOG_LEVEL` | `info` | `debug` for request logs |

API: `GET /healthz`, `GET /api/presets`, `GET /api/streams` (running streams with viewer counts and bytes, useful for monitoring), `GET /ws/stream?url=…`.

## Tradeoffs, limitations and next steps

- **CPU vs. compatibility.** Transcoding to MPEG-1 cost about 5–10% of one CPU core per 960×540 stream in local testing. A 1080p camera costs more, mostly for decoding the source. For H.264 cameras, the next step would be a second pipeline that remuxes without re-encoding (`-c:v copy` into fragmented MP4, played with Media Source Extensions), keeping MPEG-1 as the fallback for iOS and non-H.264 sources. WebRTC (for example via MediaMTX's WebRTC output) is the option for sub-second latency.
- **No audio.** Disabled on purpose (`-an`) to save bandwidth in grids. JSMpeg supports MP2 audio if it's ever needed.
- **SSRF.** The server connects to whatever RTSP host a user enters. That's fine for a demo, but a production deployment should add authentication and an allow-list, or block private IP ranges.
- **Scaling further.** One instance handles many viewers per camera thanks to fan-out. Running several instances would need a shared registry (for example Redis) mapping each camera URL to the instance that owns its hub, plus routing viewers to that instance, so each camera is still pulled once. User accounts and server-side stream lists would be the point to add a database.

## Credits

- [JSMpeg](https://github.com/phoboslab/jsmpeg) by Dominic Szablewski (MIT): MPEG-1/TS decoder and renderer, vendored in `frontend/public/vendor`.
- [MediaMTX](https://github.com/bluenviron/mediamtx) (MIT): RTSP server for the test streams.
- [coder/websocket](https://github.com/coder/websocket) (ISC): Go WebSocket library.
