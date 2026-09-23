package identity

import (
	"context"
	"errors"
	"time"

	domain "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

var ErrSessionNotFound = errors.New("session not found")

type SessionService struct {
	repository     SessionRepository
	accessResolver UserAccessResolver
	clock          clock.Clock
	ttl            time.Duration
}

func NewSessionService(repository SessionRepository, accessResolver UserAccessResolver, systemClock clock.Clock, ttl time.Duration) (*SessionService, error) {
	if repository == nil || accessResolver == nil || systemClock == nil {
		return nil, errors.New("session service dependencies are required")
	}
	if ttl <= 0 {
		return nil, errors.New("session ttl must be positive")
	}
	return &SessionService{
		repository:     repository,
		accessResolver: accessResolver,
		clock:          systemClock,
		ttl:            ttl,
	}, nil
}

func (s *SessionService) Create(ctx context.Context, principal security.Principal) (string, time.Time, error) {
	session, token, err := domain.NewSession(
		domain.UserID(principal.UserID),
		s.clock.Now(),
		s.ttl,
	)
	if err != nil {
		return "", time.Time{}, err
	}
	if _, err := s.repository.CreateSession(ctx, session); err != nil {
		return "", time.Time{}, err
	}
	return token, session.ExpiresAt, nil
}

func (s *SessionService) Authenticate(ctx context.Context, token string) (security.Principal, error) {
	session, err := s.repository.GetActiveSessionByTokenHash(ctx, domain.HashSessionToken(token))
	if errors.Is(err, ErrSessionNotFound) {
		return security.Principal{}, security.ErrUnauthenticated
	}
	if err != nil {
		return security.Principal{}, err
	}
	access, err := s.accessResolver.ResolveUser(ctx, session.UserID)
	if err != nil {
		return security.Principal{}, err
	}
	if _, err := s.repository.TouchSession(ctx, session.ID); err != nil && !errors.Is(err, ErrSessionNotFound) {
		return security.Principal{}, err
	}
	return PrincipalFromAccess(access), nil
}

func (s *SessionService) Revoke(ctx context.Context, token string) error {
	session, err := s.repository.GetActiveSessionByTokenHash(ctx, domain.HashSessionToken(token))
	if errors.Is(err, ErrSessionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.repository.RevokeSession(ctx, session.ID)
}

func PrincipalFromAccess(access Access) security.Principal {
	tenants := make([]security.TenantAccess, 0, len(access.Memberships))
	for _, membership := range access.Memberships {
		tenants = append(tenants, security.TenantAccess{
			TenantID: membership.TenantID,
			Slug:     membership.TenantSlug,
			Name:     membership.TenantName,
			Status:   membership.TenantStatus,
			RoleCode: membership.RoleCode,
		})
	}
	return security.Principal{
		UserID:        access.User.ID.UUID(),
		Subject:       access.User.GoogleSubject,
		Email:         access.User.Email,
		DisplayName:   access.User.DisplayName,
		TenantID:      access.TenantID,
		PlatformAdmin: access.PlatformAdmin,
		Tenants:       tenants,
		Scopes:        access.Scopes,
		Capabilities:  access.Capabilities,
		CompanyIDs:    access.CompanyIDs,
	}
}
