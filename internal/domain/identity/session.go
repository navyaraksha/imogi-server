package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
)

const sessionTokenBytes = 32

type Session struct {
	ID         uuid.UUID
	UserID     UserID
	TokenHash  []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

func NewSession(userID UserID, now time.Time, ttl time.Duration) (Session, string, error) {
	if userID.UUID() == uuid.Nil {
		return Session{}, "", errors.New("session user id is required")
	}
	if ttl <= 0 {
		return Session{}, "", errors.New("session ttl must be positive")
	}
	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Session{}, "", err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Session{}, "", err
	}
	now = now.UTC()
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	return Session{
		ID:         id,
		UserID:     userID,
		TokenHash:  HashSessionToken(token),
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
		LastSeenAt: now,
	}, token, nil
}

func HashSessionToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func (s Session) ActiveAt(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}
