package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const (
	keySizeBytes = 32
)

func EncryptString(secret, purpose, plaintext string) (cipherTextB64 string, nonceB64 string, err error) {
	plain := strings.TrimSpace(plaintext)
	if plain == "" {
		return "", "", fmt.Errorf("plaintext is empty")
	}

	key, err := deriveKey(secret, purpose)
	if err != nil {
		return "", "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", fmt.Errorf("generate nonce: %w", err)
	}

	encrypted := gcm.Seal(nil, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(encrypted), base64.StdEncoding.EncodeToString(nonce), nil
}

func DecryptString(secret, purpose, cipherTextB64, nonceB64 string) (string, error) {
	if strings.TrimSpace(cipherTextB64) == "" || strings.TrimSpace(nonceB64) == "" {
		return "", fmt.Errorf("ciphertext or nonce is empty")
	}

	key, err := deriveKey(secret, purpose)
	if err != nil {
		return "", err
	}

	cipherText, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cipherTextB64))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(strings.TrimSpace(nonceB64))
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return "", fmt.Errorf("invalid nonce size")
	}

	plain, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}
	return string(plain), nil
}

func deriveKey(secret, purpose string) ([]byte, error) {
	s := strings.TrimSpace(secret)
	if s == "" {
		return nil, fmt.Errorf("secret is empty")
	}
	p := strings.TrimSpace(purpose)
	if p == "" {
		return nil, fmt.Errorf("purpose is empty")
	}

	info := []byte("moevideo/" + p)
	salt := []byte("moevideo-sealed-text-v1")
	key, err := hkdf.Key(sha256.New, []byte(s), salt, string(info), keySizeBytes)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	return key, nil
}
