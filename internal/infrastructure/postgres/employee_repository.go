package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	appemployee "github.com/navyaraksha/imogi/internal/application/employee"
	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	domain "github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres/sqlc"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

// Repository is the PostgreSQL adapter for the employee application boundary.
// Transactions are opened by application use cases through WithinTransaction.
type Repository struct {
	*repositoryOps
	pool *pgxpool.Pool
}

type repositoryOps struct {
	queries   *sqlc.Queries
	protector security.SensitiveDataProtector
}

type transactionRepository struct {
	*repositoryOps
}

var _ appemployee.Repository = (*Repository)(nil)
var _ appemployee.EmployeeNumberHistoryRepository = (*Repository)(nil)

// NewRepository creates the employee repository. Sensitive identity values are
// encrypted before they leave this adapter and decrypted only for authorized
// application use cases.
func NewRepository(pool *pgxpool.Pool, protector security.SensitiveDataProtector) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	if protector == nil {
		return nil, errors.New("sensitive data protector is required")
	}
	return &Repository{
		pool: pool,
		repositoryOps: &repositoryOps{
			queries:   sqlc.New(pool),
			protector: protector,
		},
	}, nil
}

func (r *Repository) WithinTransaction(ctx context.Context, fn func(appemployee.Transaction) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin employee transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transactional := &transactionRepository{
		repositoryOps: &repositoryOps{
			queries:   r.queries.WithTx(tx),
			protector: r.protector,
		},
	}
	if err := fn(transactional); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit employee transaction: %w", err)
	}
	return nil
}

// FindIdentityCandidates is used by file validation. It deliberately returns
// only matching identity metadata; NIK and other protected fields never leave
// the encrypted employee repository for this purpose.
func (r *Repository) FindIdentityCandidates(ctx context.Context, tenantID, companyID uuid.UUID, nik, name, employeeNumber string) ([]appfilebatch.IdentityCandidate, error) {
	var nikLookup []byte
	if strings.TrimSpace(nik) != "" {
		nikLookup = r.protector.LookupHash(nik)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT e.id,
		       e.full_name,
		       COALESCE(number_match.employee_number, e.employee_number, ''),
		       number_match.employment_id,
		       ($3::bytea IS NOT NULL AND e.nik_lookup_hash = $3::bytea) AS nik_match,
		       COALESCE(e.birth_place, ''), COALESCE(to_char(e.birth_date, 'YYYY-MM-DD'), ''),
		       COALESCE(e.gender, ''), COALESCE(e.email, ''), COALESCE(e.phone, ''), COALESCE(e.address, '')
		FROM employee.employees e
		LEFT JOIN LATERAL (
			SELECT h.employee_number, h.employment_id
			FROM employee.employee_number_history h
			WHERE h.tenant_id = e.tenant_id
			  AND h.company_id = e.company_id
			  AND h.employee_id = e.id
			  AND $5 <> ''
			  AND lower(regexp_replace(h.employee_number, '[^[:alnum:]]', '', 'g')) = $5
			ORDER BY h.effective_from DESC, h.id DESC
			LIMIT 1
		) number_match
		WHERE e.tenant_id = $1
		  AND e.company_id = $2
		  AND (
			($3::bytea IS NOT NULL AND e.nik_lookup_hash = $3::bytea)
			OR lower(regexp_replace(e.full_name, '[^[:alnum:]]', '', 'g')) = $4
			OR number_match.employee_number IS NOT NULL
			OR lower(regexp_replace(COALESCE(e.employee_number, ''), '[^[:alnum:]]', '', 'g')) = $5
		  )
		ORDER BY e.id
	`, tenantID, companyID, nikLookup, normalizeIdentityLookup(name), normalizeIdentityLookup(employeeNumber))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]appfilebatch.IdentityCandidate, 0)
	for rows.Next() {
		var candidate appfilebatch.IdentityCandidate
		var birthPlace, birthDate, gender, email, phone, address string
		if err := rows.Scan(&candidate.EmployeeID, &candidate.FullName, &candidate.EmployeeNumber, &candidate.EmploymentID, &candidate.NIKMatch, &birthPlace, &birthDate, &gender, &email, &phone, &address); err != nil {
			return nil, err
		}
		candidate.PersonalFields = map[string]string{"birth_place": birthPlace, "birth_date": birthDate, "gender": gender, "email": email, "phone": phone, "address": address}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *Repository) CreateEmployeeNumberHistory(ctx context.Context, history domain.EmployeeNumberHistory) (domain.EmployeeNumberHistory, error) {
	return r.repositoryOps.CreateEmployeeNumberHistory(ctx, history)
}

func (r *repositoryOps) CreateEmployeeNumberHistory(ctx context.Context, history domain.EmployeeNumberHistory) (domain.EmployeeNumberHistory, error) {
	var employmentID *uuid.UUID
	if history.EmploymentID != nil {
		value := history.EmploymentID.UUID()
		employmentID = &value
	}
	var supersedesID *uuid.UUID
	if history.SupersedesID != nil {
		value := history.SupersedesID.UUID()
		supersedesID = &value
	}
	row, err := r.queries.CreateEmployeeNumberHistory(ctx, sqlc.CreateEmployeeNumberHistoryParams{
		ID: history.ID.UUID(), TenantID: history.TenantID.UUID(), CompanyID: history.CompanyID.UUID(),
		EmployeeID: history.EmployeeID.UUID(), EmploymentID: employmentID, EmployeeNumber: history.EmployeeNumber,
		NumberType: string(history.NumberType), EffectiveFrom: toPGDate(&history.EffectiveFrom),
		EffectiveTo: toPGDate(history.EffectiveTo), Source: string(history.Source), SourceBatchID: history.SourceBatchID,
		SupersedesID: supersedesID, CorrectionReason: history.CorrectionReason, CreatedBy: history.CreatedBy,
	})
	if err != nil {
		return domain.EmployeeNumberHistory{}, mapDatabaseError(err)
	}
	return mapEmployeeNumberHistory(row), nil
}

func (r *repositoryOps) CloseOpenEmployeeNumberHistory(ctx context.Context, employeeID domain.EmployeeID, effectiveFrom, effectiveTo time.Time) error {
	return r.queries.CloseOpenEmployeeNumberHistory(ctx, sqlc.CloseOpenEmployeeNumberHistoryParams{EmployeeID: employeeID.UUID(), EffectiveFrom: toPGDate(&effectiveFrom), EffectiveTo: toPGDate(&effectiveTo)})
}

func (r *repositoryOps) UpdateEmployeeNumberProjection(ctx context.Context, employeeID domain.EmployeeID, number *string, status string) error {
	_, err := r.queries.UpdateEmployeeNumberProjection(ctx, sqlc.UpdateEmployeeNumberProjectionParams{ID: employeeID.UUID(), EmployeeNumber: number, EmployeeNumberStatus: status})
	return mapDatabaseError(err)
}

func (r *Repository) ListEmployeeNumberHistory(ctx context.Context, employeeID domain.EmployeeID) ([]domain.EmployeeNumberHistory, error) {
	rows, err := r.queries.ListEmployeeNumberHistory(ctx, employeeID.UUID())
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	result := make([]domain.EmployeeNumberHistory, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapEmployeeNumberHistory(row))
	}
	return result, nil
}

func normalizeIdentityLookup(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func (r *repositoryOps) CreateEmployee(ctx context.Context, entity domain.Employee) (domain.Employee, error) {
	nikCiphertext, err := r.protector.Encrypt(entity.NIK.String())
	if err != nil {
		return domain.Employee{}, fmt.Errorf("encrypt employee NIK: %w", err)
	}
	row, err := r.queries.CreateEmployee(ctx, sqlc.CreateEmployeeParams{
		ID:             entity.ID.UUID(),
		TenantID:       entity.TenantID.UUID(),
		CompanyID:      entity.CompanyID.UUID(),
		EmployeeNumber: entity.EmployeeNumber,
		NikCiphertext:  nikCiphertext,
		NikLookupHash:  r.protector.LookupHash(entity.NIK.String()),
		FullName:       entity.FullName,
		BirthPlace:     entity.BirthPlace,
		BirthDate:      toPGDate(entity.BirthDate),
		Gender:         genderString(entity.Gender),
		Email:          entity.Email,
		Phone:          entity.Phone,
		Address:        entity.Address,
	})
	if err != nil {
		return domain.Employee{}, mapDatabaseError(err)
	}
	return r.mapEmployee(employeeRecord{
		ID: row.ID, EmployeeNumber: row.EmployeeNumber, NikCiphertext: row.NikCiphertext,
		NikLookupHash: row.NikLookupHash, FullName: row.FullName, BirthPlace: row.BirthPlace,
		BirthDate: row.BirthDate, Gender: row.Gender, Email: row.Email, Phone: row.Phone,
		Address: row.Address, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		TenantID: row.TenantID, CompanyID: row.CompanyID,
	})
}

func (r *repositoryOps) GetEmployee(ctx context.Context, id domain.EmployeeID) (domain.Employee, error) {
	row, err := r.queries.GetEmployee(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Employee{}, domain.ErrEmployeeNotFound
	}
	if err != nil {
		return domain.Employee{}, mapDatabaseError(err)
	}
	return r.mapEmployee(employeeRecord{
		ID: row.ID, EmployeeNumber: row.EmployeeNumber, NikCiphertext: row.NikCiphertext,
		NikLookupHash: row.NikLookupHash, FullName: row.FullName, BirthPlace: row.BirthPlace,
		BirthDate: row.BirthDate, Gender: row.Gender, Email: row.Email, Phone: row.Phone,
		Address: row.Address, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		TenantID: row.TenantID, CompanyID: row.CompanyID,
	})
}

func (r *repositoryOps) ListEmployeeSummaries(ctx context.Context, filter appemployee.EmployeeListFilter) ([]domain.EmployeeSummary, error) {
	var status *string
	if filter.EmploymentStatus != nil {
		value := string(*filter.EmploymentStatus)
		status = &value
	}
	rows, err := r.queries.ListEmployeeSummaries(ctx, sqlc.ListEmployeeSummariesParams{
		Search:           filter.Search,
		TenantID:         tenantIDUUID(filter.TenantID),
		CompanyID:        companyIDUUID(filter.CompanyID),
		CursorID:         employeeIDUUID(filter.CursorID),
		EmploymentStatus: status,
		Limit:            int32(filter.Limit),
	})
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	employees := make([]domain.EmployeeSummary, 0, len(rows))
	for _, row := range rows {
		employees = append(employees, domain.EmployeeSummary{
			ID:             domain.EmployeeID(row.ID),
			TenantID:       organization.TenantID(row.TenantID),
			CompanyID:      organization.CompanyID(row.CompanyID),
			EmployeeNumber: row.EmployeeNumber,
			FullName:       row.FullName,
			CreatedAt:      timestamp(row.CreatedAt),
			UpdatedAt:      timestamp(row.UpdatedAt),
		})
	}
	return employees, nil
}

func (r *repositoryOps) UpdateEmployee(ctx context.Context, entity domain.Employee) (domain.Employee, error) {
	row, err := r.queries.UpdateEmployeePersonalData(ctx, sqlc.UpdateEmployeePersonalDataParams{
		ID:         entity.ID.UUID(),
		FullName:   entity.FullName,
		BirthPlace: entity.BirthPlace,
		BirthDate:  toPGDate(entity.BirthDate),
		Gender:     genderString(entity.Gender),
		Email:      entity.Email,
		Phone:      entity.Phone,
		Address:    entity.Address,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Employee{}, domain.ErrEmployeeNotFound
	}
	if err != nil {
		return domain.Employee{}, mapDatabaseError(err)
	}
	return r.mapEmployee(employeeRecord{
		ID: row.ID, EmployeeNumber: row.EmployeeNumber, NikCiphertext: row.NikCiphertext,
		NikLookupHash: row.NikLookupHash, FullName: row.FullName, BirthPlace: row.BirthPlace,
		BirthDate: row.BirthDate, Gender: row.Gender, Email: row.Email, Phone: row.Phone,
		Address: row.Address, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		TenantID: row.TenantID, CompanyID: row.CompanyID,
	})
}

func (r *repositoryOps) CreateEmployment(ctx context.Context, entity domain.Employment) (domain.Employment, error) {
	row, err := r.queries.CreateEmployment(ctx, sqlc.CreateEmploymentParams{
		ID:             entity.ID.UUID(),
		EmployeeID:     entity.EmployeeID.UUID(),
		TenantID:       entity.TenantID.UUID(),
		CompanyID:      entity.CompanyID.UUID(),
		EmploymentType: string(entity.EmploymentType),
		JoinDate:       toPGDate(&entity.JoinDate),
	})
	if err != nil {
		return domain.Employment{}, mapDatabaseError(err)
	}
	return mapEmployment(row), nil
}

func (r *repositoryOps) GetEmployment(ctx context.Context, id domain.EmploymentID) (domain.Employment, error) {
	row, err := r.queries.GetEmployment(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Employment{}, domain.ErrEmploymentNotFound
	}
	if err != nil {
		return domain.Employment{}, mapDatabaseError(err)
	}
	return mapEmployment(row), nil
}

func (r *repositoryOps) GetEmploymentForUpdate(ctx context.Context, id domain.EmploymentID) (domain.Employment, error) {
	row, err := r.queries.GetEmploymentForUpdate(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Employment{}, domain.ErrEmploymentNotFound
	}
	if err != nil {
		return domain.Employment{}, mapDatabaseError(err)
	}
	return mapEmployment(row), nil
}

func (r *repositoryOps) ListEmployments(ctx context.Context, id domain.EmployeeID) ([]domain.Employment, error) {
	rows, err := r.queries.ListEmploymentsByEmployee(ctx, id.UUID())
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	employments := make([]domain.Employment, 0, len(rows))
	for _, row := range rows {
		employments = append(employments, mapEmployment(row))
	}
	return employments, nil
}

func (r *repositoryOps) EndEmployment(ctx context.Context, entity domain.Employment) (domain.Employment, error) {
	row, err := r.queries.EndEmployment(ctx, sqlc.EndEmploymentParams{
		ID:                entity.ID.UUID(),
		EndDate:           toPGDate(entity.EndDate),
		TerminationReason: entity.TerminationReason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Employment{}, domain.ErrEmploymentAlreadyEnded
	}
	if err != nil {
		return domain.Employment{}, mapDatabaseError(err)
	}
	return mapEmployment(row), nil
}

func (r *repositoryOps) CreateAssignment(ctx context.Context, entity domain.Assignment) (domain.Assignment, error) {
	row, err := r.queries.CreateAssignment(ctx, sqlc.CreateAssignmentParams{
		ID:            entity.ID.UUID(),
		EmploymentID:  entity.EmploymentID.UUID(),
		LocationID:    unitIDUUID(entity.LocationID),
		DepartmentID:  unitIDUUID(entity.DepartmentID),
		PositionID:    unitIDUUID(entity.PositionID),
		GroupID:       unitIDUUID(entity.GroupID),
		EffectiveFrom: toPGDate(&entity.EffectiveFrom),
	})
	if err != nil {
		return domain.Assignment{}, mapDatabaseError(err)
	}
	return mapAssignment(row), nil
}

func (r *repositoryOps) GetAssignment(ctx context.Context, id domain.AssignmentID) (domain.Assignment, error) {
	row, err := r.queries.GetAssignment(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Assignment{}, domain.ErrAssignmentNotFound
	}
	if err != nil {
		return domain.Assignment{}, mapDatabaseError(err)
	}
	return mapAssignment(row), nil
}

func (r *repositoryOps) ListAssignments(ctx context.Context, id domain.EmploymentID) ([]domain.Assignment, error) {
	rows, err := r.queries.ListAssignmentsByEmployment(ctx, id.UUID())
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	assignments := make([]domain.Assignment, 0, len(rows))
	for _, row := range rows {
		assignments = append(assignments, mapAssignment(row))
	}
	return assignments, nil
}

func (r *repositoryOps) GetOpenAssignmentForUpdate(ctx context.Context, id domain.EmploymentID) (domain.Assignment, error) {
	row, err := r.queries.FindOpenAssignmentByEmploymentForUpdate(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Assignment{}, domain.ErrAssignmentNotFound
	}
	if err != nil {
		return domain.Assignment{}, mapDatabaseError(err)
	}
	return mapAssignment(row), nil
}

func (r *repositoryOps) CloseAssignment(ctx context.Context, entity domain.Assignment) (domain.Assignment, error) {
	if entity.EffectiveTo == nil {
		return domain.Assignment{}, domain.ErrInvalidAssignment
	}
	row, err := r.queries.CloseAssignment(ctx, sqlc.CloseAssignmentParams{
		ID:          entity.ID.UUID(),
		EffectiveTo: toPGDate(entity.EffectiveTo),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Assignment{}, domain.ErrAssignmentNotFound
	}
	if err != nil {
		return domain.Assignment{}, mapDatabaseError(err)
	}
	return mapAssignment(row), nil
}

func (r *repositoryOps) CreateTaxProfile(ctx context.Context, entity domain.TaxProfile) (domain.TaxProfile, error) {
	nikCiphertext, err := r.protector.Encrypt(entity.NIK.String())
	if err != nil {
		return domain.TaxProfile{}, fmt.Errorf("encrypt tax profile NIK: %w", err)
	}
	var npwpCiphertext []byte
	if entity.NPWP != nil {
		npwpCiphertext, err = r.protector.Encrypt(entity.NPWP.String())
		if err != nil {
			return domain.TaxProfile{}, fmt.Errorf("encrypt tax profile NPWP: %w", err)
		}
	}
	row, err := r.queries.CreateTaxProfile(ctx, sqlc.CreateTaxProfileParams{
		ID:             entity.ID.UUID(),
		EmployeeID:     entity.EmployeeID.UUID(),
		TenantID:       entity.TenantID.UUID(),
		CompanyID:      entity.CompanyID.UUID(),
		NikCiphertext:  nikCiphertext,
		NpwpCiphertext: npwpCiphertext,
		PtkpCode:       entity.PTKPCode,
		TaxMethod:      entity.TaxMethod,
		EffectiveFrom:  toPGDate(&entity.EffectiveFrom),
	})
	if err != nil {
		return domain.TaxProfile{}, mapDatabaseError(err)
	}
	return r.mapTaxProfile(row)
}

func (r *repositoryOps) GetTaxProfile(ctx context.Context, id domain.TaxProfileID) (domain.TaxProfile, error) {
	row, err := r.queries.GetTaxProfile(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaxProfile{}, domain.ErrTaxProfileNotFound
	}
	if err != nil {
		return domain.TaxProfile{}, mapDatabaseError(err)
	}
	return r.mapTaxProfile(row)
}

func (r *repositoryOps) ListTaxProfiles(ctx context.Context, id domain.EmployeeID) ([]domain.TaxProfile, error) {
	rows, err := r.queries.ListTaxProfilesByEmployee(ctx, id.UUID())
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	taxProfiles := make([]domain.TaxProfile, 0, len(rows))
	for _, row := range rows {
		taxProfile, err := r.mapTaxProfile(row)
		if err != nil {
			return nil, err
		}
		taxProfiles = append(taxProfiles, taxProfile)
	}
	return taxProfiles, nil
}

func (r *repositoryOps) GetOpenTaxProfileForUpdate(ctx context.Context, id domain.EmployeeID) (domain.TaxProfile, error) {
	row, err := r.queries.FindOpenTaxProfileByEmployeeForUpdate(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaxProfile{}, domain.ErrTaxProfileNotFound
	}
	if err != nil {
		return domain.TaxProfile{}, mapDatabaseError(err)
	}
	return r.mapTaxProfile(row)
}

func (r *repositoryOps) CloseTaxProfile(ctx context.Context, entity domain.TaxProfile) (domain.TaxProfile, error) {
	if entity.EffectiveTo == nil {
		return domain.TaxProfile{}, domain.ErrInvalidTaxProfile
	}
	row, err := r.queries.CloseTaxProfile(ctx, sqlc.CloseTaxProfileParams{
		ID:          entity.ID.UUID(),
		EffectiveTo: toPGDate(entity.EffectiveTo),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaxProfile{}, domain.ErrTaxProfileNotFound
	}
	if err != nil {
		return domain.TaxProfile{}, mapDatabaseError(err)
	}
	return r.mapTaxProfile(row)
}

type employeeRecord struct {
	ID             uuid.UUID
	EmployeeNumber string
	NikCiphertext  []byte
	NikLookupHash  []byte
	FullName       string
	BirthPlace     *string
	BirthDate      pgtype.Date
	Gender         *string
	Email          *string
	Phone          *string
	Address        *string
	CreatedAt      pgtype.Timestamptz
	UpdatedAt      pgtype.Timestamptz
	TenantID       uuid.UUID
	CompanyID      uuid.UUID
}

func (r *repositoryOps) mapEmployee(row employeeRecord) (domain.Employee, error) {
	nik, err := r.protector.Decrypt(row.NikCiphertext)
	if err != nil {
		return domain.Employee{}, fmt.Errorf("decrypt employee NIK: %w", err)
	}
	parsedNIK, err := domain.ParseNIK(nik)
	if err != nil {
		return domain.Employee{}, fmt.Errorf("employee NIK from database: %w", err)
	}
	var gender *domain.Gender
	if row.Gender != nil {
		value := domain.Gender(*row.Gender)
		gender = &value
	}
	return domain.Employee{
		ID:             domain.EmployeeID(row.ID),
		TenantID:       organization.TenantID(row.TenantID),
		CompanyID:      organization.CompanyID(row.CompanyID),
		EmployeeNumber: row.EmployeeNumber,
		NIK:            parsedNIK,
		FullName:       row.FullName,
		BirthPlace:     row.BirthPlace,
		BirthDate:      fromPGDatePtr(row.BirthDate),
		Gender:         gender,
		Email:          row.Email,
		Phone:          row.Phone,
		Address:        row.Address,
		CreatedAt:      timestamp(row.CreatedAt),
		UpdatedAt:      timestamp(row.UpdatedAt),
	}, nil
}

func (r *repositoryOps) mapTaxProfile(row sqlc.TaxEmployeeTaxProfile) (domain.TaxProfile, error) {
	nik, err := r.protector.Decrypt(row.NikCiphertext)
	if err != nil {
		return domain.TaxProfile{}, fmt.Errorf("decrypt tax profile NIK: %w", err)
	}
	parsedNIK, err := domain.ParseNIK(nik)
	if err != nil {
		return domain.TaxProfile{}, fmt.Errorf("tax profile NIK from database: %w", err)
	}
	var npwp *domain.NPWP
	if len(row.NpwpCiphertext) > 0 {
		value, err := r.protector.Decrypt(row.NpwpCiphertext)
		if err != nil {
			return domain.TaxProfile{}, fmt.Errorf("decrypt tax profile NPWP: %w", err)
		}
		parsed, err := domain.ParseNPWP(value)
		if err != nil {
			return domain.TaxProfile{}, fmt.Errorf("tax profile NPWP from database: %w", err)
		}
		npwp = &parsed
	}
	effectiveFrom, err := fromPGDate(row.EffectiveFrom)
	if err != nil {
		return domain.TaxProfile{}, err
	}
	return domain.TaxProfile{
		ID:            domain.TaxProfileID(row.ID),
		EmployeeID:    domain.EmployeeID(row.EmployeeID),
		TenantID:      organization.TenantID(row.TenantID),
		CompanyID:     organization.CompanyID(row.CompanyID),
		NIK:           parsedNIK,
		NPWP:          npwp,
		PTKPCode:      row.PtkpCode,
		TaxMethod:     row.TaxMethod,
		EffectiveFrom: effectiveFrom,
		EffectiveTo:   fromPGDatePtr(row.EffectiveTo),
		CreatedAt:     timestamp(row.CreatedAt),
		UpdatedAt:     timestamp(row.UpdatedAt),
	}, nil
}

func mapEmployeeNumberHistory(row sqlc.EmployeeEmployeeNumberHistory) domain.EmployeeNumberHistory {
	var employmentID *domain.EmploymentID
	if row.EmploymentID != nil {
		value := domain.EmploymentID(*row.EmploymentID)
		employmentID = &value
	}
	var supersedesID *domain.EmployeeNumberHistoryID
	if row.SupersedesID != nil {
		value := domain.EmployeeNumberHistoryID(*row.SupersedesID)
		supersedesID = &value
	}
	return domain.EmployeeNumberHistory{
		ID: domain.EmployeeNumberHistoryID(row.ID), TenantID: organization.TenantID(row.TenantID),
		CompanyID: organization.CompanyID(row.CompanyID), EmployeeID: domain.EmployeeID(row.EmployeeID),
		EmploymentID: employmentID, EmployeeNumber: row.EmployeeNumber,
		NumberType: domain.EmployeeNumberType(row.NumberType), EffectiveFrom: dateValue(row.EffectiveFrom),
		EffectiveTo: fromPGDatePtr(row.EffectiveTo), Source: domain.EmployeeNumberSource(row.Source),
		SourceBatchID: row.SourceBatchID, SupersedesID: supersedesID, CorrectionReason: row.CorrectionReason,
		CreatedBy: row.CreatedBy, CreatedAt: timestamp(row.CreatedAt),
	}
}

func mapEmployment(row sqlc.EmployeeEmployment) domain.Employment {
	return domain.Employment{
		ID:                domain.EmploymentID(row.ID),
		EmployeeID:        domain.EmployeeID(row.EmployeeID),
		TenantID:          organization.TenantID(row.TenantID),
		CompanyID:         organization.CompanyID(row.CompanyID),
		EmploymentType:    domain.EmploymentType(row.EmploymentType),
		JoinDate:          dateValue(row.JoinDate),
		EndDate:           fromPGDatePtr(row.EndDate),
		TerminationReason: row.TerminationReason,
		CreatedAt:         timestamp(row.CreatedAt),
		UpdatedAt:         timestamp(row.UpdatedAt),
	}
}

func mapAssignment(row sqlc.EmployeeEmployeeAssignment) domain.Assignment {
	return domain.Assignment{
		ID:            domain.AssignmentID(row.ID),
		EmploymentID:  domain.EmploymentID(row.EmploymentID),
		LocationID:    unitID(row.LocationID),
		DepartmentID:  unitID(row.DepartmentID),
		PositionID:    unitID(row.PositionID),
		GroupID:       unitID(row.GroupID),
		EffectiveFrom: dateValue(row.EffectiveFrom),
		EffectiveTo:   fromPGDatePtr(row.EffectiveTo),
		CreatedAt:     timestamp(row.CreatedAt),
		UpdatedAt:     timestamp(row.UpdatedAt),
	}
}

func mapDatabaseError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case "employees_employee_number_uq", "employees_company_employee_number_uq":
		return domain.ErrEmployeeNumberTaken
	case "employees_nik_lookup_hash_uq", "employees_company_nik_lookup_hash_uq":
		return domain.ErrNIKAlreadyRegistered
	case "employee_number_history_no_number_overlap", "employee_number_history_no_employee_overlap":
		return domain.ErrEmployeeNumberOverlap
	case "employments_one_open_per_employee_uq":
		return domain.ErrActiveEmploymentExists
	case "employments_no_overlapping_periods":
		return domain.ErrEmploymentOverlap
	case "employee_assignments_no_overlapping_periods":
		return domain.ErrAssignmentOverlap
	case "employee_tax_profiles_no_overlapping_periods", "employee_tax_profiles_effective_start_uq":
		return domain.ErrTaxProfileOverlap
	default:
		return err
	}
}

func toPGDate(value *time.Time) pgtype.Date {
	if value == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: dateOnly(*value), Valid: true}
}

func fromPGDate(value pgtype.Date) (time.Time, error) {
	if !value.Valid {
		return time.Time{}, errors.New("database date is unexpectedly NULL")
	}
	return dateOnly(value.Time), nil
}

func dateValue(value pgtype.Date) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return dateOnly(value.Time)
}

func fromPGDatePtr(value pgtype.Date) *time.Time {
	if !value.Valid {
		return nil
	}
	result := dateOnly(value.Time)
	return &result
}

func timestamp(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func toPGTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func nullableTimestamp(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func genderString(value *domain.Gender) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}

func employeeIDUUID(value *domain.EmployeeID) *uuid.UUID {
	if value == nil {
		return nil
	}
	result := value.UUID()
	return &result
}

func companyIDUUID(value *organization.CompanyID) *uuid.UUID {
	if value == nil {
		return nil
	}
	result := value.UUID()
	return &result
}

func tenantIDUUID(value *organization.TenantID) *uuid.UUID {
	if value == nil {
		return nil
	}
	result := value.UUID()
	return &result
}

func unitID(value *uuid.UUID) *organization.UnitID {
	if value == nil {
		return nil
	}
	result := organization.UnitID(*value)
	return &result
}

func unitIDUUID(value *organization.UnitID) *uuid.UUID {
	if value == nil {
		return nil
	}
	result := value.UUID()
	return &result
}
