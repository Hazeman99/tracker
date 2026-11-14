// Package ratelimit provides in-memory, per-key request limiting and the
// login lockout guard. State lives in memory: the API is a single process.
package ratelimit

import (
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxKeys bounds memory use. When exceeded, idle keys are evicted first; if
// that is not enough, new keys share a single overflow bucket.
const maxKeys = 100_000

// Limiter is a set of token buckets keyed by string (an IP or a session id).
type Limiter struct {
	mu       sync.Mutex
	rate     rate.Limit
	burst    int
	buckets  map[string]*bucket
	overflow *rate.Limiter
	now      func() time.Time
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

// NewLimiter allows perMinute requests per minute per key with the given burst.
func NewLimiter(perMinute, burst int, now func() time.Time) *Limiter {
	r := rate.Limit(float64(perMinute) / 60)
	return &Limiter{
		rate:     r,
		burst:    burst,
		buckets:  make(map[string]*bucket),
		overflow: rate.NewLimiter(r, burst),
		now:      now,
	}
}

// Allow consumes one token for key. If none is available it returns false and
// how long the caller should wait before retrying.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	var lim *rate.Limiter
	switch {
	case ok:
		b.seen = now
		lim = b.lim
	case len(l.buckets) >= maxKeys:
		l.sweepLocked(now, time.Minute)
		if len(l.buckets) >= maxKeys {
			lim = l.overflow
			break
		}
		fallthrough
	default:
		b = &bucket{lim: rate.NewLimiter(l.rate, l.burst), seen: now}
		l.buckets[key] = b
		lim = b.lim
	}
	res := lim.ReserveN(now, 1)
	if !res.OK() {
		return false, time.Minute
	}
	if d := res.DelayFrom(now); d > 0 {
		res.CancelAt(now)
		return false, d
	}
	return true, 0
}

// Sweep removes buckets idle for longer than idle. Call it periodically.
func (l *Limiter) Sweep(idle time.Duration) {
	now := l.now()
	l.mu.Lock()
	l.sweepLocked(now, idle)
	l.mu.Unlock()
}

func (l *Limiter) sweepLocked(now time.Time, idle time.Duration) {
	for k, b := range l.buckets {
		if now.Sub(b.seen) > idle {
			delete(l.buckets, k)
		}
	}
}

// Rule describes one login lockout rule: at most Max attempts per Window,
// then a lockout of Base, doubling on each consecutive lockout up to Cap.
type Rule struct {
	Max    int
	Window time.Duration
	Base   time.Duration
	Cap    time.Duration
}

// Guard tracks login attempts for one rule (per IP or per account).
type Guard struct {
	mu    sync.Mutex
	rule  Rule
	state map[string]*guardState
	now   func() time.Time
}

type guardState struct {
	attempts    []time.Time
	lockedUntil time.Time
	level       int // consecutive lockouts, drives the exponential backoff
	lastLockout time.Time
}

// NewGuard returns a Guard enforcing rule.
func NewGuard(rule Rule, now func() time.Time) *Guard {
	return &Guard{rule: rule, state: make(map[string]*guardState), now: now}
}

// Check reports whether key is currently locked out and for how long.
func (g *Guard) Check(key string) (locked bool, retryAfter time.Duration) {
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.state[key]
	if !ok || !now.Before(s.lockedUntil) {
		return false, 0
	}
	return true, s.lockedUntil.Sub(now)
}

// Record registers an attempt for key. It returns true and the lockout
// duration if this attempt pushed the key over the limit.
func (g *Guard) Record(key string) (lockedNow bool, lockout time.Duration) {
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.state[key]
	if !ok {
		if len(g.state) >= maxKeys {
			g.sweepLocked(now)
		}
		s = &guardState{}
		g.state[key] = s
	}
	// The backoff level decays after a full day without a lockout.
	if s.level > 0 && now.Sub(s.lastLockout) > 24*time.Hour {
		s.level = 0
	}
	cutoff := now.Add(-g.rule.Window)
	kept := s.attempts[:0]
	for _, t := range s.attempts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.attempts = append(kept, now)
	if len(s.attempts) <= g.rule.Max {
		return false, 0
	}
	d := time.Duration(float64(g.rule.Base) * math.Pow(2, float64(s.level)))
	if d > g.rule.Cap || d <= 0 {
		d = g.rule.Cap
	}
	s.level++
	s.lastLockout = now
	s.lockedUntil = now.Add(d)
	s.attempts = s.attempts[:0]
	return true, d
}

// Sweep drops state that can no longer influence a decision.
func (g *Guard) Sweep() {
	now := g.now()
	g.mu.Lock()
	g.sweepLocked(now)
	g.mu.Unlock()
}

func (g *Guard) sweepLocked(now time.Time) {
	for k, s := range g.state {
		idle := len(s.attempts) == 0 || now.Sub(s.attempts[len(s.attempts)-1]) > g.rule.Window
		if idle && !now.Before(s.lockedUntil) && (s.level == 0 || now.Sub(s.lastLockout) > 24*time.Hour) {
			delete(g.state, k)
		}
	}
}
