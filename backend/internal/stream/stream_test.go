package stream

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"testing/iotest"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "rtsp://localhost:8554/cam1", want: "rtsp://localhost:8554/cam1"},
		{in: "  RTSP://Cam.Local/live  ", want: "rtsp://cam.local/live"},
		{in: "rtsps://user:pass@host/s#frag", want: "rtsps://user:pass@host/s"},
		{in: "", wantErr: true},
		{in: "http://example.com/video", wantErr: true},
		{in: "rtsp:///nohost", wantErr: true},
		{in: "rtsp://host/with space", wantErr: true},
		{in: "-i /etc/passwd", wantErr: true},
	}
	for _, c := range cases {
		got, err := NormalizeURL(c.in)
		if c.wantErr {
			if !errors.Is(err, ErrInvalidURL) {
				t.Errorf("NormalizeURL(%q): expected ErrInvalidURL, got %v", c.in, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestRedactURL(t *testing.T) {
	got := RedactURL("rtsp://admin:secret@10.0.0.5/stream")
	if bytes.Contains([]byte(got), []byte("secret")) {
		t.Fatalf("password leaked: %s", got)
	}
}

func TestPumpAlignedEmitsWholePackets(t *testing.T) {
	data := bytes.Repeat([]byte{0x47}, tsPacketSize*10+50) // 50 trailing bytes
	var total int
	// OneByteReader forces many tiny reads, the worst case for alignment.
	err := pumpAligned(iotest.OneByteReader(bytes.NewReader(data)), func(chunk []byte) {
		if len(chunk)%tsPacketSize != 0 {
			t.Fatalf("chunk of %d bytes is not packet aligned", len(chunk))
		}
		total += len(chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != tsPacketSize*10 {
		t.Fatalf("emitted %d bytes, want %d", total, tsPacketSize*10)
	}
}

func TestPumpAlignedPropagatesErrors(t *testing.T) {
	boom := errors.New("boom")
	err := pumpAligned(io.MultiReader(bytes.NewReader(make([]byte, 200)), iotest.ErrReader(boom)), func([]byte) {})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]struct {
		log   string
		fatal bool
	}{
		"auth":    {"method DESCRIBE failed: 401 Unauthorized", true},
		"missing": {"method DESCRIBE failed: 404 Not Found", true},
		"refused": {"rtsp://x: Connection refused", false},
		"timeout": {"rtsp://x: Connection timed out", false},
	}
	for name, c := range cases {
		e := classify(c.log, nil)
		if e.Fatal != c.fatal || e.Reason == "" {
			t.Errorf("%s: got fatal=%v reason=%q", name, e.Fatal, e.Reason)
		}
	}
}

func TestBackoffIsCapped(t *testing.T) {
	if backoff(1) != time.Second || backoff(2) != 2*time.Second {
		t.Fatal("unexpected early backoff values")
	}
	if backoff(20) != 15*time.Second {
		t.Fatalf("backoff not capped: %v", backoff(20))
	}
}

func TestManagerSharesOneHubPerURL(t *testing.T) {
	m := NewManager(ManagerOptions{
		FFmpeg: FFmpegOptions{
			Path:           "/nonexistent/ffmpeg",
			RTSPTransport:  "tcp",
			MaxWidth:       640,
			FPS:            25,
			Bitrate:        "500k",
			ConnectTimeout: time.Second,
			StallTimeout:   time.Second,
		},
		MaxStreams:  1,
		MaxRetries:  1,
		IdleTimeout: 50 * time.Millisecond,
	})

	_, unsub1, err := m.Subscribe("rtsp://example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	defer unsub1()
	_, unsub2, err := m.Subscribe("RTSP://EXAMPLE.com/a") // same stream, different spelling
	if err != nil {
		t.Fatal(err)
	}
	defer unsub2()
	if _, _, err := m.Subscribe("rtsp://example.com/b"); !errors.Is(err, ErrTooManyStreams) {
		t.Fatalf("expected stream limit error, got %v", err)
	}
}

func TestHubReportsFatalErrorAndCloses(t *testing.T) {
	m := NewManager(ManagerOptions{
		FFmpeg: FFmpegOptions{
			Path: "/nonexistent/ffmpeg", RTSPTransport: "tcp", MaxWidth: 640, FPS: 25,
			Bitrate: "500k", ConnectTimeout: time.Second, StallTimeout: time.Second,
		},
		MaxStreams:  4,
		MaxRetries:  3,
		IdleTimeout: time.Second,
	})
	sub, unsub, err := m.Subscribe("rtsp://example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	timeout := time.After(5 * time.Second)
	var last Status
	for {
		select {
		case st := <-sub.Status:
			last = st
		case <-sub.Done:
			// Drain anything queued before Done.
			for len(sub.Status) > 0 {
				last = <-sub.Status
			}
			if last.State != StateError || !last.Fatal {
				t.Fatalf("expected fatal error status, got %+v", last)
			}
			deadline := time.Now().Add(time.Second)
			for len(m.Stats()) != 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if len(m.Stats()) != 0 {
				t.Fatal("failed hub was not removed from the manager")
			}
			return
		case <-timeout:
			t.Fatalf("hub did not give up, last status %+v", last)
		}
	}
}
