package upstream

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const siteCredentialCipherVersion = "v1"

// SiteCredentialCipher protects persisted upstream login passwords. The key is
// supplied only by the server environment and is never exposed through APIs.
type SiteCredentialCipher struct {
	aead cipher.AEAD
}

func NewSiteCredentialCipher(encodedKey string) (*SiteCredentialCipher, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil {
		return nil, fmt.Errorf("decode upstream credential key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("upstream credential key must decode to 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create upstream credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create upstream credential gcm: %w", err)
	}
	return &SiteCredentialCipher{aead: aead}, nil
}

func (c *SiteCredentialCipher) Encrypt(plaintext string) (string, error) {
	if c == nil || c.aead == nil {
		return "", fmt.Errorf("upstream credential cipher unavailable")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate upstream credential nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, sealed...)
	return siteCredentialCipherVersion + ":" + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (c *SiteCredentialCipher) Decrypt(ciphertext string) (string, error) {
	if c == nil || c.aead == nil {
		return "", fmt.Errorf("upstream credential cipher unavailable")
	}
	prefix := siteCredentialCipherVersion + ":"
	if !strings.HasPrefix(ciphertext, prefix) {
		return "", fmt.Errorf("unsupported upstream credential ciphertext")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(ciphertext, prefix))
	if err != nil || len(payload) < c.aead.NonceSize() {
		return "", fmt.Errorf("invalid upstream credential ciphertext")
	}
	nonce := payload[:c.aead.NonceSize()]
	plaintext, err := c.aead.Open(nil, nonce, payload[c.aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt upstream credential: %w", err)
	}
	return string(plaintext), nil
}
