// Package server is the HTTP layer of the API.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"tracker/api/internal/auth"
	"tracker/api/internal/ratelimit"
	"tracker/api/internal/store"
)

// Body size caps.
const (
	smallBody int64 = 64 << 10 // 64 KB default
	largeBody int64 = 1 << 20  // 1 MB for description and log bodies
)

// Login lockout rules.
var (
	// LoginIPRule: at most 5 attempts per minute per IP.
	LoginIPRule = ratelimit.Rule{Max: 5, Window: time.Minute, Base: time.Minute, Cap: time.Hour}
	// LoginAccountRule: at most 10 attempts per 15 minutes per account.
	LoginAccountRule = ratelimit.Rule{Max: 10, Window: 15 * time.Minute, Base: 15 * time.Minute, Cap: 24 * time.Hour}
)

// Options configures a Server.
type Options struct {
	Store        *store.Store
	Hasher       *auth.Hasher
	Logger       *slog.Logger
	Now          func() time.Time
	TrustedProxy netip.Addr
	// Global limits for all endpoints, per IP and per session.
	RatePerMinute int
	RateBurst     int
}

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	store        *store.Store
	hasher       *auth.Hasher
	log          *slog.Logger
	now          func() time.Time
	trustedProxy netip.Addr
	ipLimit      *ratelimit.Limiter
	sessionLimit *ratelimit.Limiter
	loginIP      *ratelimit.Guard
	loginAccount *ratelimit.Guard
	handler      http.Handler
}

// New builds a Server.
func New(o Options) *Server {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RatePerMinute == 0 {
		o.RatePerMinute = 120
	}
	if o.RateBurst == 0 {
		o.RateBurst = 30
	}
	s := &Server{
		store:        o.Store,
		hasher:       o.Hasher,
		log:          o.Logger,
		now:          o.Now,
		trustedProxy: o.TrustedProxy,
		ipLimit:      ratelimit.NewLimiter(o.RatePerMinute, o.RateBurst, o.Now),
		sessionLimit: ratelimit.NewLimiter(o.RatePerMinute, o.RateBurst, o.Now),
		loginIP:      ratelimit.NewGuard(LoginIPRule, o.Now),
		loginAccount: ratelimit.NewGuard(LoginAccountRule, o.Now),
	}
	s.handler = s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// RunJanitor periodically drops idle rate-limit state until ctx is done.
func (s *Server) RunJanitor(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.ipLimit.Sweep(10 * time.Minute)
			s.sessionLimit.Sweep(10 * time.Minute)
			s.loginIP.Sweep()
			s.loginAccount.Sweep()
		}
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	public := func(pattern string, limit int64, h http.HandlerFunc) {
		mux.Handle(pattern, withBodyLimit(limit, h))
	}
	authed := func(pattern string, limit int64, h http.HandlerFunc) {
		mux.Handle(pattern, withBodyLimit(limit, s.requireSession(h)))
	}

	public("GET /v1/healthz", smallBody, s.healthz)
	public("POST /v1/auth/login", smallBody, s.login)

	authed("POST /v1/auth/logout", smallBody, s.logout)
	authed("GET /v1/auth/sessions", smallBody, s.listSessions)
	authed("DELETE /v1/auth/sessions/{id}", smallBody, s.revokeSession)
	authed("POST /v1/auth/password", smallBody, s.changePassword)

	authed("GET /v1/projects", smallBody, s.listProjects)
	authed("POST /v1/projects", largeBody, s.createProject)
	authed("PATCH /v1/projects/order", smallBody, s.reorderProjects)
	authed("GET /v1/projects/{id}", smallBody, s.getProject)
	authed("PATCH /v1/projects/{id}", largeBody, s.updateProject)
	authed("DELETE /v1/projects/{id}", smallBody, s.deleteProject)

	authed("GET /v1/tags", smallBody, s.listTags)
	authed("PATCH /v1/tags/{id}", smallBody, s.renameTag)

	authed("POST /v1/projects/{id}/log", largeBody, s.createLogEntry)
	authed("PATCH /v1/log/{id}", largeBody, s.updateLogEntry)
	authed("DELETE /v1/log/{id}", smallBody, s.deleteLogEntry)

	authed("POST /v1/projects/{id}/tasks", smallBody, s.createTask)
	authed("PATCH /v1/tasks/{id}", smallBody, s.updateTask)
	authed("DELETE /v1/tasks/{id}", smallBody, s.deleteTask)

	mux.Handle("/", withBodyLimit(smallBody, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})))

	var h http.Handler = mux
	h = s.limitByIP(h)
	h = s.recoverPanics(h)
	h = securityHeaders(h)
	h = s.logRequests(h)
	return h
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
