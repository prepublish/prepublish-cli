package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Service URLs are the two addresses the CLI is willing to trust: the API it
// sends a bearer token to, and the app it opens in a browser. Both are
// user-configurable, so both are vetted in one place, before a command acts on
// them.

// ValidateServiceURL checks a base URL the CLI is about to call or open.
//
// The rule is https only, with plain http allowed for a loopback address. A
// request carrying an API key over http is readable by anything on the path, and
// "it is only a dev server" is exactly how a production key ends up on the wire;
// a local server is the one case where the traffic cannot leave the machine, and
// that is what the loopback exception covers.
func ValidateServiceURL(raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return errors.New("an empty URL is not allowed: use `prepublish config unset`")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		// url.Parse rejects ASCII control characters, so a URL that arrived with
		// an escape sequence in it fails here rather than at the terminal.
		return fmt.Errorf("%s is not a URL: %w", value, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s: the URL has no host", value)
	}

	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if loopbackHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("%s is plain http: anything sent over it — an API key, a sign-in code — can be read on the way. Use https, or a loopback address (localhost, 127.0.0.1, ::1) for a local server", value)
	default:
		return fmt.Errorf("%s: the URL must start with https:// (http:// is accepted only for a loopback address)", value)
	}
}

// loopbackHost reports whether host names this machine.
func loopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		// Covers 127.0.0.0/8 and ::1, which is what a local server binds.
		return ip.IsLoopback()
	}
	return false
}

// Origin is the scheme and host of raw: lowercased, with the scheme's default
// port removed, and the path, query and fragment dropped.
//
// This is the identity a stored API key is bound to. It is deliberately stricter
// than "same host": http://api.example and https://api.example are different
// origins because a key that is safe on one is not safe on the other.
//
// An unparseable value yields "", which compares equal to nothing.
func Origin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Host)
	switch {
	case scheme == "https" && strings.HasSuffix(host, ":443"):
		host = strings.TrimSuffix(host, ":443")
	case scheme == "http" && strings.HasSuffix(host, ":80"):
		host = strings.TrimSuffix(host, ":80")
	}
	return scheme + "://" + host
}

// SameOrigin reports whether two URLs are the same origin. It is the check that
// decides whether a stored key may be sent to the API in effect.
func SameOrigin(a, b string) bool {
	origin := Origin(a)
	return origin != "" && origin == Origin(b)
}
