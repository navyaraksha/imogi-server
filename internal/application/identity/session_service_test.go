package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type sessionRepository struct {
	session domain.Session
}

func (r *sessionRepository) CreateSession(_ context.Context, session domain.Session) (domain.Session, error) {
	r.session = session
	return session, nil
}

func (r *sessionRepository) GetActiveSessionByTokenHash(_ context.Context, tokenHash []byte) (domain.Session, error) {
	if len(r.session.TokenHash) == 0 || string(r.session.TokenHash) != string(tokenHash) || r.session.RevokedAt != nil {
		return domain.Session{}, ErrSessionNotFound
	}
	return r.session, nil
}

func (r *sessionRepository) TouchSession(_ context.Context, _ uuid.UUID) (domain.Session, error) {
	return r.session, nil
}

func (r *sessionRepository) RevokeSession(_ context.Context, sessionID uuid.UUID) error {
	if r.session.ID != sessionID {
		return ErrSessionNotFound
	}
	now := time.Now().UTC()
	r.session.RevokedAt = &now
	return nil
}

type sessionAccessResolver struct {
	access Access
}

func (r sessionAccessResolver) ResolveUser(context.Context, domain.UserID) (Access, error) {
	return r.access, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

func TestSessionServiceCreatesAndAuthenticatesOpaqueSession(t *testing.T) {
	userID := uuid.Must(uuid.NewV7())
	repository := &sessionRepository{}
	access := Access{User: domain.User{ID: domain.UserID(userID), GoogleSubject: "google-subject", Email: "person@example.com"}}
	service, err := NewSessionService(repository, sessionAccessResolver{access: access}, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	token, expiresAt, err := service.Create(context.Background(), security.Principal{UserID: userID, Subject: "google-subject"})
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || string(repository.session.TokenHash) == token || !expiresAt.After(repository.session.CreatedAt) {
		t.Fatalf("session token or expiry was invalid: token=%q session=%+v", token, repository.session)
	}

	principal, err := service.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != userID || principal.Subject != "google-subject" {
		t.Fatalf("principal = %+v", principal)
	}

	if err := service.Revoke(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, security.ErrUnauthenticated) {
		t.Fatalf("authenticated revoked session with error %v", err)
	}
}
