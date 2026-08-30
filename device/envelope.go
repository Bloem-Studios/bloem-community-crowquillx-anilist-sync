package device

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	kdfSalt = "silo-anilist-sync-bridge-v1"
	kdfInfo = "anilist-access-token"
	aad     = "v1"
)

// envelope is the zero-knowledge transport shape produced by the connect
// bridge's browser side and consumed by Poll. v is always 1; n and c are
// base64url (RawURLEncoding) nonce and ciphertext||tag.
type envelope struct {
	V int    `json:"v"`
	N string `json:"n"`
	C string `json:"c"`
}

// deriveKey re-derives the AES-256 key from the normalized user code using the
// fixed HKDF-SHA256 parameters shared with the bridge's WebCrypto side.
func deriveKey(normalizedCode string) ([]byte, error) {
	return hkdf.Key(sha256.New, []byte(normalizedCode), []byte(kdfSalt), kdfInfo, 32)
}

// codeHash reports the SHA-256 hex digest of the normalized code, which is the
// bridge's poll-path identifier.
func codeHash(normalizedCode string) string {
	sum := sha256.Sum256([]byte(normalizedCode))
	return hex.EncodeToString(sum[:])
}

func decryptEnvelope(env envelope, key []byte) (string, error) {
	if env.V != 1 {
		return "", fmt.Errorf("unsupported envelope version %d", env.V)
	}
	nonce, err := base64.RawURLEncoding.DecodeString(env.N)
	if err != nil {
		return "", fmt.Errorf("decode envelope nonce: %w", err)
	}
	if len(nonce) != 12 {
		return "", errors.New("envelope nonce must be 12 bytes")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(env.C)
	if err != nil {
		return "", fmt.Errorf("decode envelope ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("init AES-256-GCM: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("init AES-256-GCM: %w", err)
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return "", fmt.Errorf("open envelope: %w", err)
	}
	return string(plaintext), nil
}
