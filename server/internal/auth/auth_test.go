package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundtrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Error("correct password rejected")
	}
	if CheckPassword(hash, "wrong password") {
		t.Error("wrong password accepted")
	}
}

func TestTokens(t *testing.T) {
	a, b := NewToken(), NewToken()
	if a == b {
		t.Fatal("two tokens are identical")
	}
	if len(a) != 43 {
		t.Errorf("token length = %d, want 43", len(a))
	}
	if !TokensEqual(HashToken(a), a) {
		t.Error("token does not match its own hash")
	}
	if TokensEqual(HashToken(a), b) {
		t.Error("different token matched")
	}
}

func TestClaimCodeFormat(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		code := NewClaimCode()
		if seen[code] {
			t.Fatalf("duplicate code generated: %s", code)
		}
		seen[code] = true
		if !strings.HasPrefix(code, "LG-") || len(code) != len("LG-XXXXX-XXXXX") {
			t.Fatalf("unexpected code format: %q", code)
		}
		for _, c := range code[3:] {
			if c == '-' {
				continue
			}
			if !strings.ContainsRune(claimAlphabet, c) {
				t.Fatalf("code %q contains char %q outside the alphabet", code, c)
			}
		}
	}
}

func TestNormalizeClaimCode(t *testing.T) {
	if got := NormalizeClaimCode("  lg-7xk2m-9rq4t\n"); got != "LG-7XK2M-9RQ4T" {
		t.Errorf("NormalizeClaimCode = %q", got)
	}
}
