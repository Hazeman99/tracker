// Package auth implements password hashing, session tokens and random ids.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Params are Argon2id cost parameters.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// DefaultParams follow the OWASP Password Storage Cheat Sheet recommendation
// for Argon2id: m=19 MiB, t=2, p=1.
var DefaultParams = Params{Memory: 19 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// Password length rules. Length is counted in Unicode code points.
const (
	MinPasswordLen = 12
	MaxPasswordLen = 1024
)

// ErrPasswordPolicy is returned when a new password violates the length rules.
var ErrPasswordPolicy = fmt.Errorf("password must be between %d and %d characters", MinPasswordLen, MaxPasswordLen)

// CheckPolicy validates a new password.
func CheckPolicy(pw string) error {
	if !utf8.ValidString(pw) {
		return ErrPasswordPolicy
	}
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen || n > MaxPasswordLen {
		return ErrPasswordPolicy
	}
	return nil
}

// Hasher hashes and verifies passwords. It bounds the number of concurrent
// Argon2 computations so that a burst of logins cannot exhaust memory.
type Hasher struct {
	params Params
	sem    chan struct{}
	dummy  string
}

// NewHasher returns a Hasher using p, allowing at most concurrency hashes at once.
func NewHasher(p Params, concurrency int) (*Hasher, error) {
	if concurrency < 1 {
		concurrency = 1
	}
	h := &Hasher{params: p, sem: make(chan struct{}, concurrency)}
	dummy, err := h.Hash("dummy password used to equalise login timing")
	if err != nil {
		return nil, err
	}
	h.dummy = dummy
	return h, nil
}

// Hash returns an encoded Argon2id hash in the PHC string format.
func (h *Hasher) Hash(pw string) (string, error) {
	salt := make([]byte, h.params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h.sem <- struct{}{}
	key := argon2.IDKey([]byte(pw), salt, h.params.Time, h.params.Memory, h.params.Threads, h.params.KeyLen)
	<-h.sem
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.Memory, h.params.Time, h.params.Threads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether pw matches the encoded hash. The comparison of the
// derived keys is constant-time.
func (h *Hasher) Verify(pw, encoded string) bool {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return false
	}
	h.sem <- struct{}{}
	got := argon2.IDKey([]byte(pw), salt, p.Time, p.Memory, p.Threads, uint32(len(want))) // #nosec G115 -- len(want) is bounded by decode
	<-h.sem
	return subtle.ConstantTimeCompare(got, want) == 1
}

// VerifyDummy performs a verification against a fixed hash and discards the
// result. It is used for unknown accounts so that login takes the same time
// whether or not the email exists.
func (h *Hasher) VerifyDummy(pw string) {
	_ = h.Verify(pw, h.dummy)
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("invalid hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, errors.New("unsupported argon2 version")
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return Params{}, nil, nil, errors.New("invalid hash parameters")
	}
	// Refuse absurd parameters from a tampered database row.
	if p.Memory < 1024 || p.Memory > 1<<20 || p.Time < 1 || p.Time > 10 || p.Threads < 1 || p.Threads > 16 {
		return Params{}, nil, nil, errors.New("hash parameters out of range")
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return Params{}, nil, nil, errors.New("invalid salt")
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return Params{}, nil, nil, errors.New("invalid key")
	}
	return p, salt, key, nil
}
