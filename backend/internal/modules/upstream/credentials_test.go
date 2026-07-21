package upstream

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSiteCredentialCipherRoundTripAndRejectsTampering(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cipher, err := NewSiteCredentialCipher(key)
	if err != nil {
		t.Fatalf("NewSiteCredentialCipher returned error: %v", err)
	}

	ciphertext, err := cipher.Encrypt("saved-password")
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	plaintext, err := cipher.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if plaintext != "saved-password" {
		t.Fatalf("Decrypt = %q, want saved password", plaintext)
	}

	tampered := []byte(ciphertext)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := cipher.Decrypt(string(tampered)); err == nil {
		t.Fatal("Decrypt accepted a tampered credential")
	}
}
