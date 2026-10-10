package stream

import (
	"strings"
)

// SourceError describes why an RTSP source could not be read, in words that
// make sense to an end user, while keeping FFmpeg's raw output for logs.
type SourceError struct {
	Reason string // human readable, safe to show in the UI
	Log    string // raw FFmpeg stderr tail (may contain hostnames), for server logs
	Fatal  bool   // retrying is unlikely to help (bad credentials, wrong path...)
	cause  error
}

func (e *SourceError) Error() string { return e.Reason }
func (e *SourceError) Unwrap() error { return e.cause }

// classify maps FFmpeg's stderr output to a friendly error. The patterns
// cover the failure modes users hit most often with IP cameras.
func classify(log string, cause error) *SourceError {
	l := strings.ToLower(log)
	e := &SourceError{Log: log, cause: cause}
	switch {
	case strings.Contains(l, "401") || strings.Contains(l, "unauthorized"):
		e.Reason, e.Fatal = "authentication failed: check the username and password in the URL", true
	case strings.Contains(l, "404") || strings.Contains(l, "not found"):
		e.Reason, e.Fatal = "stream not found on the server: check the path in the URL", true
	case strings.Contains(l, "connection refused"):
		e.Reason = "connection refused: is the RTSP server running on that host and port?"
	case strings.Contains(l, "timed out") || strings.Contains(l, "timeout"):
		e.Reason = "connection timed out: the host is unreachable or not responding"
	case strings.Contains(l, "name or service not known") ||
		strings.Contains(l, "failed to resolve") ||
		strings.Contains(l, "temporary failure in name resolution"):
		e.Reason, e.Fatal = "could not resolve the host name", true
	case strings.Contains(l, "no route to host") || strings.Contains(l, "network is unreachable"):
		e.Reason = "network unreachable"
	case strings.Contains(l, "invalid data") || strings.Contains(l, "could not find codec"):
		e.Reason = "the server returned data that could not be decoded"
	case strings.Contains(l, "461") || strings.Contains(l, "unsupported transport"):
		e.Reason, e.Fatal = "the server does not support the requested RTSP transport", true
	default:
		e.Reason = "the stream stopped unexpectedly"
	}
	return e
}
