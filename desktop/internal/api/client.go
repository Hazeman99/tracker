// Package api is the desktop app's HTTP client for the Tracker API. It runs
// on the Go side only; the webview never sees the session token.
package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Error kinds understood by the frontend.
const (
	KindUnauthorized = "unauthorized" // token missing, expired or revoked
	KindOffline      = "offline"      // server unreachable
	KindInvalid      = "invalid"      // 4xx with a message from the server
	KindRateLimited  = "rate_limited"
	KindServer       = "server" // 5xx or unexpected response
)

// Error is returned by every client call.
type Error struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Status  int    `json:"status,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// IsKind reports whether err is an *Error of the given kind.
func IsKind(err error, kind string) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == kind
}

const maxResponse = 32 << 20

// Client talks to one server origin.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for the origin base (already validated). It uses the
// system certificate store, TLS 1.2+, and never follows redirects (so the
// Authorization header is never replayed elsewhere). insecureTLS skips
// certificate verification — only ever set from the user's explicit,
// off-by-default choice on the Connect screen (self-signed servers).
func New(base string, insecureTLS bool) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureTLS, // #nosec G402 -- explicit user opt-in per server, default false
	}
	tr.ResponseHeaderTimeout = 20 * time.Second
	return &Client{
		base: base,
		http: &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Do sends a JSON request. in may be nil; out may be nil.
func (c *Client) Do(ctx context.Context, method, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return &Error{Kind: KindServer, Message: "could not encode request"}
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return &Error{Kind: KindServer, Message: "invalid request"}
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return &Error{Kind: KindOffline, Message: "request cancelled"}
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			var certErr *tls.CertificateVerificationError
			if errors.As(err, &certErr) {
				return &Error{Kind: KindOffline, Message: "the server's certificate is not trusted (for a self-signed certificate, enable \"Disable SSL certificate verification\")"}
			}
		}
		return &Error{Kind: KindOffline, Message: "cannot reach the server"}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return &Error{Kind: KindOffline, Message: "connection interrupted"}
	}
	if len(data) > maxResponse {
		return &Error{Kind: KindServer, Message: "response too large"}
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return &Error{Kind: KindUnauthorized, Message: serverMessage(data, "signed out"), Status: resp.StatusCode}
	case resp.StatusCode == http.StatusTooManyRequests:
		msg := "too many requests, try again later"
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			msg = fmt.Sprintf("too many attempts, try again in %s seconds", ra)
		}
		return &Error{Kind: KindRateLimited, Message: msg, Status: resp.StatusCode}
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return &Error{Kind: KindServer, Message: "the server answered with a redirect; check the server address", Status: resp.StatusCode}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return &Error{Kind: KindInvalid, Message: serverMessage(data, "request rejected"), Status: resp.StatusCode}
	case resp.StatusCode >= 500:
		return &Error{Kind: KindServer, Message: "server error", Status: resp.StatusCode}
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.Unmarshal(data, out); err != nil {
			return &Error{Kind: KindServer, Message: "unexpected response from server"}
		}
	}
	return nil
}

func serverMessage(data []byte, fallback string) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" && len(e.Error) < 300 {
		return e.Error
	}
	return fallback
}

// NewWithHTTPClient returns a client using hc (tests use it to trust an
// httptest server certificate). Redirects are still never followed.
func NewWithHTTPClient(base string, hc *http.Client) *Client {
	cp := *hc
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: base, http: &cp}
}
