package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"
)

// TOTP (RFC 6238) with the parameters every authenticator app defaults to:
// HMAC-SHA1, 30-second steps, 6 digits. Kept dependency-free on purpose —
// 2FA is core security hygiene and always CE (PRD §5.3).

const (
	totpStep   = 30 * time.Second
	totpDigits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a fresh 160-bit secret, base32-encoded for entry
// into authenticator apps.
func NewTOTPSecret() string {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand failure is not recoverable
	}
	return b32.EncodeToString(buf)
}

// TOTPCode computes the code for a secret at time t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := b32.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("invalid TOTP secret")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(t.Unix())/uint64(totpStep.Seconds()))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1_000_000), nil
}

// CheckTOTP verifies a code, accepting the previous and next time step so a
// slightly skewed clock still works. Comparison is constant-time.
func CheckTOTP(secret, code string, t time.Time) bool {
	if len(code) != totpDigits {
		return false
	}
	for _, dt := range []time.Duration{0, -totpStep, totpStep} {
		want, err := TOTPCode(secret, t.Add(dt))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// OTPAuthURL renders the otpauth:// URI that authenticator apps import
// (shown as text/QR at enrollment).
func OTPAuthURL(secret, account string) string {
	return "otpauth://totp/Logos:" + url.PathEscape(account) +
		"?secret=" + secret + "&issuer=Logos"
}
