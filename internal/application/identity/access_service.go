package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

var (
	ErrIdentityNotProvisioned   = errors.New("identity is not provisioned")
	ErrIdentityBlocked          = errors.New("identity is blocked")
	ErrIdentityConflict         = errors.New("identity conflict")
	ErrPlatformAdminNotFound    = errors.New("platform admin is not provisioned")
	ErrPlatformUserNotFound     = errors.New("platform user not found")
	ErrTenantMembershipNotFound = errors.New("tenant membership not found")
	ErrTenantNotFound           = errors.New("tenant not found")
	ErrActiveTenantRequired     = errors.New("active tenant is required")
	ErrTenantAccessDenied       = errors.New("tenant access denied")
)

type AccessService struct {
	repository      AccessRepository
	bootstrapEmails map[string]struct{}
}

func NewAccessService(repository AccessRepository, bootstrapEmails []string) (*AccessService, error) {
	if repository == nil {
		return nil, errors.New("identity repository is required")
	}
	allowed := make(map[string]struct{}, len(bootstrapEmails))
	for _, email := range bootstrapEmails {
		email = strings.TrimSpace(strings.ToLower(email))
		if email != "" {
			allowed[email] = struct{}{}
		}
	}
	return &AccessService{repository: repository, bootstrapEmails: allowed}, nil
}

func (s *AccessService) ResolveGoogleClaims(ctx context.Context, claims GoogleClaims) (Access, error) {
	if !claims.EmailVerified {
		return Access{}, fmt.Errorf("%w: google email is not verified", security.ErrForbidden)
	}
	user, err := s.resolveGoogleUser(ctx, claims)
	if err != nil {
		return Access{}, err
	}
	return s.resolveUserAccess(ctx, user)
}

func (s *AccessService) ResolveUser(ctx context.Context, userID domain.UserID) (Access, error) {
	user, err := s.repository.FindUserByID(ctx, userID)
	if err != nil {
		return Access{}, err
	}
	return s.resolveUserAccess(ctx, user)
}

func (s *AccessService) resolveUserAccess(ctx context.Context, user domain.User) (Access, error) {
	if user.Status != domain.UserActive {
		return Access{}, ErrIdentityBlocked
	}

	platformAdmin := false
	if _, allowed := s.bootstrapEmails[user.Email]; allowed {
		if err := s.repository.UpsertPlatformAdmin(ctx, user.ID); err != nil {
			return Access{}, err
		}
		platformAdmin = true
	} else {
		if _, err := s.repository.GetPlatformAdmin(ctx, user.ID); err == nil {
			platformAdmin = true
		} else if !errors.Is(err, ErrPlatformAdminNotFound) {
			return Access{}, err
		}
	}

	access := Access{
		User:          user,
		PlatformAdmin: platformAdmin,
		Scopes:        make(map[string]struct{}),
		Capabilities:  make(map[string]struct{}),
		CompanyIDs:    make(map[uuid.UUID]struct{}),
	}
	if platformAdmin {
		grantPlatformAccess(&access)
	}

	memberships, err := s.repository.ListActiveMembershipsByUser(ctx, user.ID)
	if err != nil {
		return Access{}, err
	}
	if len(memberships) == 0 && !platformAdmin {
		return Access{}, ErrIdentityNotProvisioned
	}
	access.Memberships = memberships

	tenantID := security.TenantSelectorFromContext(ctx)
	var membership *Membership
	if tenantID != uuid.Nil {
		for index := range memberships {
			if memberships[index].TenantID == tenantID {
				membership = &memberships[index]
				break
			}
		}
		if membership == nil {
			return Access{}, ErrTenantAccessDenied
		}
	} else if len(memberships) == 1 {
		membership = &memberships[0]
	}
	if membership == nil {
		return access, nil
	}
	access.TenantID = membership.TenantID
	grantTenantAccess(&access, membership.RoleCode)
	companyIDs, err := s.companyAccess(ctx, *membership)
	if err != nil {
		return Access{}, err
	}
	for _, companyID := range companyIDs {
		access.CompanyIDs[companyID] = struct{}{}
	}
	return access, nil
}

func (s *AccessService) resolveGoogleUser(ctx context.Context, claims GoogleClaims) (domain.User, error) {
	user, err := s.repository.FindUserByGoogleSubject(ctx, claims.Subject)
	if err == nil {
		if user.Status == domain.UserBlocked {
			return domain.User{}, ErrIdentityBlocked
		}
		return s.updateGoogleProfile(ctx, user.ID, claims)
	}
	if !errors.Is(err, ErrPlatformUserNotFound) {
		return domain.User{}, err
	}

	user, err = s.repository.FindUserByEmail(ctx, strings.ToLower(strings.TrimSpace(claims.Email)))
	if err == nil {
		if user.Status == domain.UserBlocked {
			return domain.User{}, ErrIdentityBlocked
		}
		if user.GoogleSubject != "" && user.GoogleSubject != claims.Subject {
			return domain.User{}, ErrIdentityConflict
		}
		if user.GoogleSubject == "" {
			linked, linkErr := s.repository.LinkGoogleIdentity(ctx, user.ID, claims)
			if errors.Is(linkErr, ErrPlatformUserAlreadyExists) {
				return domain.User{}, ErrIdentityConflict
			}
			return linked, linkErr
		}
		return s.updateGoogleProfile(ctx, user.ID, claims)
	}
	if !errors.Is(err, ErrPlatformUserNotFound) {
		return domain.User{}, err
	}

	if _, allowed := s.bootstrapEmails[strings.ToLower(strings.TrimSpace(claims.Email))]; !allowed {
		return domain.User{}, ErrIdentityNotProvisioned
	}

	userID, err := domain.NewUserID()
	if err != nil {
		return domain.User{}, fmt.Errorf("generate user id: %w", err)
	}
	user, err = domain.NewUser(userID, claims.Subject, claims.Email, claims.DisplayName, time.Now())
	if err != nil {
		return domain.User{}, err
	}
	return s.repository.UpsertGoogleUser(ctx, user)
}

func (s *AccessService) updateGoogleProfile(ctx context.Context, userID domain.UserID, claims GoogleClaims) (domain.User, error) {
	user, err := s.repository.UpdateGoogleUserProfile(ctx, userID, claims)
	if errors.Is(err, ErrPlatformUserAlreadyExists) {
		return domain.User{}, ErrIdentityConflict
	}
	return user, err
}

func (s *AccessService) companyAccess(ctx context.Context, membership Membership) ([]uuid.UUID, error) {
	if membership.RoleCode == "tenant_admin" {
		return s.repository.ListCompanyIDsByTenant(ctx, membership.TenantID)
	}
	return s.repository.ListMembershipCompanyAccess(ctx, membership)
}

func grantPlatformAccess(access *Access) {
	grant(access, "platform:tenant:read", security.CapabilityPlatformTenantRead)
	grant(access, "platform:tenant:write", security.CapabilityPlatformTenantCreate)
	grant(access, "platform:tenant:write", security.CapabilityPlatformTenantUpdate)
	grant(access, "platform:tenant:write", security.CapabilityPlatformTenantActivate)
	grant(access, "platform:tenant:write", security.CapabilityPlatformTenantSuspend)
	grant(access, "platform:tenant:write", security.CapabilityPlatformTenantArchive)
	grant(access, "platform:user:read", security.CapabilityPlatformUserRead)
	grant(access, "platform:user:write", security.CapabilityPlatformUserCreate, security.CapabilityPlatformUserBlock)
	grant(access, "platform:membership:read", security.CapabilityPlatformMembershipRead)
	grant(access, "platform:membership:write", security.CapabilityPlatformMembershipWrite)
}

func grantTenantAccess(access *Access, role string) {
	grant(access, "tenant:read", security.CapabilityTenantRead)
	grant(access, "tenant:write", security.CapabilityTenantUpdate)
	grant(access, "organization:read", security.CapabilityCompanyRead, security.CapabilityOrganizationUnitRead)
	grant(access, "organization:write", security.CapabilityCompanyCreate, security.CapabilityCompanyUpdate, security.CapabilityCompanyActivate, security.CapabilityCompanySuspend, security.CapabilityCompanyArchive, security.CapabilityOrganizationUnitCreate, security.CapabilityOrganizationUnitUpdate, security.CapabilityOrganizationUnitArchive)
	switch role {
	case "tenant_admin":
		grant(access, "membership:read", security.CapabilityMembershipRead)
		grant(access, "membership:write", security.CapabilityMembershipWrite)
		grant(access, "audit:read", "audit.read")
	case "hr_admin":
		grant(access, "employee:read-basic", security.CapabilityEmployeeReadBasic)
		grant(access, "employee:read-pii", security.CapabilityEmployeeReadIdentity)
		grant(access, "employee:write-pii", security.CapabilityEmployeeCreate, security.CapabilityEmployeeWriteIdentity)
		grant(access, "employee:write-basic", security.CapabilityEmployeeUpdatePersonal)
		grant(access, "employment:read", security.CapabilityEmploymentRead, security.CapabilityAssignmentRead)
		grant(access, "employment:write", security.CapabilityEmploymentStart, security.CapabilityEmploymentRejoin, security.CapabilityEmploymentResign, security.CapabilityAssignmentWrite)
		grant(access, "file:read", security.CapabilityFileBatchRead, security.CapabilityFileArtifactRead, security.CapabilityFileTemplateRead)
		grant(access, "file:write", security.CapabilityFileBatchCreate, security.CapabilityFileBatchCancel)
	case "tax_admin":
		grant(access, "employee:read-basic", security.CapabilityEmployeeReadBasic)
		grant(access, "employee:read-pii", security.CapabilityEmployeeReadIdentity)
		grant(access, "employment:read", security.CapabilityEmploymentRead)
		grant(access, "tax:read", security.CapabilityTaxProfileRead)
		grant(access, "tax:write", security.CapabilityTaxProfileWrite)
	case "payroll_admin":
		grant(access, "payroll:read", security.CapabilityPayrollPeriodRead, security.CapabilityPayrollResultRead)
		grant(access, "payroll:write", security.CapabilityPayrollPeriodWrite, security.CapabilityPayrollResultWrite, security.CapabilityPayrollFinalize)
		grant(access, "employee:read-basic", security.CapabilityEmployeeReadBasic)
		grant(access, "employment:read", security.CapabilityEmploymentRead)
		grant(access, "file:read", security.CapabilityFileBatchRead, security.CapabilityFileArtifactRead, security.CapabilityFileTemplateRead)
		grant(access, "file:write", security.CapabilityFileBatchCreate, security.CapabilityFileBatchCancel)
		grant(access, "file:commit", security.CapabilityFileBatchCommit)
	}
}

func grant(access *Access, scope string, capabilities ...string) {
	access.Scopes[scope] = struct{}{}
	for _, capability := range capabilities {
		access.Capabilities[capability] = struct{}{}
	}
}
