package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/pravinkumar172929/rtsp-stream-viewer/backend/internal/stream"
)

const (
	writeTimeout = 10 * time.Second
	pingInterval = 20 * time.Second

	// Application close codes (4000-4999 are reserved for applications).
	// The frontend uses them to decide whether reconnecting makes sense.
	closeSourceFailed = websocket.StatusCode(4000) // stream failed permanently
	closeBadRequest   = websocket.StatusCode(4001) // invalid URL
	closeServerBusy   = websocket.StatusCode(4002) // stream/client limit reached
	closeTooSlow      = websocket.StatusCode(4003) // client could not keep up
)

// handleStreamWS upgrades the request and relays one stream to the client.
//
// Protocol (server -> client only):
//   - text frames: JSON status, e.g. {"type":"status","state":"live","viewers":2}
//   - binary frames: MPEG-TS packets (MPEG-1 video) for JSMpeg
//
// Errors are reported in-band after the upgrade, because browsers do not
// expose HTTP error bodies of failed WebSocket handshakes to JavaScript.
func (s *Server) handleStreamWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, s.acceptOptions())
	if err != nil {
		s.log.Warn("websocket accept failed", "err", err)
		return
	}
	defer conn.CloseNow()

	if n := s.clients.Add(1); n > int64(s.cfg.MaxClients) {
		s.clients.Add(-1)
		s.closeWithError(conn, closeServerBusy, "server has too many viewers, try again later")
		return
	}
	defer s.clients.Add(-1)

	sub, unsubscribe, err := s.streams.Subscribe(r.URL.Query().Get("url"))
	switch {
	case errors.Is(err, stream.ErrInvalidURL):
		s.closeWithError(conn, closeBadRequest, strings.ReplaceAll(err.Error(), "\n", ": "))
		return
	case errors.Is(err, stream.ErrTooManyStreams):
		s.closeWithError(conn, closeServerBusy, err.Error())
		return
	case err != nil:
		s.closeWithError(conn, websocket.StatusInternalError, "internal error")
		return
	}
	defer unsubscribe()

	// We never expect messages from the client, but something has to read so
	// that ping/pong and close frames are processed. CloseRead does that and
	// cancels ctx when the browser goes away.
	ctx := conn.CloseRead(r.Context())

	s.pump(ctx, conn, sub)
}

// pump writes status updates and video chunks until the client leaves or the
// hub ends the subscription.
func (s *Server) pump(ctx context.Context, conn *websocket.Conn, sub *stream.Subscriber) {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	var last stream.Status
	for {
		select {
		case <-ctx.Done():
			return

		case st := <-sub.Status:
			last = st
			if err := writeStatus(ctx, conn, st); err != nil {
				return
			}

		case chunk := <-sub.Data:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(wctx, websocket.MessageBinary, chunk)
			cancel()
			if err != nil {
				return
			}

		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}

		case <-sub.Done:
			// Flush any final status (e.g. the fatal error) before closing.
		drain:
			for {
				select {
				case st := <-sub.Status:
					last = st
					_ = writeStatus(ctx, conn, st)
				default:
					break drain
				}
			}
			switch {
			case last.State == stream.StateError:
				conn.Close(closeSourceFailed, truncate(last.Message, 120))
			case sub.Reason == "client too slow":
				conn.Close(closeTooSlow, sub.Reason)
			default:
				conn.Close(websocket.StatusGoingAway, truncate(sub.Reason, 120))
			}
			return
		}
	}
}

func writeStatus(ctx context.Context, conn *websocket.Conn, st stream.Status) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func (s *Server) closeWithError(conn *websocket.Conn, code websocket.StatusCode, msg string) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	_ = writeStatus(ctx, conn, stream.Status{Type: "status", State: stream.StateError, Message: msg, Fatal: true})
	conn.Close(code, truncate(msg, 120))
}

func (s *Server) acceptOptions() *websocket.AcceptOptions {
	opts := &websocket.AcceptOptions{
		// Video is already compressed; permessage-deflate would only burn CPU.
		CompressionMode: websocket.CompressionDisabled,
	}
	for _, o := range s.cfg.AllowedOrigins {
		if o == "*" {
			opts.InsecureSkipVerify = true // any origin allowed by configuration
			return opts
		}
		// coder/websocket matches against the origin host, e.g. "app.vercel.app".
		host := strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://")
		opts.OriginPatterns = append(opts.OriginPatterns, strings.TrimSuffix(host, "/"))
	}
	return opts
}

// truncate keeps close reasons under the 123-byte limit of WebSocket close frames.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
