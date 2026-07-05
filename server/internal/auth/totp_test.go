package auth

import (
	"testing"
	"time"
)

// RFC 6238 appendix B vectors (SHA-1 secret "12345678901234567890"), with
// the last 6 digits of the published 8-digit codes.
func TestTOTPCodeRFCVectors(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	} {
		got, err := TOTPCode(secret, time.Unix(tc.unix, 0))
		if err != nil || got != tc.want {
			t.Errorf("t=%d: got %q err=%v, want %q", tc.unix, got, err, tc.want)
		}
	}
}

func TestCheckTOTP(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Unix(1751700000, 0)
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}

	if !CheckTOTP(secret, code, now) {
		t.Error("current code rejected")
	}
	// ±1 step tolerated (clock skew), ±2 not.
	if !CheckTOTP(secret, code, now.Add(30*time.Second)) || !CheckTOTP(secret, code, now.Add(-30*time.Second)) {
		t.Error("adjacent-step code rejected")
	}
	if CheckTOTP(secret, code, now.Add(90*time.Second)) {
		t.Error("stale code accepted")
	}
	if CheckTOTP(secret, "000000", now) && code != "000000" {
		t.Error("wrong code accepted")
	}
	if CheckTOTP(secret, code+"0", now) {
		t.Error("wrong-length code accepted")
	}
	if CheckTOTP("not base32!!", code, now) {
		t.Error("bad secret accepted")
	}
}

func TestOTPAuthURL(t *testing.T) {
	u := OTPAuthURL("SECRET", "admin@example.com")
	want := "otpauth://totp/Logos:admin@example.com?secret=SECRET&issuer=Logos"
	if u != want {
		t.Errorf("got %q, want %q", u, want)
	}
}
