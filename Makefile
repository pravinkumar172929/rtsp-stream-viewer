# Convenience targets for local development. See README for details.

PRESETS ?= Test pattern=rtsp://localhost:8554/cam1,Mandelbrot=rtsp://localhost:8554/cam2,Game of Life=rtsp://localhost:8554/cam3,Color bars=rtsp://localhost:8554/cam4

.PHONY: streams backend frontend test up

streams:   ## test RTSP streams on :8554 (needs ffmpeg; mediamtx or docker)
	./scripts/dev-streams.sh

backend:   ## Go backend on :8080
	cd backend && PRESET_STREAMS="$(PRESETS)" go run ./cmd/server

frontend:  ## Vite dev server on :5173 (proxies /api and /ws to :8080)
	cd frontend && npm install && npm run dev

test:      ## backend tests + frontend type-check and build
	cd backend && go vet ./... && go test -race ./...
	cd frontend && npm install && npm run build

up:        ## everything in Docker on :8080
	docker compose up --build
