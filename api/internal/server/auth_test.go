package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"tracker/api/internal/auth"
	"tracker/api/internal/store"
)

func kinds(t *testing.T, e *env) []string {
	t.Helper()
	evs, err := e.st.AuthEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ev := range evs {
		out = append(out, ev.Kind)
	}
	return out
}

func countKind(t *testing.T, e *env, kind string) int {
	n := 0
	for _, k := range kinds(t, e) {
		if k == kind {
			n++
		}
	}
	return n
}

func TestLoginSuccessAndFailure(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)

	tok := e.login("A@Example.com ", pwA, "")
	if !auth.WellFormedToken(tok) {
		t.Fatal("token malformed")
	}
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects", token: tok}), http.StatusOK)

	wrong := e.do(req{method: "POST", path: "/v1/auth/login", body: map[string]string{"email": "a@example.com", "password": "wrong password!!"}})
	e.mustStatus(wrong, http.StatusUnauthorized)
	unknown := e.do(req{method: "POST", path: "/v1/auth/login", body: map[string]string{"email": "nobody@example.com", "password": "wrong password!!"}})
	e.mustStatus(unknown, http.StatusUnauthorized)
	if wrong.Body.String() != unknown.Body.String() {
		t.Fatalf("login errors differ: %q vs %q", wrong.Body.String(), unknown.Body.String())
	}
	if errorMessage(t, wrong) != genericLoginError {
		t.Fatal("non-generic login error")
	}
	if got := countKind(t, e, store.EventLoginFailure); got != 2 {
		t.Fatalf("expected 2 failures recorded, got %d (%v)", got, kinds(t, e))
	}
	if countKind(t, e, store.EventLoginSuccess) != 1 {
		t.Fatal("success not recorded")
	}

	// Neither the password nor the token may appear in the logs.
	if strings.Contains(e.logs.String(), tok) || strings.Contains(e.logs.String(), pwA) {
		t.Fatal("secret leaked into logs")
	}
}

func TestLoginTokenNotStoredInPlaintext(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	tok := e.login("a@example.com", pwA, "")
	if _, err := e.st.Authenticate(context.Background(), []byte(tok)); err == nil {
		t.Fatal("raw token matched a stored value")
	}
	if _, err := e.st.Authenticate(context.Background(), auth.HashToken(tok)); err != nil {
		t.Fatal("hashed token did not match")
	}
}

// Unknown emails must cost a password hash too, so timing does not reveal
// which accounts exist.
func TestLoginTimingUnknownEmail(t *testing.T) {
	e := newEnv(t, func(o *Options) {
		h, err := auth.NewHasher(auth.Params{Memory: 16 * 1024, Time: 3, Threads: 1, SaltLen: 16, KeyLen: 32}, 4)
		if err != nil {
			t.Fatal(err)
		}
		o.Hasher = h
	})
	e.hasher = e.srv.hasher
	e.createUser("a@example.com", pwA)
	measure := func(email string, i int) time.Duration {
		start := time.Now()
		e.do(req{method: "POST", path: "/v1/auth/login", ip: fmt.Sprintf("192.0.2.%d", i),
			body: map[string]string{"email": email, "password": "wrong password!!"}})
		return time.Since(start)
	}
	minOf := func(email string) time.Duration {
		best := time.Hour
		for i := 0; i < 4; i++ {
			if d := measure(email, i+10*len(email)); d < best {
				best = d
			}
		}
		return best
	}
	known, unknown := minOf("a@example.com"), minOf("zz@example.com")
	ratio := float64(unknown) / float64(known)
	if ratio < 0.5 || ratio > 2 {
		t.Fatalf("timing differs: known %v unknown %v", known, unknown)
	}
}

func TestLockoutPerIP(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	ip := "203.0.113.5"
	for i := 0; i < 5; i++ {
		rec := e.do(req{method: "POST", path: "/v1/auth/login", ip: ip, body: map[string]string{"email": "a@example.com", "password": "nope nope nope"}})
		e.mustStatus(rec, http.StatusUnauthorized)
	}
	// The 6th attempt within a minute is refused, even with the right password.
	rec := e.do(req{method: "POST", path: "/v1/auth/login", ip: ip, body: map[string]string{"email": "a@example.com", "password": pwA}})
	e.mustStatus(rec, http.StatusTooManyRequests)
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	rec = e.do(req{method: "POST", path: "/v1/auth/login", ip: ip, body: map[string]string{"email": "a@example.com", "password": pwA}})
	e.mustStatus(rec, http.StatusTooManyRequests)
	if countKind(t, e, store.EventLockoutIP) != 1 || countKind(t, e, store.EventLoginBlocked) != 1 {
		t.Fatalf("events %v", kinds(t, e))
	}
	// Other IPs are unaffected.
	e.login("a@example.com", pwA, "203.0.113.6")
	// After the lockout the IP can log in again.
	e.clock.Add(61 * time.Second)
	e.login("a@example.com", pwA, ip)
}

func TestLockoutPerAccountWithBackoff(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	for _, email := range []string{"a@example.com", "ghost@example.com"} {
		for i := 0; i < 10; i++ {
			rec := e.do(req{method: "POST", path: "/v1/auth/login", ip: fmt.Sprintf("198.18.%d.%d", len(email), i),
				body: map[string]string{"email": email, "password": "nope nope nope"}})
			e.mustStatus(rec, http.StatusUnauthorized)
		}
		rec := e.do(req{method: "POST", path: "/v1/auth/login", ip: "198.18.99.1",
			body: map[string]string{"email": email, "password": pwA}})
		e.mustStatus(rec, http.StatusTooManyRequests)
		if rec.Header().Get("Retry-After") != "900" {
			t.Fatalf("%s: Retry-After %q", email, rec.Header().Get("Retry-After"))
		}
	}
	if countKind(t, e, store.EventLockoutAccount) != 2 {
		t.Fatalf("events %v", kinds(t, e))
	}
	// Exponential backoff: the next lockout lasts twice as long.
	e.clock.Add(16 * time.Minute)
	for i := 0; i < 10; i++ {
		e.do(req{method: "POST", path: "/v1/auth/login", ip: fmt.Sprintf("198.19.0.%d", i),
			body: map[string]string{"email": "a@example.com", "password": "nope nope nope"}})
	}
	rec := e.do(req{method: "POST", path: "/v1/auth/login", ip: "198.19.1.1", body: map[string]string{"email": "a@example.com", "password": pwA}})
	e.mustStatus(rec, http.StatusTooManyRequests)
	if rec.Header().Get("Retry-After") != "1800" {
		t.Fatalf("Retry-After %q, want doubled lockout", rec.Header().Get("Retry-After"))
	}
	e.clock.Add(31 * time.Minute)
	e.login("a@example.com", pwA, "198.19.2.1")
}

func TestExpiredTokens(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	get := func(tok string) int {
		return e.do(req{method: "GET", path: "/v1/projects", token: tok}).Code
	}

	// Idle expiry: 30 days without use.
	idle := e.login("a@example.com", pwA, "")
	e.clock.Add(30*24*time.Hour + time.Minute)
	if got := get(idle); got != http.StatusUnauthorized {
		t.Fatalf("idle token: %d", got)
	}

	// Sliding: used every 29 days it stays valid, until 180 days absolute.
	tok := e.login("a@example.com", pwA, "198.51.100.8")
	for day := 29; day < 180; day += 29 {
		e.clock.Add(29 * 24 * time.Hour)
		if got := get(tok); got != http.StatusOK {
			t.Fatalf("day %d: sliding session rejected (%d)", day, got)
		}
	}
	e.clock.Add(29 * 24 * time.Hour) // day 174 -> 203
	if got := get(tok); got != http.StatusUnauthorized {
		t.Fatalf("absolute expiry not enforced: %d", got)
	}

	// Garbage and missing tokens.
	for _, h := range []string{"", "Bearer", "Bearer x", "Basic abc", "Bearer " + strings.Repeat("A", 43)} {
		rec := e.do(req{method: "GET", path: "/v1/projects", header: map[string]string{"Authorization": h}})
		e.mustStatus(rec, http.StatusUnauthorized)
	}
}

func TestRevokedTokens(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	t1 := e.login("a@example.com", pwA, "")
	t2 := e.login("a@example.com", pwA, "")

	var list struct {
		Sessions []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}
	rec := e.do(req{method: "GET", path: "/v1/auth/sessions", token: t1})
	e.mustStatus(rec, http.StatusOK)
	decodeBody(t, rec, &list)
	if len(list.Sessions) != 2 {
		t.Fatalf("sessions %+v", list)
	}
	var other string
	for _, s := range list.Sessions {
		if !s.Current {
			other = s.ID
		}
	}
	e.mustStatus(e.do(req{method: "DELETE", path: "/v1/auth/sessions/" + other, token: t1}), http.StatusNoContent)
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects", token: t2}), http.StatusUnauthorized)
	// Revoking again is a 404.
	e.mustStatus(e.do(req{method: "DELETE", path: "/v1/auth/sessions/" + other, token: t1}), http.StatusNotFound)

	e.mustStatus(e.do(req{method: "POST", path: "/v1/auth/logout", token: t1}), http.StatusNoContent)
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects", token: t1}), http.StatusUnauthorized)
	if countKind(t, e, store.EventSessionRevoked) != 1 || countKind(t, e, store.EventLogout) != 1 {
		t.Fatalf("events %v", kinds(t, e))
	}

	// CLI revoke-all.
	t3 := e.login("a@example.com", pwA, "")
	if _, err := e.st.RevokeAllSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects", token: t3}), http.StatusUnauthorized)
}

func TestChangePassword(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	cur := e.login("a@example.com", pwA, "")
	other := e.login("a@example.com", pwA, "")

	rec := e.do(req{method: "POST", path: "/v1/auth/password", token: cur, body: map[string]string{"current_password": "wrong one here", "new_password": pwB}})
	e.mustStatus(rec, http.StatusBadRequest)
	rec = e.do(req{method: "POST", path: "/v1/auth/password", token: cur, body: map[string]string{"current_password": pwA, "new_password": "short"}})
	e.mustStatus(rec, http.StatusBadRequest)
	// Still valid after failed attempts.
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects", token: other}), http.StatusOK)

	rec = e.do(req{method: "POST", path: "/v1/auth/password", token: cur, body: map[string]string{"current_password": pwA, "new_password": pwB}})
	e.mustStatus(rec, http.StatusNoContent)
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects", token: cur}), http.StatusOK)
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects", token: other}), http.StatusUnauthorized)

	bad := e.do(req{method: "POST", path: "/v1/auth/login", ip: "192.0.2.200", body: map[string]string{"email": "a@example.com", "password": pwA}})
	e.mustStatus(bad, http.StatusUnauthorized)
	e.login("a@example.com", pwB, "192.0.2.201")
}

func TestChangePasswordCountsAgainstLockout(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	cur := e.login("a@example.com", pwA, "192.0.2.1")
	var last int
	for i := 0; i < 12; i++ {
		rec := e.do(req{method: "POST", path: "/v1/auth/password", token: cur, ip: fmt.Sprintf("192.0.2.%d", 10+i),
			body: map[string]string{"current_password": "guess guess guess", "new_password": pwB}})
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("password guessing not limited: %d", last)
	}
}
