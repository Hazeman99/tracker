// Package serverurl validates the server address entered on the Connect screen.
package serverurl

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// ErrInsecure is returned for any non-https address (http://localhost is
// accepted only in dev builds).
var ErrInsecure = errors.New("the server address must start with https://")

// ErrInvalid is returned for addresses that are not a bare origin.
var ErrInvalid = errors.New("enter a server address such as https://tracker.example.com")

// Normalize validates raw and returns the canonical origin, e.g.
// "https://tracker.example.com" or "https://tracker.example.com:8443".
// A missing scheme is treated as https.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalid
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", ErrInvalid
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") {
		return "", ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		return "", ErrInvalid
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !allowInsecureLocalhost || !isLoopback(host) {
			return "", ErrInsecure
		}
	default:
		return "", ErrInsecure
	}
	origin := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
	return origin, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Host returns the host[:port] part of an origin produced by Normalize.
func Host(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return origin
	}
	return u.Host
}
