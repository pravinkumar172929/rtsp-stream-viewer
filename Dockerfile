# syntax=docker/dockerfile:1
#
# One image that contains everything:
#   - the Go backend (API + WebSocket streaming, spawns FFmpeg)
#   - the built React frontend, served by the backend (STATIC_DIR)
#   - optional demo RTSP streams (MediaMTX + FFmpeg loops), DEMO_STREAMS=true
#
# The frontend can still be deployed separately (e.g. Vercel); see README.

# ---------- frontend ----------
FROM node:22-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi
COPY frontend/ ./
# Empty = same origin (the backend serves the frontend in this image).
ARG VITE_BACKEND_URL=
ENV VITE_BACKEND_URL=$VITE_BACKEND_URL
RUN npm run build

# ---------- backend ----------
FROM golang:1.24-bookworm AS api
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---------- runtime ----------
FROM debian:bookworm-slim
ARG MEDIAMTX_VERSION=1.9.3
ARG TARGETARCH
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ffmpeg fonts-dejavu-core ca-certificates curl netcat-openbsd tini; \
    case "${TARGETARCH:-amd64}" in \
      amd64) mtx_arch=amd64 ;; \
      arm64) mtx_arch=arm64v8 ;; \
      *) echo "unsupported arch ${TARGETARCH}"; exit 1 ;; \
    esac; \
    curl -fsSL "https://github.com/bluenviron/mediamtx/releases/download/v${MEDIAMTX_VERSION}/mediamtx_v${MEDIAMTX_VERSION}_linux_${mtx_arch}.tar.gz" \
      | tar -xz -C /usr/local/bin mediamtx; \
    apt-get purge -y curl; apt-get autoremove -y; rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY streams/ /app/streams/
# Demo clips are generated at build time from FFmpeg test sources (no
# downloads, no copyrighted material). 960x540 keeps CPU use modest.
RUN SIZE=960x540 /app/streams/generate-clips.sh /app/streams/clips
COPY docker/entrypoint.sh /app/entrypoint.sh
COPY --from=api /out/server /app/server
COPY --from=web /web/dist /app/public

RUN useradd --system --uid 10001 app && chown -R app /app
USER app

ENV PORT=8080 \
    STATIC_DIR=/app/public \
    DEMO_STREAMS=true
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD nc -z 127.0.0.1 ${PORT} || exit 1
ENTRYPOINT ["/usr/bin/tini", "--", "/app/entrypoint.sh"]
