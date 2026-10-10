package stream

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// State is the lifecycle state of a stream as reported to viewers.
type State string

const (
	StateConnecting   State = "connecting"
	StateLive         State = "live"
	StateReconnecting State = "reconnecting"
	StateError        State = "error"
)

// Status is sent to viewers as a JSON text message whenever it changes.
type Status struct {
	Type    string `json:"type"` // always "status"
	State   State  `json:"state"`
	Message string `json:"message,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
	Fatal   bool   `json:"fatal,omitempty"`
	Viewers int    `json:"viewers"`
}

const (
	subscriberDataBuffer = 256 // chunks; ~10s of video at typical bitrates
	maxConsecutiveDrops  = 64  // disconnect viewers that are this far behind
)

// Subscriber is one viewer of a Hub. Video chunks arrive on Data and status
// updates on Status. Done is closed when the hub ends the subscription; the
// data channels are never closed, so senders can never panic.
type Subscriber struct {
	Data   chan []byte
	Status chan Status
	Done   chan struct{} // closed when the hub drops this subscriber
	Reason string        // why the hub ended the subscription (set before Done closes)

	drops int
}

// Hub owns a single FFmpeg process for one source URL and fans its output out
// to any number of subscribers. The process starts with the first viewer and
// is stopped IdleTimeout after the last one leaves, so N viewers of the same
// camera cost one RTSP connection and one transcode.
type Hub struct {
	url     string
	opts    FFmpegOptions
	retries int
	idle    time.Duration
	log     *slog.Logger
	onClose func(*Hub)

	mu        sync.Mutex
	subs      map[*Subscriber]struct{}
	status    Status
	cancel    context.CancelFunc
	idleTimer *time.Timer
	closed    bool
	done      chan struct{}

	startedAt time.Time
	bytesIn   atomic.Int64
}

func newHub(url string, opts FFmpegOptions, retries int, idle time.Duration, log *slog.Logger, onClose func(*Hub)) *Hub {
	return &Hub{
		url:     url,
		opts:    opts,
		retries: retries,
		idle:    idle,
		log:     log.With("stream", RedactURL(url)),
		onClose: onClose,
		subs:    make(map[*Subscriber]struct{}),
		status:  Status{Type: "status", State: StateConnecting, Message: "connecting to camera"},
		done:    make(chan struct{}),
	}
}

// subscribe registers a new viewer and starts FFmpeg if needed. It returns
// false if the hub is already shutting down (the caller should create a new
// hub).
func (h *Hub) subscribe() (*Subscriber, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, false
	}
	s := &Subscriber{
		Data:   make(chan []byte, subscriberDataBuffer),
		Status: make(chan Status, 16),
		Done:   make(chan struct{}),
	}
	h.subs[s] = struct{}{}
	if h.idleTimer != nil {
		h.idleTimer.Stop()
		h.idleTimer = nil
	}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		h.startedAt = time.Now()
		go h.run(ctx)
	}
	h.broadcastStatusLocked() // tell everyone (incl. the new viewer) the viewer count
	return s, true
}

// unsubscribe removes a viewer. When nobody is left the hub waits IdleTimeout
// (so a quick pause/resume or page reload reuses the running process) and
// then shuts down.
func (h *Hub) unsubscribe(s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; !ok {
		return
	}
	h.removeLocked(s, "")
	if len(h.subs) == 0 && !h.closed && h.idleTimer == nil {
		h.idleTimer = time.AfterFunc(h.idle, func() {
			h.mu.Lock()
			empty := len(h.subs) == 0
			h.mu.Unlock()
			if empty {
				h.shutdown("idle")
			}
		})
	} else {
		h.broadcastStatusLocked()
	}
}

func (h *Hub) removeLocked(s *Subscriber, reason string) {
	delete(h.subs, s)
	s.Reason = reason
	close(s.Done)
}

// shutdown stops FFmpeg, disconnects all viewers and removes the hub from
// its manager. Safe to call more than once.
func (h *Hub) shutdown(reason string) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	if h.cancel != nil {
		h.cancel()
	}
	if h.idleTimer != nil {
		h.idleTimer.Stop()
	}
	for s := range h.subs {
		h.removeLocked(s, reason)
	}
	h.mu.Unlock()

	h.log.Info("stream hub closed", "reason", reason)
	if h.onClose != nil {
		h.onClose(h)
	}
}

// run keeps FFmpeg alive with exponential backoff until the hub is shut down
// or a source that has never been live fails MaxRetries times in a row (or
// with an error that retrying cannot fix).
func (h *Hub) run(ctx context.Context) {
	defer close(h.done)
	failures := 0
	everLive := false
	for attempt := 1; ; attempt++ {
		h.log.Info("starting ffmpeg", "attempt", attempt)

		gotData := false
		err := runFFmpeg(ctx, h.opts, h.url, func(chunk []byte) {
			if !gotData {
				gotData = true
				h.setStatus(Status{State: StateLive})
			}
			h.bytesIn.Add(int64(len(chunk)))
			h.broadcast(chunk)
		})
		if ctx.Err() != nil {
			return
		}

		var se *SourceError
		if !errors.As(err, &se) {
			se = classify("", err)
		}
		h.log.Warn("ffmpeg stopped", "reason", se.Reason, "ffmpeg", redactLog(h.url, se.Log))

		if gotData {
			everLive = true
			failures = 0
		} else {
			failures++
		}
		// A source that never worked is given up on quickly, so the user sees
		// a clear error. One that was live before (a camera rebooting, an RTSP
		// server restarting) is retried for as long as someone is watching.
		if !everLive && (se.Fatal || failures >= h.retries) {
			h.setStatus(Status{State: StateError, Message: se.Reason, Fatal: true})
			// Give the status message a moment to reach viewers, then close.
			time.Sleep(200 * time.Millisecond)
			h.shutdown("source failed: " + se.Reason)
			return
		}

		delay := backoff(failures)
		h.setStatus(Status{
			State:   StateReconnecting,
			Message: se.Reason,
			Attempt: failures,
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func backoff(failures int) time.Duration {
	d := time.Second << max(failures-1, 0)
	return min(d, 15*time.Second)
}

func (h *Hub) setStatus(s Status) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s.Type = "status"
	h.status = s
	h.broadcastStatusLocked()
}

func (h *Hub) broadcastStatusLocked() {
	st := h.status
	st.Viewers = len(h.subs)
	for s := range h.subs {
		select {
		case s.Status <- st:
		default: // status channel full: the viewer is hopelessly behind
		}
	}
}

// broadcast fans a chunk out without ever blocking on a slow viewer. Viewers
// that keep falling behind are disconnected so they can reconnect fresh
// rather than watch a corrupted, ever-delayed picture.
func (h *Hub) broadcast(chunk []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		select {
		case s.Data <- chunk:
			s.drops = 0
		default:
			s.drops++
			if s.drops >= maxConsecutiveDrops {
				h.log.Warn("disconnecting slow viewer")
				h.removeLocked(s, "client too slow")
			}
		}
	}
}

// Info is a snapshot of a hub for the stats API.
type Info struct {
	URL       string  `json:"url"`
	State     State   `json:"state"`
	Viewers   int     `json:"viewers"`
	UptimeSec float64 `json:"uptimeSec"`
	BytesIn   int64   `json:"bytesIn"`
}

func (h *Hub) info() Info {
	h.mu.Lock()
	defer h.mu.Unlock()
	return Info{
		URL:       RedactURL(h.url),
		State:     h.status.State,
		Viewers:   len(h.subs),
		UptimeSec: time.Since(h.startedAt).Round(time.Second).Seconds(),
		BytesIn:   h.bytesIn.Load(),
	}
}
