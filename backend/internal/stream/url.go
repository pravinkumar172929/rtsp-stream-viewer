package stream

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

const maxURLLength = 2048

// ErrInvalidURL is returned for URLs that are not acceptable RTSP sources.
var ErrInvalidURL = errors.New("invalid RTSP URL")

// NormalizeURL validates an RTSP URL and returns a canonical form that is
// used as the key for sharing one FFmpeg process between viewers.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.Join(ErrInvalidURL, errors.New("URL is empty"))
	}
	if len(raw) > maxURLLength {
		return "", errors.Join(ErrInvalidURL, errors.New("URL is too long"))
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", errors.Join(ErrInvalidURL, errors.New("URL contains whitespace or control characters"))
		}
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.Join(ErrInvalidURL, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" {
		return "", errors.Join(ErrInvalidURL, errors.New("URL must start with rtsp:// or rtsps://"))
	}
	if u.Hostname() == "" {
		return "", errors.Join(ErrInvalidURL, errors.New("URL has no host"))
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	return u.String(), nil
}

// RedactURL hides the password of a URL so it can be logged or returned by
// the stats API without leaking camera credentials.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	return u.Redacted()
}

// redactLog removes the URL password from FFmpeg output, which echoes the
// input URL in its error messages.
func redactLog(rawURL, log string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return log
	}
	if pw, ok := u.User.Password(); ok && pw != "" {
		return strings.ReplaceAll(log, pw, "xxxxx")
	}
	return log
}
