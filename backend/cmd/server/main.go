// Command server runs the RTSP Stream Viewer backend.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/your-username/rtsp-stream-viewer/backend/internal/config"
	"github.com/your-username/rtsp-stream-viewer/backend/internal/server"
	"github.com/your-username/rtsp-stream-viewer/backend/internal/stream"
)

func main() {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	if err := run(log); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath(cfg.FFmpegPath); err != nil {
		log.Warn("ffmpeg not found, streams will fail until it is installed", "path", cfg.FFmpegPath)
	}

	manager := stream.NewManager(stream.ManagerOptions{
		FFmpeg: stream.FFmpegOptions{
			Path:           cfg.FFmpegPath,
			RTSPTransport:  cfg.RTSPTransport,
			MaxWidth:       cfg.OutputWidth,
			FPS:            cfg.OutputFPS,
			Bitrate:        cfg.VideoBitrate,
			ConnectTimeout: cfg.ConnectTimeout,
			StallTimeout:   cfg.StallTimeout,
		},
		MaxStreams:  cfg.MaxStreams,
		MaxRetries:  cfg.MaxRetries,
		IdleTimeout: cfg.IdleTimeout,
		Logger:      log,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(cfg, manager, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "presets", len(cfg.Presets))
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	deadline := time.Now().Add(cfg.ShutdownTimeout)
	// Stop streams first: this closes every WebSocket, which lets
	// srv.Shutdown finish quickly (it does not wait for hijacked conns).
	manager.Shutdown(deadline)
	sctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
