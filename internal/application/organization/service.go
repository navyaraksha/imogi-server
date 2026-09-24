package organization

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	domain "github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type Service struct {
	repository Repository
	authorizer security.Authorizer
	clock      clock.Clock
}

func NewService(repository Repository, authorizer security.Authorizer, systemClock clock.Clock) (*Service, error) {
	if repository == nil || authorizer == nil || systemClock == nil {
		return nil, errors.New("organization service dependencies are required")
	}
	return &Service{repository: repository, authorizer: authorizer, clock: systemClock}, nil
}

func (s *Service) CreateTenant(ctx context.Context, input CreateTenantInput) (domain.Tenant, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformTenantCreate); err != nil {
		return domain.Tenant{}, err
	}
	id, err := domain.NewTenantID()
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("generate tenant id: %w", err)
	}
	entity, err := domain.NewTenant(id, input.Slug, input.Name, s.clock.Now())
	if err != nil {
		return domain.Tenant{}, err
	}
	if userID, err := security.CurrentUserID(ctx); err == nil {
		if provisioner, ok := s.repository.(TenantProvisioner); ok {
			return provisioner.CreateTenantWithAdmin(ctx, entity, userID)
		}
	}
	return s.repository.CreateTenant(ctx, entity)
}

func (s *Service) ListTenants(ctx context.Context, cursor string, limit int) ([]domain.Tenant, string, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformTenantRead); err != nil {
		return nil, "", err
	}
	var cursorID *domain.TenantID
	if cursor != "" {
		id, err := decodeTenantCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		cursorID = &id
	}
	limit = normalizeLimit(limit)
	items, err := s.repository.ListTenants(ctx, cursorID, limit+1)
	if err != nil {
		return nil, "", err
	}
	return paginateTenants(items, limit)
}

func (s *Service) GetTenant(ctx context.Context, id domain.TenantID) (domain.Tenant, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformTenantRead); err != nil {
		return domain.Tenant{}, err
	}
	return s.repository.GetTenant(ctx, id)
}

func (s *Service) UpdateTenant(ctx context.Context, id domain.TenantID, input UpdateTenantInput) (domain.Tenant, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformTenantUpdate); err != nil {
		return domain.Tenant{}, err
	}
	entity, err := s.repository.GetTenant(ctx, id)
	if err != nil {
		return domain.Tenant{}, err
	}
	if err := entity.UpdateName(input.Name, s.clock.Now()); err != nil {
		return domain.Tenant{}, err
	}
	return s.repository.UpdateTenant(ctx, entity)
}

func (s *Service) TransitionTenant(ctx context.Context, id domain.TenantID, status domain.TenantStatus) (domain.Tenant, error) {
	capability := tenantTransitionCapability(status)
	if err := s.authorizer.Require(ctx, capability); err != nil {
		return domain.Tenant{}, err
	}
	entity, err := s.repository.GetTenant(ctx, id)
	if err != nil {
		return domain.Tenant{}, err
	}
	if err := entity.Transition(status, s.clock.Now()); err != nil {
		return domain.Tenant{}, err
	}
	return s.repository.TransitionTenant(ctx, entity)
}

func (s *Service) GetCurrentTenant(ctx context.Context) (domain.Tenant, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityTenantRead); err != nil {
		return domain.Tenant{}, err
	}
	id, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return domain.Tenant{}, err
	}
	return s.repository.GetTenant(ctx, domain.TenantID(id))
}

func (s *Service) UpdateCurrentTenant(ctx context.Context, input UpdateTenantInput) (domain.Tenant, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityTenantUpdate); err != nil {
		return domain.Tenant{}, err
	}
	id, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return domain.Tenant{}, err
	}
	entity, err := s.repository.GetTenant(ctx, domain.TenantID(id))
	if err != nil {
		return domain.Tenant{}, err
	}
	if err := entity.UpdateName(input.Name, s.clock.Now()); err != nil {
		return domain.Tenant{}, err
	}
	return s.repository.UpdateTenant(ctx, entity)
}

func (s *Service) CreateCompany(ctx context.Context, input CreateCompanyInput) (domain.Company, error) {
	tenantID, err := s.activeTenant(ctx)
	if err != nil {
		return domain.Company{}, err
	}
	if err := s.authorizer.Require(ctx, security.CapabilityCompanyCreate); err != nil {
		return domain.Company{}, err
	}
	id, err := domain.NewCompanyID()
	if err != nil {
		return domain.Company{}, fmt.Errorf("generate company id: %w", err)
	}
	entity, err := domain.NewCompany(id, tenantID, input.Code, input.LegalName, input.DisplayName, s.clock.Now())
	if err != nil {
		return domain.Company{}, err
	}
	return s.repository.CreateCompany(ctx, entity)
}

func (s *Service) ListCompanies(ctx context.Context, cursor string, limit int) ([]domain.Company, string, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityCompanyRead); err != nil {
		return nil, "", err
	}
	tenantID, err := s.activeTenant(ctx)
	if err != nil {
		return nil, "", err
	}
	var cursorID *domain.CompanyID
	if cursor != "" {
		id, err := decodeCompanyCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		cursorID = &id
	}
	limit = normalizeLimit(limit)
	items, err := s.repository.ListCompanies(ctx, tenantID, cursorID, limit+1)
	if err != nil {
		return nil, "", err
	}
	if len(items) <= limit {
		return items, "", nil
	}
	return items[:limit], encodeCompanyCursor(items[limit-1].ID), nil
}

func (s *Service) GetCompany(ctx context.Context, id domain.CompanyID) (domain.Company, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityCompanyRead); err != nil {
		return domain.Company{}, err
	}
	entity, err := s.repository.GetCompany(ctx, id)
	if err != nil {
		return domain.Company{}, err
	}
	if err := s.requireCompany(ctx, entity); err != nil {
		return domain.Company{}, err
	}
	return entity, nil
}

func (s *Service) UpdateCompany(ctx context.Context, id domain.CompanyID, input UpdateCompanyInput) (domain.Company, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityCompanyUpdate); err != nil {
		return domain.Company{}, err
	}
	entity, err := s.GetCompany(ctx, id)
	if err != nil {
		return domain.Company{}, err
	}
	if err := entity.Update(input.Code, input.LegalName, input.DisplayName, s.clock.Now()); err != nil {
		return domain.Company{}, err
	}
	return s.repository.UpdateCompany(ctx, entity)
}

func (s *Service) TransitionCompany(ctx context.Context, id domain.CompanyID, status domain.CompanyStatus) (domain.Company, error) {
	capability := companyTransitionCapability(status)
	if err := s.authorizer.Require(ctx, capability); err != nil {
		return domain.Company{}, err
	}
	entity, err := s.repository.GetCompany(ctx, id)
	if err != nil {
		return domain.Company{}, err
	}
	if err := s.requireCompany(ctx, entity); err != nil {
		return domain.Company{}, err
	}
	if err := entity.Transition(status, s.clock.Now()); err != nil {
		return domain.Company{}, err
	}
	return s.repository.TransitionCompany(ctx, entity)
}

func (s *Service) CreateUnit(ctx context.Context, input CreateUnitInput) (domain.Unit, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityOrganizationUnitCreate); err != nil {
		return domain.Unit{}, err
	}
	tenantID, err := s.activeTenant(ctx)
	if err != nil {
		return domain.Unit{}, err
	}
	if err := s.authorizer.RequireCompany(ctx, input.CompanyID.UUID()); err != nil {
		return domain.Unit{}, err
	}
	id, err := domain.NewUnitID()
	if err != nil {
		return domain.Unit{}, fmt.Errorf("generate unit id: %w", err)
	}
	entity, err := domain.NewUnit(id, tenantID, input.CompanyID, input.Type, input.Code, input.Name, s.clock.Now())
	if err != nil {
		return domain.Unit{}, err
	}
	return s.repository.CreateUnit(ctx, entity)
}

func (s *Service) ListUnits(ctx context.Context, unitType domain.UnitType, cursor string, companyID *domain.CompanyID, limit int) ([]domain.Unit, string, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityOrganizationUnitRead); err != nil {
		return nil, "", err
	}
	tenantID, err := s.activeTenant(ctx)
	if err != nil {
		return nil, "", err
	}
	if companyID != nil {
		if err := s.authorizer.RequireCompany(ctx, companyID.UUID()); err != nil {
			return nil, "", err
		}
	}
	var cursorID *domain.UnitID
	if cursor != "" {
		id, err := decodeUnitCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		cursorID = &id
	}
	limit = normalizeLimit(limit)
	items, err := s.repository.ListUnits(ctx, tenantID, unitType, cursorID, companyID, limit+1)
	if err != nil {
		return nil, "", err
	}
	if len(items) <= limit {
		return items, "", nil
	}
	return items[:limit], encodeUnitCursor(items[limit-1].ID), nil
}

func (s *Service) GetUnit(ctx context.Context, id domain.UnitID, expectedType domain.UnitType) (domain.Unit, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityOrganizationUnitRead); err != nil {
		return domain.Unit{}, err
	}
	entity, err := s.repository.GetUnit(ctx, id)
	if err != nil {
		return domain.Unit{}, err
	}
	if entity.Type != expectedType {
		return domain.Unit{}, domain.ErrUnitTypeMismatch
	}
	if err := s.requireUnit(ctx, entity); err != nil {
		return domain.Unit{}, err
	}
	return entity, nil
}

func (s *Service) UpdateUnit(ctx context.Context, id domain.UnitID, expectedType domain.UnitType, input UpdateUnitInput) (domain.Unit, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityOrganizationUnitUpdate); err != nil {
		return domain.Unit{}, err
	}
	entity, err := s.GetUnit(ctx, id, expectedType)
	if err != nil {
		return domain.Unit{}, err
	}
	if err := entity.Update(input.Code, input.Name, s.clock.Now()); err != nil {
		return domain.Unit{}, err
	}
	return s.repository.UpdateUnit(ctx, entity)
}

func (s *Service) ArchiveUnit(ctx context.Context, id domain.UnitID, expectedType domain.UnitType) (domain.Unit, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityOrganizationUnitArchive); err != nil {
		return domain.Unit{}, err
	}
	entity, err := s.GetUnit(ctx, id, expectedType)
	if err != nil {
		return domain.Unit{}, err
	}
	if err := entity.Archive(s.clock.Now()); err != nil {
		return domain.Unit{}, err
	}
	return s.repository.ArchiveUnit(ctx, entity)
}

func (s *Service) activeTenant(ctx context.Context) (domain.TenantID, error) {
	id, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return domain.TenantID{}, err
	}
	return domain.TenantID(id), nil
}

func (s *Service) requireCompany(ctx context.Context, company domain.Company) error {
	tenant, err := s.activeTenant(ctx)
	if err != nil {
		return err
	}
	if tenant != company.TenantID {
		return security.ErrForbidden
	}
	return s.authorizer.RequireCompany(ctx, company.ID.UUID())
}

func (s *Service) requireUnit(ctx context.Context, unit domain.Unit) error {
	tenant, err := s.activeTenant(ctx)
	if err != nil {
		return err
	}
	if tenant != unit.TenantID {
		return security.ErrForbidden
	}
	return s.authorizer.RequireCompany(ctx, unit.CompanyID.UUID())
}

func normalizeLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}

func tenantTransitionCapability(status domain.TenantStatus) string {
	switch status {
	case domain.TenantActive:
		return security.CapabilityPlatformTenantActivate
	case domain.TenantSuspended:
		return security.CapabilityPlatformTenantSuspend
	case domain.TenantArchived:
		return security.CapabilityPlatformTenantArchive
	default:
		return security.CapabilityPlatformTenantUpdate
	}
}

func companyTransitionCapability(status domain.CompanyStatus) string {
	switch status {
	case domain.CompanyActive:
		return security.CapabilityCompanyActivate
	case domain.CompanySuspended:
		return security.CapabilityCompanySuspend
	case domain.CompanyArchived:
		return security.CapabilityCompanyArchive
	default:
		return security.CapabilityCompanyUpdate
	}
}

func encodeCompanyCursor(id domain.CompanyID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String()))
}
func decodeCompanyCursor(value string) (domain.CompanyID, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return domain.CompanyID{}, err
	}
	return domain.ParseCompanyID(string(b))
}
func encodeTenantCursor(id domain.TenantID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String()))
}
func decodeTenantCursor(value string) (domain.TenantID, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return domain.TenantID{}, err
	}
	return domain.ParseTenantID(string(b))
}
func encodeUnitCursor(id domain.UnitID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String()))
}
func decodeUnitCursor(value string) (domain.UnitID, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return domain.UnitID{}, err
	}
	return domain.ParseUnitID(string(b))
}
func paginateTenants(items []domain.Tenant, limit int) ([]domain.Tenant, string, error) {
	if len(items) <= limit {
		return items, "", nil
	}
	return items[:limit], encodeTenantCursor(items[limit-1].ID), nil
}
