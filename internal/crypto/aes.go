package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

const (
	// encryptedPrefix marks ciphertext so we can distinguish from plaintext.
	encryptedPrefix = "enc::"
)

var (
	initOnce sync.Once
	aesKey   []byte
	initErr  error
)

// loadKey reads the 32-byte AES-256 key from CHANNEL_CONFIG_KEY env var.
// If unset, encryption is disabled (passthrough mode).
// If set but invalid, loadKey records an error so callers can fail loudly.
func loadKey() {
	initOnce.Do(func() {
		raw := os.Getenv("CHANNEL_CONFIG_KEY")
		if raw == "" {
			log.Println("WARNING: CHANNEL_CONFIG_KEY is not set — channel configs will be stored in plaintext")
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			initErr = fmt.Errorf("CHANNEL_CONFIG_KEY is set but not valid base64: %w", err)
			return
		}
		if len(decoded) != 32 {
			initErr = fmt.Errorf("CHANNEL_CONFIG_KEY must decode to exactly 32 bytes, got %d", len(decoded))
			return
		}
		aesKey = decoded
	})
}

// IsEnabled returns true if an encryption key is configured and valid.
func IsEnabled() bool {
	loadKey()
	return len(aesKey) == 32 && initErr == nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns the original string if encryption is not configured.
// Returns an error if CHANNEL_CONFIG_KEY is set but invalid.
func Encrypt(plaintext string) (string, error) {
	loadKey()
	if initErr != nil {
		return "", initErr
	}
	if len(aesKey) != 32 {
		return plaintext, nil
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext produced by Encrypt.
// If the value is not prefixed (legacy plaintext), returns it unchanged.
func Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value, nil
	}

	loadKey()
	// H2: Fail closed if the key was set but invalid (e.g. wrong length, bad base64)
	if initErr != nil {
		return "", fmt.Errorf("encrypted config found but encryption key is invalid: %w", initErr)
	}
	if len(aesKey) != 32 {
		return "", fmt.Errorf("encrypted config found but no encryption key configured")
	}

	encoded := strings.TrimPrefix(value, encryptedPrefix)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}
