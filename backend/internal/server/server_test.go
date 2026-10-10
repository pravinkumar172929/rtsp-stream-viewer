package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/pravinkumar172929/rtsp-stream-viewer/backend/internal/config"
	"github.com/pravinkumar172929/rtsp-stream-viewer/backend/internal/stream"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := &config.Config{
		AllowedOrigins: []string{"*"},
		MaxClients:     10,
		MaxStreams:     2,
		Presets:        []config.Preset{{Name: "Demo", URL: "rtsp://localhost:8554/cam1"}},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := stream.NewManager(stream.ManagerOptions{
		FFmpeg: stream.FFmpegOptions{
			Path: "/nonexistent/ffmpeg", RTSPTransport: "tcp", MaxWidth: 640, FPS: 25,
			Bitrate: "500k", ConnectTimeout: time.Second, StallTimeout: time.Second,
		},
		MaxStreams: 2, MaxRetries: 1, IdleTimeout: 50 * time.Millisecond, Logger: log,
	})
	ts := httptest.NewServer(New(cfg, mgr, log).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestHealthAndPresets(t *testing.T) {
	ts := newTestServer(t)

	res, err := http.Get(ts.URL + "/healthz")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %v %v", err, res)
	}

	res, err = http.Get(ts.URL + "/api/presets")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var presets []config.Preset
	if err := json.NewDecoder(res.Body).Decode(&presets); err != nil || len(presets) != 1 {
		t.Fatalf("presets: %v %+v", err, presets)
	}
}

// dialStream connects to the stream endpoint and returns the first status
// message and the close code.
func dialStream(t *testing.T, ts *httptest.Server, rtsp string) (stream.Status, websocket.StatusCode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/stream?url=" + url.QueryEscape(rtsp)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	var last stream.Status
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return last, websocket.CloseStatus(err)
		}
		if typ == websocket.MessageText {
			if err := json.Unmarshal(data, &last); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestStreamRejectsInvalidURL(t *testing.T) {
	ts := newTestServer(t)
	st, code := dialStream(t, ts, "http://example.com/video.mp4")
	if code != closeBadRequest || st.State != stream.StateError || !st.Fatal {
		t.Fatalf("got code %d status %+v", code, st)
	}
}

func TestStreamReportsSourceFailure(t *testing.T) {
	ts := newTestServer(t)
	// FFmpeg path does not exist, so the source fails permanently.
	st, code := dialStream(t, ts, "rtsp://127.0.0.1:1/none")
	if code != closeSourceFailed || st.State != stream.StateError || st.Message == "" {
		t.Fatalf("got code %d status %+v", code, st)
	}
}
