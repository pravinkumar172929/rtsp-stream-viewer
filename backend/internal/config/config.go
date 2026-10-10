// Package config loads runtime settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Preset is a named stream URL offered to the UI as a one-click option.
type Preset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Config holds every tunable of the server. All fields have sane defaults so
// the binary runs with zero configuration in development.
type Config struct {
	Addr           string
	AllowedOrigins []string
	StaticDir      string // optional: serve the built frontend from here

	FFmpegPath    string
	RTSPTransport string // tcp | udp | auto
	OutputWidth   int    // max output width, aspect ratio is preserved
	OutputFPS     int    // MPEG-1 only supports a fixed set of frame rates
	VideoBitrate  string // e.g. "1000k"

	MaxStreams      int           // max concurrent FFmpeg processes (unique URLs)
	MaxClients      int           // max concurrent WebSocket viewers
	IdleTimeout     time.Duration // keep FFmpeg alive this long after the last viewer leaves
	ConnectTimeout  time.Duration // RTSP socket I/O timeout passed to FFmpeg
	StallTimeout    time.Duration // restart FFmpeg if it produces no output for this long
	MaxRetries      int           // consecutive failed attempts before giving up on a stream
	ShutdownTimeout time.Duration

	Presets []Preset
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	c := &Config{
		Addr:            ":" + env("PORT", "8080"),
		AllowedOrigins:  splitList(env("ALLOWED_ORIGINS", "*")),
		StaticDir:       env("STATIC_DIR", ""),
		FFmpegPath:      env("FFMPEG_PATH", "ffmpeg"),
		RTSPTransport:   strings.ToLower(env("RTSP_TRANSPORT", "tcp")),
		OutputWidth:     envInt("OUTPUT_MAX_WIDTH", 960),
		OutputFPS:       envInt("OUTPUT_FPS", 25),
		VideoBitrate:    env("VIDEO_BITRATE", "1000k"),
		MaxStreams:      envInt("MAX_STREAMS", 16),
		MaxClients:      envInt("MAX_CLIENTS", 200),
		IdleTimeout:     envDuration("IDLE_TIMEOUT", 5*time.Second),
		ConnectTimeout:  envDuration("CONNECT_TIMEOUT", 10*time.Second),
		StallTimeout:    envDuration("STALL_TIMEOUT", 15*time.Second),
		MaxRetries:      envInt("MAX_RETRIES", 5),
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
	}

	presets, err := parsePresets(os.Getenv("PRESET_STREAMS"))
	if err != nil {
		return nil, err
	}
	c.Presets = presets

	switch c.RTSPTransport {
	case "tcp", "udp", "auto":
	default:
		return nil, fmt.Errorf("RTSP_TRANSPORT must be tcp, udp or auto, got %q", c.RTSPTransport)
	}
	switch c.OutputFPS {
	case 24, 25, 30, 50, 60:
	default:
		return nil, fmt.Errorf("OUTPUT_FPS must be one of 24, 25, 30, 50, 60 (MPEG-1 limitation), got %d", c.OutputFPS)
	}
	return c, nil
}

// parsePresets parses "Name=rtsp://a,Other=rtsp://b".
func parsePresets(raw string) ([]Preset, error) {
	out := []Preset{}
	for _, item := range splitList(raw) {
		name, url, ok := strings.Cut(item, "=")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("invalid PRESET_STREAMS entry %q, expected Name=rtsp://...", item)
		}
		out = append(out, Preset{Name: strings.TrimSpace(name), URL: strings.TrimSpace(url)})
	}
	return out, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(env(key, "")); err == nil && v > 0 {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(env(key, "")); err == nil && v > 0 {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
