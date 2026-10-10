package stream

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// tsPacketSize is the size of an MPEG transport stream packet. Every chunk we
// forward to browsers is a whole number of packets, so a viewer that joins in
// the middle of a stream always starts on a packet boundary.
const tsPacketSize = 188

// FFmpegOptions controls how a source is transcoded.
type FFmpegOptions struct {
	Path           string
	RTSPTransport  string
	MaxWidth       int
	FPS            int
	Bitrate        string
	ConnectTimeout time.Duration
	StallTimeout   time.Duration
}

// buildArgs returns the FFmpeg command line for one RTSP source.
//
// The output is MPEG-1 video in an MPEG-TS container, which JSMpeg decodes in
// the browser (WASM + WebGL). MPEG-1 is old but has two big advantages here:
// it is cheap to decode on any device, and a fresh sequence header is emitted
// with every GOP, so a new viewer gets a picture within one GOP (~1s).
func buildArgs(o FFmpegOptions, src string) []string {
	args := []string{
		"-hide_banner", "-nostdin",
		"-loglevel", "error",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		// Probe the input for at most 1s / 1MB instead of the default 5s:
		// cuts time-to-first-frame from ~3s to ~1s for typical cameras.
		"-analyzeduration", "1000000",
		"-probesize", "1000000",
	}
	if o.RTSPTransport != "auto" {
		args = append(args, "-rtsp_transport", o.RTSPTransport)
	}
	// Socket I/O timeout in microseconds: makes unreachable hosts fail fast.
	args = append(args, "-timeout", strconv.FormatInt(o.ConnectTimeout.Microseconds(), 10))
	args = append(args,
		"-i", src,
		"-an", // video only: keeps bandwidth and CPU low for multi-stream grids
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", o.MaxWidth),
		"-r", strconv.Itoa(o.FPS),
		"-c:v", "mpeg1video",
		"-b:v", o.Bitrate,
		"-maxrate", o.Bitrate,
		"-bufsize", o.Bitrate,
		"-bf", "0", // no B-frames: lower latency
		"-g", strconv.Itoa(o.FPS), // one keyframe per second: fast join for new viewers
		"-f", "mpegts",
		"-flush_packets", "1",
		"pipe:1",
	)
	return args
}

// runFFmpeg starts FFmpeg for src and calls onData with packet-aligned chunks
// until the process exits, ctx is cancelled, or no data arrives for
// StallTimeout. It returns a descriptive error explaining why it stopped.
func runFFmpeg(ctx context.Context, o FFmpegOptions, src string, onData func([]byte)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, o.Path, buildArgs(o, src)...)
	// Ask FFmpeg to exit cleanly first; force-kill if it does not.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return &SourceError{Reason: "could not start ffmpeg on the server", Log: err.Error(), Fatal: true, cause: err}
	}

	logTail := newTailBuffer(8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			logTail.add(sc.Text())
		}
	}()

	// Watchdog: kill FFmpeg if it stops producing output.
	stalled := make(chan struct{})
	lastData := make(chan struct{}, 1)
	go func() {
		t := time.NewTimer(o.StallTimeout)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-lastData:
				if !t.Stop() {
					<-t.C
				}
				t.Reset(o.StallTimeout)
			case <-t.C:
				close(stalled)
				cancel()
				return
			}
		}
	}()

	readErr := pumpAligned(stdout, func(chunk []byte) {
		select {
		case lastData <- struct{}{}:
		default:
		}
		onData(chunk)
	})
	waitErr := cmd.Wait()
	wg.Wait()

	select {
	case <-stalled:
		return &SourceError{Reason: fmt.Sprintf("no video received for %s", o.StallTimeout), Log: logTail.String()}
	default:
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil || readErr != nil {
		return classify(logTail.String(), errors.Join(waitErr, readErr))
	}
	return &SourceError{Reason: "stream ended", Log: logTail.String()}
}

// pumpAligned reads r and emits chunks whose length is a multiple of
// tsPacketSize. Each emitted slice is freshly allocated so it can be shared
// with many subscribers without copying.
func pumpAligned(r io.Reader, emit func([]byte)) error {
	buf := make([]byte, 64*1024)
	pending := 0
	for {
		n, err := r.Read(buf[pending:])
		pending += n
		if whole := pending - pending%tsPacketSize; whole > 0 {
			chunk := make([]byte, whole)
			copy(chunk, buf[:whole])
			emit(chunk)
			pending = copy(buf, buf[whole:pending])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// tailBuffer keeps the last N lines of FFmpeg's stderr for error reporting.
type tailBuffer struct {
	mu    sync.Mutex
	max   int
	lines []string
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, "\n")
}
