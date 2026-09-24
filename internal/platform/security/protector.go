package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidCiphertext = errors.New("invalid sensitive data ciphertext")

type SensitiveDataProtector interface {
	Encrypt(plaintext string) ([]byte, error)
	Decrypt(ciphertext []byte) (string, error)
	LookupHash(plaintext string) []byte
}

type AESGCMProtector struct {
	aead      cipher.AEAD
	lookupKey []byte
}

func NewAESGCMProtector(encryptionKey, lookupKey []byte) (*AESGCMProtector, error) {
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("create sensitive data cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create sensitive data AEAD: %w", err)
	}
	if len(lookupKey) == 0 {
		return nil, errors.New("sensitive data lookup key cannot be empty")
	}
	return &AESGCMProtector{aead: aead, lookupKey: append([]byte(nil), lookupKey...)}, nil
}

func (p *AESGCMProtector) Encrypt(plaintext string) ([]byte, error) {
	// A fresh nonce is required for every AES-GCM encryption to preserve the
	// confidentiality guarantees of the cipher.
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	return p.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func (p *AESGCMProtector) Decrypt(ciphertext []byte) (string, error) {
	if len(ciphertext) < p.aead.NonceSize() {
		return "", ErrInvalidCiphertext
	}
	nonce, payload := ciphertext[:p.aead.NonceSize()], ciphertext[p.aead.NonceSize():]
	plaintext, err := p.aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCiphertext, err)
	}
	return string(plaintext), nil
}

func (p *AESGCMProtector) LookupHash(plaintext string) []byte {
	mac := hmac.New(sha256.New, p.lookupKey)
	_, _ = mac.Write([]byte(plaintext))
	return mac.Sum(nil)
}
