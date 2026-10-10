package stream

import (
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// ErrTooManyStreams is returned when the server is already running the
// maximum number of FFmpeg processes.
var ErrTooManyStreams = errors.New("server is at its stream limit, try again later")

// ManagerOptions configures a Manager.
type ManagerOptions struct {
	FFmpeg      FFmpegOptions
	MaxStreams  int
	MaxRetries  int
	IdleTimeout time.Duration
	Logger      *slog.Logger
}

// Manager maps source URLs to hubs so that every unique URL is pulled and
// transcoded exactly once, no matter how many browsers are watching it.
type Manager struct {
	opts ManagerOptions
	log  *slog.Logger

	mu   sync.Mutex
	hubs map[string]*Hub
}

// NewManager creates an empty manager.
func NewManager(opts ManagerOptions) *Manager {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Manager{opts: opts, log: opts.Logger, hubs: make(map[string]*Hub)}
}

// Subscribe attaches a viewer to the stream for rawURL, starting it if
// necessary. The returned function must be called to unsubscribe.
func (m *Manager) Subscribe(rawURL string) (*Subscriber, func(), error) {
	url, err := NormalizeURL(rawURL)
	if err != nil {
		return nil, nil, err
	}
	for {
		h, err := m.getOrCreate(url)
		if err != nil {
			return nil, nil, err
		}
		if s, ok := h.subscribe(); ok {
			return s, func() { h.unsubscribe(s) }, nil
		}
		// The hub closed between lookup and subscribe; wait for it to be
		// removed and try again with a fresh one.
		m.remove(h)
	}
}

func (m *Manager) getOrCreate(url string) (*Hub, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.hubs[url]; ok {
		return h, nil
	}
	if len(m.hubs) >= m.opts.MaxStreams {
		return nil, ErrTooManyStreams
	}
	h := newHub(url, m.opts.FFmpeg, m.opts.MaxRetries, m.opts.IdleTimeout, m.log, m.remove)
	m.hubs[url] = h
	return h, nil
}

func (m *Manager) remove(h *Hub) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hubs[h.url] == h {
		delete(m.hubs, h.url)
	}
}

// Stats returns a snapshot of all running streams.
func (m *Manager) Stats() []Info {
	m.mu.Lock()
	hubs := make([]*Hub, 0, len(m.hubs))
	for _, h := range m.hubs {
		hubs = append(hubs, h)
	}
	m.mu.Unlock()

	out := make([]Info, 0, len(hubs))
	for _, h := range hubs {
		out = append(out, h.info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out
}

// Shutdown stops every stream and waits (up to the deadline) for FFmpeg
// processes to exit.
func (m *Manager) Shutdown(deadline time.Time) {
	m.mu.Lock()
	hubs := make([]*Hub, 0, len(m.hubs))
	for _, h := range m.hubs {
		hubs = append(hubs, h)
	}
	m.mu.Unlock()

	for _, h := range hubs {
		h.shutdown("server shutting down")
	}
	for _, h := range hubs {
		h.mu.Lock()
		started := h.cancel != nil
		h.mu.Unlock()
		if !started {
			continue
		}
		select {
		case <-h.done:
		case <-time.After(time.Until(deadline)):
			return
		}
	}
}
