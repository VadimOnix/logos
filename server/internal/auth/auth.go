// Package auth implements password hashing and opaque-token handling.
//
// Tokens (sessions, API tokens, node tokens) are 256-bit random strings shown
// once; only their SHA-256 digest is stored, so a database leak does not leak
// credentials (PRD §6: secrets encrypted/hashed at rest).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewToken returns a fresh opaque bearer token (43 chars, base64url of 32 random bytes).
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken derives the storable digest of a token.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// TokensEqual compares a presented token against a stored hash in constant time.
func TokensEqual(storedHash []byte, presented string) bool {
	h := HashToken(presented)
	return subtle.ConstantTimeCompare(storedHash, h) == 1
}

// claimAlphabet avoids visually ambiguous characters (0/O, 1/I/L, U/V).
const claimAlphabet = "23456789ABCDEFGHJKMNPQRSTWXYZ"

// NewClaimCode returns a human-typable enrollment code like "LG-7XK2M-9RQ4T".
// ~47 bits of entropy; codes are additionally single-use, expiring, and the
// enrollment endpoint is rate-limited.
func NewClaimCode() string {
	var sb strings.Builder
	sb.WriteString("LG-")
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i, c := range b {
		if i == 5 {
			sb.WriteByte('-')
		}
		sb.WriteByte(claimAlphabet[int(c)%len(claimAlphabet)])
	}
	return sb.String()
}

// NormalizeClaimCode makes user input forgiving: trims, uppercases.
func NormalizeClaimCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
