package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"tracker/api/internal/auth"
	"tracker/api/internal/store"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxClientIP
	ctxSession
)

func requestID(r *http.Request) string {
	v, _ := r.Context().Value(ctxRequestID).(string)
	return v
}

func clientIPFrom(r *http.Request) string {
	v, _ := r.Context().Value(ctxClientIP).(string)
	return v
}

func sessionFrom(r *http.Request) store.Session {
	v, _ := r.Context().Value(ctxSession).(store.Session)
	return v
}

// statusWriter records the response status for logging.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// logRequests assigns a request id, resolves the client IP and writes one
// structured log line per request. It never logs headers, bodies, tokens or
// query strings.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var b [8]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		ip := s.clientIP(r)
		ctx := context.WithValue(r.Context(), ctxRequestID, id)
		ctx = context.WithValue(ctx, ctxClientIP, ip)
		w.Header().Set("X-Request-Id", id)
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(ctx))
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		s.log.LogAttrs(r.Context(), slog.LevelInfo, "request",
			slog.String("request_id", id),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Int("bytes", sw.bytes),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("ip", ip),
		)
	})
}

// securityHeaders sets the hardening headers on every response. There are
// deliberately no CORS headers: the only client is the desktop app.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// recoverPanics turns a panic into a generic 500 and logs the stack trace
// server-side only.
func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
					panic(v)
				}
				s.log.Error("panic", "request_id", requestID(r), "panic", v, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the socket peer address, or the address Caddy reports in
// X-Forwarded-For when (and only when) the peer is the trusted proxy. Caddy
// appends the real client address, so the rightmost entry is used.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	peer = peer.Unmap()
	if s.trustedProxy.IsValid() && peer == s.trustedProxy {
		vals := r.Header.Values("X-Forwarded-For")
		if len(vals) > 0 {
			parts := strings.Split(vals[len(vals)-1], ",")
			if fwd, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				return fwd.Unmap().String()
			}
		}
	}
	return peer.String()
}

func retryAfter(w http.ResponseWriter, d time.Duration) {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, "too many requests")
}

// limitByIP applies the global per-IP rate limit to every request.
func (s *Server) limitByIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, d := s.ipLimit.Allow(clientIPFrom(r)); !ok {
			retryAfter(w, d)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withBodyLimit caps the request body.
func withBodyLimit(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > limit {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// requireSession authenticates the bearer token and applies the per-session
// rate limit.
func (s *Server) requireSession(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			unauthorized(w)
			return
		}
		sess, err := s.store.Authenticate(r.Context(), auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			unauthorized(w)
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		if ok, d := s.sessionLimit.Allow(sess.ID); !ok {
			retryAfter(w, d)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxSession, sess)))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	vals := r.Header.Values("Authorization")
	if len(vals) != 1 {
		return "", false
	}
	scheme, token, ok := strings.Cut(vals[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if !auth.WellFormedToken(token) {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer`)
	writeError(w, http.StatusUnauthorized, "unauthorized")
}
