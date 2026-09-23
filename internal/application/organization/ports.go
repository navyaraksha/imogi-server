package organization

import (
	"context"

	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	domain "github.com/navyaraksha/imogi/internal/domain/organization"
)

type Repository interface {
	CreateTenant(context.Context, domain.Tenant) (domain.Tenant, error)
	GetTenant(context.Context, domain.TenantID) (domain.Tenant, error)
	ListTenants(context.Context, *domain.TenantID, int) ([]domain.Tenant, error)
	UpdateTenant(context.Context, domain.Tenant) (domain.Tenant, error)
	TransitionTenant(context.Context, domain.Tenant) (domain.Tenant, error)

	CreateCompany(context.Context, domain.Company) (domain.Company, error)
	GetCompany(context.Context, domain.CompanyID) (domain.Company, error)
	ListCompanies(context.Context, domain.TenantID, *domain.CompanyID, int) ([]domain.Company, error)
	UpdateCompany(context.Context, domain.Company) (domain.Company, error)
	TransitionCompany(context.Context, domain.Company) (domain.Company, error)

	CreateUnit(context.Context, domain.Unit) (domain.Unit, error)
	GetUnit(context.Context, domain.UnitID) (domain.Unit, error)
	ListUnits(context.Context, domain.TenantID, domain.UnitType, *domain.UnitID, *domain.CompanyID, int) ([]domain.Unit, error)
	UpdateUnit(context.Context, domain.Unit) (domain.Unit, error)
	ArchiveUnit(context.Context, domain.Unit) (domain.Unit, error)
}

// TenantProvisioner creates a tenant and its initial administrator atomically.
// The application service uses this capability when the authenticated creator
// is available; repositories that do not support it remain useful in tests or
// non-persistent adapters through the base Repository contract.
type TenantProvisioner interface {
	CreateTenantWithAdmin(context.Context, domain.Tenant, identitydomain.UserID) (domain.Tenant, error)
}

type CreateTenantInput struct {
	Slug string
	Name string
}

type UpdateTenantInput struct {
	Name string
}

type CreateCompanyInput struct {
	Code        string
	LegalName   string
	DisplayName string
}

type UpdateCompanyInput struct {
	Code        string
	LegalName   string
	DisplayName string
}
type CreateUnitInput struct {
	CompanyID domain.CompanyID
	Type      domain.UnitType
	Code      string
	Name      string
}
type UpdateUnitInput struct {
	Code string
	Name string
}
