package anilist

import (
	"testing"
	"time"
)

func TestTokenExpiresAt(t *testing.T) {
	const valid = "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjE3MDAwMDAwMDAsInN1YiI6IjEyMyJ9.e30"
	if got := TokenExpiresAt(valid); !got.Equal(time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)) {
		t.Fatalf("TokenExpiresAt(valid) = %v, want 2023-11-14T22:13:20Z", got)
	}

	const withoutExp = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.e30"
	if got := TokenExpiresAt(withoutExp); !got.IsZero() {
		t.Fatalf("TokenExpiresAt(without exp) = %v, want zero", got)
	}

	if got := TokenExpiresAt("not a jwt"); !got.IsZero() {
		t.Fatalf("TokenExpiresAt(garbage) = %v, want zero", got)
	}
	if got := TokenExpiresAt("eyJhbGciOiJIUzI1NiJ9.!!!!!.e30"); !got.IsZero() {
		t.Fatalf("TokenExpiresAt(bad base64) = %v, want zero", got)
	}

	if got := TokenExpiresAt("only.two"); !got.IsZero() {
		t.Fatalf("TokenExpiresAt(two segments) = %v, want zero", got)
	}
}
