package device

import (
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestInteropVector(t *testing.T) {
	const normalized = "W8TWTC3Q0SDVJYWPYCEW"
	const wantKeyHex = "0e0ad877c574165c19e5beb53d77ed8aae2d134d18fffc3805a19c8c1853c5ab"
	const wantCodeHash = "6352452f0e8c9bbe62b2b9d6d032c687b61f71f8c81568bc292f38239c1b7e75"
	const wantToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NSIsImV4cCI6MTc4ODA1NzE2N30.dGhpcy1pcy1hLXNhbXBsZS1zaWduYXR1cmU"
	const envelopeJSON = `{"v":1,"n":"AAAAAAAAAAAAAAAA","c":"BOHbo74BarLuVOI6PXcnpkOj2gM0qXTRP19KDfqJqDLI-A7m5VHNKWntRY2H1toAY8TkABQ1WMfLMCDTVKstG_uVKoN0fUrmP8UYNc1fb2eK7xof5pw0sig66O_MgHHjeJ79PtsXAps67aiE8r4sieEMm1tqfNnY4YDTDDR-lcB3vmed"}`

	key, err := deriveKey(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(key); got != wantKeyHex {
		t.Fatalf("deriveKey() = %s, want %s", got, wantKeyHex)
	}
	if got := codeHash(normalized); got != wantCodeHash {
		t.Fatalf("codeHash() = %s, want %s", got, wantCodeHash)
	}

	var env envelope
	if err := json.Unmarshal([]byte(envelopeJSON), &env); err != nil {
		t.Fatal(err)
	}
	token, err := decryptEnvelope(env, key)
	if err != nil {
		t.Fatal(err)
	}
	if token != wantToken {
		t.Fatalf("decryptEnvelope() = %q, want %q", token, wantToken)
	}
}

func TestNormalizeCode(t *testing.T) {
	if got := normalizeCode("w8twt-c3q0s-dvjyw-pycew"); got != "W8TWTC3Q0SDVJYWPYCEW" {
		t.Fatalf("normalizeCode() = %q", got)
	}
	if got := normalizeCode("ilo"); got != "110" {
		t.Fatalf("normalizeCode(ilo) = %q, want 110", got)
	}
}

func TestFormatCode(t *testing.T) {
	if got := formatCode("W8TWTC3Q0SDVJYWPYCEW"); got != "W8TWT-C3Q0S-DVJYW-PYCEW" {
		t.Fatalf("formatCode() = %q", got)
	}
}
