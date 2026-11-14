package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"tracker/api/internal/auth"
	"tracker/api/internal/store"
)

const genericLoginError = "invalid email or password"

type loginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name"`
}

type loginResponse struct {
	Token   string        `json:"token"`
	Session store.Session `json:"session"`
}

// login authenticates with email and password and returns a new session
// token. Unknown emails take the same time as wrong passwords (a dummy hash
// is computed) and every failure returns the same generic error. Lockouts are
// keyed by the normalised email, so they behave identically whether or not
// the account exists.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	ip := clientIPFrom(r)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	acctKey := "acct:" + email

	if locked, d := s.lockedOut(ip, acctKey); locked {
		s.audit(r, "", store.EventLoginBlocked)
		retryAfter(w, d)
		return
	}
	if locked, d := s.recordLoginAttempt(r, ip, acctKey); locked {
		retryAfter(w, d)
		return
	}

	device := strings.TrimSpace(req.DeviceName)
	if device == "" {
		device = "Unknown device"
	}
	device, devErr := singleLine("device_name", device, maxDeviceName)

	if email == "" || len(email) > maxEmail || len(req.Password) > auth.MaxPasswordLen*4 {
		s.hasher.VerifyDummy(req.Password)
		s.audit(r, "", store.EventLoginFailure)
		writeError(w, http.StatusUnauthorized, genericLoginError)
		return
	}
	user, err := s.store.UserByEmail(ctx, email)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.hasher.VerifyDummy(req.Password)
		s.audit(r, "", store.EventLoginFailure)
		writeError(w, http.StatusUnauthorized, genericLoginError)
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	if !s.hasher.Verify(req.Password, user.PasswordHash) {
		s.audit(r, user.ID, store.EventLoginFailure)
		writeError(w, http.StatusUnauthorized, genericLoginError)
		return
	}
	// Validate the device name only after authentication so that a
	// malformed one cannot be used to probe for valid credentials faster.
	if devErr != nil {
		s.fail(w, r, devErr)
		return
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	sess, err := s.store.CreateSession(ctx, user.ID, hash, device)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, user.ID, store.EventLoginSuccess)
	writeJSON(w, http.StatusOK, loginResponse{Token: token, Session: sess})
}

func (s *Server) lockedOut(ip, acctKey string) (bool, time.Duration) {
	l1, d1 := s.loginIP.Check(ip)
	l2, d2 := s.loginAccount.Check(acctKey)
	if d2 > d1 {
		d1 = d2
	}
	return l1 || l2, d1
}

// recordLoginAttempt counts an attempt against both the IP and the account.
// If that pushes either over its limit, the attempt is refused and the
// lockout is written to the audit trail.
func (s *Server) recordLoginAttempt(r *http.Request, ip, acctKey string) (bool, time.Duration) {
	ipLocked, d1 := s.loginIP.Record(ip)
	acctLocked, d2 := s.loginAccount.Record(acctKey)
	if ipLocked {
		s.audit(r, "", store.EventLockoutIP)
	}
	if acctLocked {
		uid := ""
		if u, err := s.store.UserByEmail(r.Context(), strings.TrimPrefix(acctKey, "acct:")); err == nil {
			uid = u.ID
		}
		s.audit(r, uid, store.EventLockoutAccount)
	}
	if d2 > d1 {
		d1 = d2
	}
	return ipLocked || acctLocked, d1
}

// audit records an auth event; failure to write it is logged, not fatal.
func (s *Server) audit(r *http.Request, userID, kind string) {
	if err := s.store.RecordAuthEvent(r.Context(), userID, kind, clientIPFrom(r)); err != nil {
		s.log.Error("audit write failed", "request_id", requestID(r), "kind", kind, "err", err)
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if err := s.store.RevokeSession(r.Context(), sess.UserID, sess.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, sess.UserID, store.EventLogout)
	w.WriteHeader(http.StatusNoContent)
}

type sessionView struct {
	store.Session
	Current bool `json:"current"`
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	cur := sessionFrom(r)
	list, err := s.store.ListSessions(r.Context(), cur.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]sessionView, 0, len(list))
	for _, sess := range list {
		out = append(out, sessionView{Session: sess, Current: sess.ID == cur.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cur := sessionFrom(r)
	if err := s.store.RevokeSession(r.Context(), cur.UserID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, cur.UserID, store.EventSessionRevoked)
	w.WriteHeader(http.StatusNoContent)
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changePassword verifies the current password, sets the new one and revokes
// every other session. Attempts count against the account's login budget so
// a stolen token cannot be used to brute-force the password.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req passwordRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	cur := sessionFrom(r)
	user, err := s.store.UserByID(r.Context(), cur.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ip := clientIPFrom(r)
	acctKey := "acct:" + strings.ToLower(user.Email)
	if locked, d := s.lockedOut(ip, acctKey); locked {
		s.audit(r, user.ID, store.EventLoginBlocked)
		retryAfter(w, d)
		return
	}
	if locked, d := s.recordLoginAttempt(r, ip, acctKey); locked {
		retryAfter(w, d)
		return
	}
	if len(req.CurrentPassword) > auth.MaxPasswordLen*4 || !s.hasher.Verify(req.CurrentPassword, user.PasswordHash) {
		s.audit(r, user.ID, store.EventPasswordFailure)
		writeError(w, http.StatusBadRequest, "current password is incorrect")
		return
	}
	if err := auth.CheckPolicy(req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := s.hasher.Hash(req.NewPassword)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err := s.store.SetPassword(r.Context(), user.ID, hash, cur.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, user.ID, store.EventPasswordChanged)
	w.WriteHeader(http.StatusNoContent)
}
