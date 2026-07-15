package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const encryptedSecretVersion = "v1"

func HasPersistentCryptoSecret() bool {
	return strings.TrimSpace(os.Getenv("CRYPTO_SECRET")) != "" || strings.TrimSpace(os.Getenv("SESSION_SECRET")) != ""
}

func EncryptSecret(purpose string, plaintext string) (string, error) {
	if strings.TrimSpace(purpose) == "" {
		return "", errors.New("secret purpose is required")
	}
	if CryptoSecret == "" {
		return "", errors.New("crypto secret is not configured")
	}

	block, err := aes.NewCipher(secretEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(purpose))
	return encryptedSecretVersion + ":" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func DecryptSecret(purpose string, ciphertext string) (string, error) {
	if strings.TrimSpace(purpose) == "" {
		return "", errors.New("secret purpose is required")
	}
	if CryptoSecret == "" {
		return "", errors.New("crypto secret is not configured")
	}

	version, encoded, ok := strings.Cut(ciphertext, ":")
	if !ok || version != encryptedSecretVersion || encoded == "" {
		return "", errors.New("invalid encrypted secret format")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	block, err := aes.NewCipher(secretEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("encrypted secret is truncated")
	}

	nonce, payload := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, payload, []byte(purpose))
	if err != nil {
		return "", errors.New("decrypt encrypted secret failed")
	}
	return string(plaintext), nil
}

func secretEncryptionKey() []byte {
	digest := sha256.Sum256([]byte(CryptoSecret))
	return digest[:]
}
