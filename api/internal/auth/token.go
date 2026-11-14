package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// tokenBytes is the session token entropy: 256 bits.
const tokenBytes = 32

// tokenLen is the length of an encoded token (unpadded base64url of 32 bytes).
var tokenLen = base64.RawURLEncoding.EncodedLen(tokenBytes)

// NewToken returns a new opaque session token and the SHA-256 hash that is
// stored in the database. The token itself is never stored.
func NewToken() (token string, hash []byte, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken returns the SHA-256 hash of a token.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// WellFormedToken reports whether s has the shape of a token this server
// issues, so obviously bogus values never reach the database.
func WellFormedToken(s string) bool {
	if len(s) != tokenLen {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return err == nil && len(b) == tokenBytes
}

// NewID returns a random (version 4) UUID in canonical lowercase form.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read never returns an error on supported platforms.
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ValidID reports whether s is a canonical lowercase UUID string.
func ValidID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return false
			}
		}
	}
	return true
}
