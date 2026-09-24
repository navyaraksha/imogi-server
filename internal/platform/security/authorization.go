package security

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
)

const (
	CapabilityPlatformTenantRead      = "platform.tenant.read"
	CapabilityPlatformTenantCreate    = "platform.tenant.create"
	CapabilityPlatformTenantUpdate    = "platform.tenant.update"
	CapabilityPlatformTenantActivate  = "platform.tenant.activate"
	CapabilityPlatformTenantSuspend   = "platform.tenant.suspend"
	CapabilityPlatformTenantArchive   = "platform.tenant.archive"
	CapabilityPlatformUserRead        = "platform.user.read"
	CapabilityPlatformUserCreate      = "platform.user.create"
	CapabilityPlatformUserBlock       = "platform.user.block"
	CapabilityPlatformMembershipRead  = "platform.membership.read"
	CapabilityPlatformMembershipWrite = "platform.membership.write"
	CapabilityTenantRead              = "tenant.read"
	CapabilityTenantUpdate            = "tenant.update"
	CapabilityCompanyRead             = "company.read"
	CapabilityCompanyCreate           = "company.create"
	CapabilityCompanyUpdate           = "company.update"
	CapabilityCompanyActivate         = "company.activate"
	CapabilityCompanySuspend          = "company.suspend"
	CapabilityCompanyArchive          = "company.archive"
	CapabilityOrganizationUnitRead    = "organization.unit.read"
	CapabilityOrganizationUnitCreate  = "organization.unit.create"
	CapabilityOrganizationUnitUpdate  = "organization.unit.update"
	CapabilityOrganizationUnitArchive = "organization.unit.archive"
	CapabilityEmployeeReadBasic       = "employee.read.basic"
	CapabilityEmployeeReadIdentity    = "employee.read.identity"
	CapabilityEmployeeCreate          = "employee.create"
	CapabilityEmployeeWriteIdentity   = "employee.write.identity"
	CapabilityEmployeeUpdatePersonal  = "employee.update.personal"
	CapabilityEmploymentRead          = "employment.read"
	CapabilityEmploymentStart         = "employment.start"
	CapabilityEmploymentRejoin        = "employment.rejoin"
	CapabilityEmploymentResign        = "employment.resign"
	CapabilityAssignmentRead          = "assignment.read"
	CapabilityAssignmentWrite         = "assignment.write"
	CapabilityTaxProfileRead          = "tax_profile.read"
	CapabilityTaxProfileWrite         = "tax_profile.write"
	CapabilityPayrollPeriodRead       = "payroll.period.read"
	CapabilityPayrollPeriodWrite      = "payroll.period.write"
	CapabilityPayrollResultRead       = "payroll.result.read"
	CapabilityPayrollResultWrite      = "payroll.result.write"
	CapabilityPayrollFinalize         = "payroll.finalize"
	CapabilityFileBatchRead           = "file_batch.read"
	CapabilityFileBatchCreate         = "file_batch.create"
	CapabilityFileBatchCommit         = "file_batch.commit"
	CapabilityFileBatchCancel         = "file_batch.cancel"
	CapabilityFileArtifactRead        = "file_artifact.read"
	CapabilityFileTemplateRead        = "file_template.read"
	CapabilityFileTemplateWrite       = "file_template.write"
	CapabilityBackgroundJobRead       = "background_job.read"
	CapabilityMembershipRead          = "membership.read"
	CapabilityMembershipWrite         = "membership.write"
)

var (
	ErrUnauthenticated         = errors.New("authentication required")
	ErrForbidden               = errors.New("permission denied")
	ErrTenantSelectionRequired = errors.New("active tenant selection required")
)

type Principal struct {
	UserID        uuid.UUID
	Subject       string
	Email         string
	DisplayName   string
	TenantID      uuid.UUID
	PlatformAdmin bool
	Tenants       []TenantAccess
	Scopes        map[string]struct{}
	Capabilities  map[string]struct{}
	CompanyIDs    map[uuid.UUID]struct{}
}

type TenantAccess struct {
	TenantID uuid.UUID
	Slug     string
	Name     string
	Status   string
	RoleCode string
}

func CurrentUserID(ctx context.Context) (identitydomain.UserID, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok || principal.Subject == "" {
		return identitydomain.UserID(uuid.Nil), ErrUnauthenticated
	}
	if principal.UserID == uuid.Nil {
		return identitydomain.UserID(uuid.Nil), fmt.Errorf("%w: user identity is missing", ErrForbidden)
	}
	return identitydomain.UserID(principal.UserID), nil
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

type Authorizer interface {
	Require(ctx context.Context, capability string) error
	RequireCompany(ctx context.Context, companyID uuid.UUID) error
}

// TenantScope resolves the active tenant from trusted authentication context.
// It deliberately has no method accepting a client-supplied tenant ID.
type TenantScope interface {
	ActiveTenant(context.Context) (uuid.UUID, error)
}

type ContextAuthorizer struct{}

func (ContextAuthorizer) Require(ctx context.Context, capability string) error {
	principal, ok := PrincipalFromContext(ctx)
	if !ok || principal.Subject == "" {
		return ErrUnauthenticated
	}
	if _, ok := principal.Capabilities[capability]; !ok {
		return fmt.Errorf("%w: missing capability %s", ErrForbidden, capability)
	}
	return nil
}

func (ContextAuthorizer) RequireCompany(ctx context.Context, companyID uuid.UUID) error {
	principal, ok := PrincipalFromContext(ctx)
	if !ok || principal.Subject == "" {
		return ErrUnauthenticated
	}
	if _, ok := principal.CompanyIDs[companyID]; !ok {
		return fmt.Errorf("%w: company access denied", ErrForbidden)
	}
	return nil
}

func (ContextAuthorizer) ActiveTenant(ctx context.Context) (uuid.UUID, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok || principal.Subject == "" {
		return uuid.Nil, ErrUnauthenticated
	}
	if principal.TenantID == uuid.Nil {
		return uuid.Nil, ErrTenantSelectionRequired
	}
	return principal.TenantID, nil
}

type AllowAll struct{}

func (AllowAll) Require(context.Context, string) error           { return nil }
func (AllowAll) RequireCompany(context.Context, uuid.UUID) error { return nil }
func (AllowAll) ActiveTenant(context.Context) (uuid.UUID, error) { return uuid.Nil, nil }

func ActiveTenant(ctx context.Context, authorizer Authorizer) (uuid.UUID, error) {
	scope, ok := authorizer.(TenantScope)
	if !ok {
		return uuid.Nil, nil
	}
	return scope.ActiveTenant(ctx)
}
