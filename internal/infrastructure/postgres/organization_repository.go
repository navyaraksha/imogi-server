package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	apporganization "github.com/navyaraksha/imogi/internal/application/organization"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	domain "github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres/sqlc"
)

type OrganizationRepository struct {
	queries *sqlc.Queries
	pool    *pgxpool.Pool
}

var _ apporganization.Repository = (*OrganizationRepository)(nil)
var _ apporganization.TenantProvisioner = (*OrganizationRepository)(nil)

func NewOrganizationRepository(pool *pgxpool.Pool) (*OrganizationRepository, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	return &OrganizationRepository{
		queries: sqlc.New(pool),
		pool:    pool,
	}, nil
}

func (r *OrganizationRepository) CreateTenant(ctx context.Context, entity domain.Tenant) (domain.Tenant, error) {
	row, err := r.queries.CreateTenant(ctx, sqlc.CreateTenantParams{
		ID:   entity.ID.UUID(),
		Slug: entity.Slug,
		Name: entity.Name,
	})
	if err != nil {
		return domain.Tenant{}, mapOrganizationDatabaseError(err)
	}
	return mapTenant(row), nil
}

func (r *OrganizationRepository) CreateTenantWithAdmin(ctx context.Context, entity domain.Tenant, userID identitydomain.UserID) (domain.Tenant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("begin tenant provisioning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := r.queries.WithTx(tx)
	row, err := queries.CreateTenant(ctx, sqlc.CreateTenantParams{
		ID:   entity.ID.UUID(),
		Slug: entity.Slug,
		Name: entity.Name,
	})
	if err != nil {
		return domain.Tenant{}, mapOrganizationDatabaseError(err)
	}
	membershipID, err := uuid.NewV7()
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("generate tenant administrator membership id: %w", err)
	}
	if _, err := queries.CreateTenantMembership(ctx, sqlc.CreateTenantMembershipParams{
		ID:       membershipID,
		UserID:   userID.UUID(),
		TenantID: entity.ID.UUID(),
		RoleCode: "tenant_admin",
	}); err != nil {
		return domain.Tenant{}, fmt.Errorf("create tenant administrator membership: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Tenant{}, fmt.Errorf("commit tenant provisioning transaction: %w", err)
	}
	return mapTenant(row), nil
}

func (r *OrganizationRepository) GetTenant(ctx context.Context, id domain.TenantID) (domain.Tenant, error) {
	row, err := r.queries.GetTenant(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Tenant{}, domain.ErrTenantNotFound
	}
	if err != nil {
		return domain.Tenant{}, mapOrganizationDatabaseError(err)
	}
	return mapTenant(row), nil
}

func (r *OrganizationRepository) ListTenants(ctx context.Context, cursor *domain.TenantID, limit int) ([]domain.Tenant, error) {
	rows, err := r.queries.ListTenants(ctx, sqlc.ListTenantsParams{
		CursorID: tenantIDUUID(cursor),
		Limit:    int32(limit),
	})
	if err != nil {
		return nil, mapOrganizationDatabaseError(err)
	}
	items := make([]domain.Tenant, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapTenant(row))
	}
	return items, nil
}

func (r *OrganizationRepository) UpdateTenant(ctx context.Context, entity domain.Tenant) (domain.Tenant, error) {
	row, err := r.queries.UpdateTenant(ctx, sqlc.UpdateTenantParams{
		ID:   entity.ID.UUID(),
		Name: entity.Name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Tenant{}, domain.ErrTenantNotFound
	}
	if err != nil {
		return domain.Tenant{}, mapOrganizationDatabaseError(err)
	}
	return mapTenant(row), nil
}

func (r *OrganizationRepository) TransitionTenant(ctx context.Context, entity domain.Tenant) (domain.Tenant, error) {
	row, err := r.queries.TransitionTenant(ctx, sqlc.TransitionTenantParams{
		ID:     entity.ID.UUID(),
		Status: string(entity.Status),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Tenant{}, domain.ErrTenantNotFound
	}
	if err != nil {
		return domain.Tenant{}, mapOrganizationDatabaseError(err)
	}
	return mapTenant(row), nil
}

func (r *OrganizationRepository) CreateCompany(ctx context.Context, entity domain.Company) (domain.Company, error) {
	row, err := r.queries.CreateCompany(ctx, sqlc.CreateCompanyParams{
		ID:          entity.ID.UUID(),
		TenantID:    entity.TenantID.UUID(),
		Code:        entity.Code,
		LegalName:   entity.LegalName,
		DisplayName: entity.DisplayName,
	})
	if err != nil {
		return domain.Company{}, mapOrganizationDatabaseError(err)
	}
	return mapCompany(row), nil
}

func (r *OrganizationRepository) GetCompany(ctx context.Context, id domain.CompanyID) (domain.Company, error) {
	row, err := r.queries.GetCompany(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, domain.ErrCompanyNotFound
	}
	if err != nil {
		return domain.Company{}, mapOrganizationDatabaseError(err)
	}
	return mapCompany(row), nil
}

func (r *OrganizationRepository) ListCompanies(ctx context.Context, tenantID domain.TenantID, cursor *domain.CompanyID, limit int) ([]domain.Company, error) {
	rows, err := r.queries.ListCompanies(ctx, sqlc.ListCompaniesParams{
		TenantID: tenantID.UUID(),
		CursorID: companyIDUUID(cursor),
		Limit:    int32(limit),
	})
	if err != nil {
		return nil, mapOrganizationDatabaseError(err)
	}
	items := make([]domain.Company, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapCompany(row))
	}
	return items, nil
}

func (r *OrganizationRepository) UpdateCompany(ctx context.Context, entity domain.Company) (domain.Company, error) {
	row, err := r.queries.UpdateCompany(ctx, sqlc.UpdateCompanyParams{
		ID:          entity.ID.UUID(),
		Code:        entity.Code,
		LegalName:   entity.LegalName,
		DisplayName: entity.DisplayName,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, domain.ErrCompanyNotFound
	}
	if err != nil {
		return domain.Company{}, mapOrganizationDatabaseError(err)
	}
	return mapCompany(row), nil
}

func (r *OrganizationRepository) TransitionCompany(ctx context.Context, entity domain.Company) (domain.Company, error) {
	row, err := r.queries.TransitionCompany(ctx, sqlc.TransitionCompanyParams{
		ID:     entity.ID.UUID(),
		Status: string(entity.Status),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, domain.ErrCompanyNotFound
	}
	if err != nil {
		return domain.Company{}, mapOrganizationDatabaseError(err)
	}
	return mapCompany(row), nil
}

func (r *OrganizationRepository) CreateUnit(ctx context.Context, entity domain.Unit) (domain.Unit, error) {
	row, err := r.queries.CreateUnit(ctx, sqlc.CreateUnitParams{
		ID:        entity.ID.UUID(),
		TenantID:  entity.TenantID.UUID(),
		CompanyID: entity.CompanyID.UUID(),
		UnitType:  string(entity.Type),
		Code:      entity.Code,
		Name:      entity.Name,
	})
	if err != nil {
		return domain.Unit{}, mapOrganizationDatabaseError(err)
	}
	return mapUnit(row), nil
}

func (r *OrganizationRepository) GetUnit(ctx context.Context, id domain.UnitID) (domain.Unit, error) {
	row, err := r.queries.GetUnit(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Unit{}, domain.ErrUnitNotFound
	}
	if err != nil {
		return domain.Unit{}, mapOrganizationDatabaseError(err)
	}
	return mapUnit(row), nil
}

func (r *OrganizationRepository) ListUnits(ctx context.Context, tenantID domain.TenantID, unitType domain.UnitType, cursor *domain.UnitID, companyID *domain.CompanyID, limit int) ([]domain.Unit, error) {
	rows, err := r.queries.ListUnits(ctx, sqlc.ListUnitsParams{
		TenantID:  tenantID.UUID(),
		UnitType:  string(unitType),
		CursorID:  unitIDUUID(cursor),
		CompanyID: companyIDUUID(companyID),
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, mapOrganizationDatabaseError(err)
	}
	units := make([]domain.Unit, 0, len(rows))
	for _, row := range rows {
		units = append(units, mapUnit(row))
	}
	return units, nil
}

func (r *OrganizationRepository) UpdateUnit(ctx context.Context, entity domain.Unit) (domain.Unit, error) {
	row, err := r.queries.UpdateUnit(ctx, sqlc.UpdateUnitParams{
		ID:   entity.ID.UUID(),
		Code: entity.Code,
		Name: entity.Name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Unit{}, domain.ErrUnitNotFound
	}
	if err != nil {
		return domain.Unit{}, mapOrganizationDatabaseError(err)
	}
	return mapUnit(row), nil
}

func (r *OrganizationRepository) ArchiveUnit(ctx context.Context, entity domain.Unit) (domain.Unit, error) {
	row, err := r.queries.ArchiveUnit(ctx, entity.ID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Unit{}, domain.ErrUnitNotFound
	}
	if err != nil {
		return domain.Unit{}, mapOrganizationDatabaseError(err)
	}
	return mapUnit(row), nil
}

func mapTenant(row sqlc.PlatformTenant) domain.Tenant {
	return domain.Tenant{
		ID:        domain.TenantID(row.ID),
		Slug:      row.Slug,
		Name:      row.Name,
		Status:    domain.TenantStatus(row.Status),
		CreatedAt: timestamp(row.CreatedAt),
		UpdatedAt: timestamp(row.UpdatedAt),
	}
}
func mapCompany(row sqlc.OrganizationCompany) domain.Company {
	return domain.Company{
		ID:          domain.CompanyID(row.ID),
		TenantID:    domain.TenantID(row.TenantID),
		Code:        row.Code,
		LegalName:   row.LegalName,
		DisplayName: row.DisplayName,
		Status:      domain.CompanyStatus(row.Status),
		CreatedAt:   timestamp(row.CreatedAt),
		UpdatedAt:   timestamp(row.UpdatedAt),
	}
}
func mapUnit(row sqlc.OrganizationUnit) domain.Unit {
	return domain.Unit{
		ID:        domain.UnitID(row.ID),
		TenantID:  domain.TenantID(row.TenantID),
		CompanyID: domain.CompanyID(row.CompanyID),
		Type:      domain.UnitType(row.UnitType),
		Code:      row.Code,
		Name:      row.Name,
		Status:    domain.UnitStatus(row.Status),
		CreatedAt: timestamp(row.CreatedAt),
		UpdatedAt: timestamp(row.UpdatedAt),
	}
}

func mapOrganizationDatabaseError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case "tenants_slug_uq":
		return fmt.Errorf("tenant slug already exists: %w", err)
	case "companies_tenant_code_uq":
		return fmt.Errorf("company code already exists: %w", err)
	case "units_company_type_code_uq":
		return fmt.Errorf("organization unit code already exists: %w", err)
	default:
		return err
	}
}
