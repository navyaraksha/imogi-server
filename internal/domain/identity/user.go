package identity

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type UserID uuid.UUID

type UserStatus string

const (
	UserPending UserStatus = "pending"
	UserActive  UserStatus = "active"
	UserBlocked UserStatus = "blocked"
)

type User struct {
	ID            UserID
	GoogleSubject string
	Email         string
	DisplayName   string
	Status        UserStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewUserID() (UserID, error) {
	id, err := NewUUIDv7()
	return UserID(id), err
}

func ParseUserID(value string) (UserID, error) {
	id, err := ParseUUIDv7(value)
	if err != nil {
		return UserID(uuid.Nil), fmt.Errorf("user id: %w", err)
	}
	return UserID(id), nil
}

func (id UserID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id UserID) String() string  { return id.UUID().String() }

func NewUser(id UserID, googleSubject, email, displayName string, now time.Time) (User, error) {
	googleSubject = strings.TrimSpace(googleSubject)
	email = strings.TrimSpace(strings.ToLower(email))
	displayName = strings.TrimSpace(displayName)
	if googleSubject == "" || len(googleSubject) > 255 {
		return User{}, errorsInvalidUser("google subject")
	}
	if email == "" || len(email) > 320 {
		return User{}, errorsInvalidUser("email")
	}
	if displayName == "" || len(displayName) > 200 {
		return User{}, errorsInvalidUser("display name")
	}
	now = now.UTC()
	return User{
		ID:            id,
		GoogleSubject: googleSubject,
		Email:         email,
		DisplayName:   displayName,
		Status:        UserActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func NewPendingUser(id UserID, email, displayName string, now time.Time) (User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	displayName = strings.TrimSpace(displayName)
	if email == "" || len(email) > 320 {
		return User{}, errorsInvalidUser("email")
	}
	if displayName == "" {
		displayName = email
	}
	if len(displayName) > 200 {
		return User{}, errorsInvalidUser("display name")
	}
	now = now.UTC()
	return User{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		Status:      UserPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func errorsInvalidUser(field string) error {
	return fmt.Errorf("invalid user %s", field)
}
