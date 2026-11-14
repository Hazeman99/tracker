package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tracker/api/internal/auth"
	"tracker/api/internal/db"
	"tracker/api/internal/store"
)

// Cheap Argon2 parameters keep the test suite fast; production parameters
// are asserted separately in the auth package.
var testParams = auth.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type env struct {
	t      *testing.T
	srv    *Server
	st     *store.Store
	clock  *fakeClock
	hasher *auth.Hasher
	logs   *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuffer) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type envOpt func(*Options)

func newEnv(t *testing.T, opts ...envOpt) *env {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	clock := &fakeClock{t: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)}
	st := store.New(d, clock.Now)
	h, err := auth.NewHasher(testParams, 4)
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	o := Options{
		Store: st, Hasher: h, Now: clock.Now,
		Logger:        slog.New(slog.NewJSONHandler(logs, nil)),
		RatePerMinute: 1_000_000, RateBurst: 1_000_000,
		TrustedProxy: netip.MustParseAddr("10.0.0.2"),
	}
	for _, f := range opts {
		f(&o)
	}
	return &env{t: t, srv: New(o), st: st, clock: clock, hasher: h, logs: logs}
}

type req struct {
	method, path, token string
	body                any // string = raw, otherwise JSON-encoded
	ip                  string
	contentType         string
	header              map[string]string
}

func (e *env) do(r req) *httptest.ResponseRecorder {
	e.t.Helper()
	var body io.Reader
	switch b := r.body.(type) {
	case nil:
	case string:
		body = strings.NewReader(b)
	case []byte:
		body = bytes.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		body = bytes.NewReader(buf)
	}
	hr := httptest.NewRequest(r.method, r.path, body)
	if r.ip == "" {
		r.ip = "198.51.100.7"
	}
	hr.RemoteAddr = r.ip + ":40000"
	if body != nil {
		ct := r.contentType
		if ct == "" {
			ct = "application/json"
		}
		if ct != "-" {
			hr.Header.Set("Content-Type", ct)
		}
	}
	if r.token != "" {
		hr.Header.Set("Authorization", "Bearer "+r.token)
	}
	for k, v := range r.header {
		hr.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, hr)
	return rec
}

func (e *env) mustStatus(rec *httptest.ResponseRecorder, want int) {
	e.t.Helper()
	if rec.Code != want {
		e.t.Fatalf("status %d, want %d; body %s", rec.Code, want, rec.Body.String())
	}
}

func (e *env) createUser(email, pw string) store.User {
	e.t.Helper()
	hash, err := e.hasher.Hash(pw)
	if err != nil {
		e.t.Fatal(err)
	}
	u, err := e.st.CreateUser(context.Background(), email, hash)
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *env) login(email, pw, ip string) string {
	e.t.Helper()
	rec := e.do(req{method: "POST", path: "/v1/auth/login", ip: ip,
		body: map[string]string{"email": email, "password": pw, "device_name": "test"}})
	e.mustStatus(rec, http.StatusOK)
	var out struct {
		Token string `json:"token"`
	}
	decodeBody(e.t, rec, &out)
	if out.Token == "" {
		e.t.Fatal("no token")
	}
	return out.Token
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error string `json:"error"`
	}
	decodeBody(t, rec, &out)
	return out.Error
}

const (
	pwA = "correct horse battery staple"
	pwB = "another long passphrase here"
)
