package identity

import (
	"context"
	"time"

	"github.com/google/uuid"
	domain "github.com/navyaraksha/imogi/internal/domain/identity"
)

type GoogleClaims struct {
	Subject       string
	Email         string
	DisplayName   string
	EmailVerified bool
	HostedDomain  string
}

type Membership struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	TenantID        uuid.UUID
	TenantSlug      string
	TenantName      string
	TenantStatus    string
	RoleCode        string
	Status          string
	UserEmail       string
	UserDisplayName string
	UserStatus      string
	GoogleLinked    bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AccessRepository interface {
	FindUserByGoogleSubject(context.Context, string) (domain.User, error)
	FindUserByEmail(context.Context, string) (domain.User, error)
	FindUserByID(context.Context, domain.UserID) (domain.User, error)
	UpsertGoogleUser(context.Context, domain.User) (domain.User, error)
	LinkGoogleIdentity(context.Context, domain.UserID, GoogleClaims) (domain.User, error)
	UpdateGoogleUserProfile(context.Context, domain.UserID, GoogleClaims) (domain.User, error)
	GetPlatformAdmin(context.Context, domain.UserID) (domain.User, error)
	ListActiveMembershipsByUser(context.Context, domain.UserID) ([]Membership, error)
	ListMembershipCompanyAccess(context.Context, Membership) ([]uuid.UUID, error)
	ListCompanyIDsByTenant(context.Context, uuid.UUID) ([]uuid.UUID, error)
	UpsertPlatformAdmin(context.Context, domain.UserID) error
}

type ProvisioningRepository interface {
	CreatePendingUser(context.Context, domain.User) (domain.User, error)
	ListPlatformUsers(context.Context, UserFilter) ([]domain.User, error)
	GetPlatformUser(context.Context, domain.UserID) (domain.User, error)
	BlockPlatformUser(context.Context, domain.UserID) (domain.User, error)
	CreateTenantMembership(context.Context, domain.UserID, uuid.UUID, string) (Membership, error)
	ProvisionTenantMembership(context.Context, ProvisionMembershipInput) (Membership, error)
	ListTenantMemberships(context.Context, TenantMembershipFilter) ([]Membership, error)
	GetTenantMembership(context.Context, uuid.UUID) (Membership, error)
	UpdateTenantMembershipRole(context.Context, uuid.UUID, string) (Membership, error)
	RevokeTenantMembership(context.Context, uuid.UUID) (Membership, error)
	ReactivateTenantMembership(context.Context, uuid.UUID) (Membership, error)
}

type Repository interface {
	AccessRepository
	ProvisioningRepository
}

type SessionRepository interface {
	CreateSession(context.Context, domain.Session) (domain.Session, error)
	GetActiveSessionByTokenHash(context.Context, []byte) (domain.Session, error)
	TouchSession(context.Context, uuid.UUID) (domain.Session, error)
	RevokeSession(context.Context, uuid.UUID) error
}

type UserFilter struct {
	Cursor *uuid.UUID
	Search string
	Status string
	Limit  int
}

type TenantMembershipFilter struct {
	TenantID uuid.UUID
	Cursor   *uuid.UUID
	Limit    int
}

type ProvisionMembershipInput struct {
	TenantID    uuid.UUID
	Email       string
	DisplayName string
	RoleCode    string
}

type AccessResolver interface {
	ResolveGoogleClaims(context.Context, GoogleClaims) (Access, error)
}

type UserAccessResolver interface {
	ResolveUser(context.Context, domain.UserID) (Access, error)
}

type Access struct {
	User          domain.User
	TenantID      uuid.UUID
	PlatformAdmin bool
	Memberships   []Membership
	Scopes        map[string]struct{}
	Capabilities  map[string]struct{}
	CompanyIDs    map[uuid.UUID]struct{}
}
