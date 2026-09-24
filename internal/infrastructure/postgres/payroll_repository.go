package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	apppayroll "github.com/navyaraksha/imogi/internal/application/payroll"
	"github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	domain "github.com/navyaraksha/imogi/internal/domain/payroll"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres/sqlc"
)

type PayrollRepository struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

type payrollTransaction struct {
	queries *sqlc.Queries
}

var _ apppayroll.Repository = (*PayrollRepository)(nil)

func NewPayrollRepository(pool *pgxpool.Pool) (*PayrollRepository, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	return &PayrollRepository{
		pool:    pool,
		queries: sqlc.New(pool),
	}, nil
}

func (r *PayrollRepository) WithinTransaction(ctx context.Context, fn func(apppayroll.Transaction) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin payroll transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transactional := &payrollTransaction{queries: r.queries.WithTx(tx)}
	if err := fn(transactional); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit payroll transaction: %w", err)
	}
	return nil
}

func (r *PayrollRepository) CreatePayrollPeriod(ctx context.Context, entity domain.PayrollPeriod) (domain.PayrollPeriod, error) {
	row, err := r.queries.CreatePayrollPeriod(ctx, sqlc.CreatePayrollPeriodParams{
		ID:        entity.ID.UUID(),
		TenantID:  entity.TenantID.UUID(),
		CompanyID: entity.CompanyID.UUID(),
		Year:      int32(entity.Year),
		Month:     int32(entity.Month),
	})
	if err != nil {
		return domain.PayrollPeriod{}, mapPayrollDatabaseError(err)
	}
	return mapPayrollPeriod(row), nil
}

func (r *PayrollRepository) GetPayrollPeriod(ctx context.Context, id domain.PayrollPeriodID) (domain.PayrollPeriod, error) {
	row, err := r.queries.GetPayrollPeriod(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PayrollPeriod{}, domain.ErrPayrollPeriodNotFound
	}
	if err != nil {
		return domain.PayrollPeriod{}, mapPayrollDatabaseError(err)
	}
	return mapPayrollPeriod(row), nil
}

func (r *PayrollRepository) GetPayrollPeriodByScope(ctx context.Context, tenantID organization.TenantID, companyID organization.CompanyID, year, month int) (domain.PayrollPeriod, error) {
	row, err := r.queries.GetPayrollPeriodByTenantCompanyMonth(ctx, sqlc.GetPayrollPeriodByTenantCompanyMonthParams{TenantID: tenantID.UUID(), CompanyID: companyID.UUID(), Year: int32(year), Month: int32(month)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PayrollPeriod{}, domain.ErrPayrollPeriodNotFound
	}
	if err != nil {
		return domain.PayrollPeriod{}, mapPayrollDatabaseError(err)
	}
	return mapPayrollPeriod(row), nil
}

func (r *PayrollRepository) ListPayrollPeriods(ctx context.Context, filter apppayroll.PeriodFilter) ([]domain.PayrollPeriod, error) {
	var companyID *uuid.UUID
	if filter.CompanyID.UUID() != uuid.Nil {
		value := filter.CompanyID.UUID()
		companyID = &value
	}
	var status *string
	if filter.Status != nil {
		value := string(*filter.Status)
		status = &value
	}
	var cursorID *uuid.UUID
	if filter.CursorID != nil {
		value := filter.CursorID.UUID()
		cursorID = &value
	}
	rows, err := r.queries.ListPayrollPeriods(ctx, sqlc.ListPayrollPeriodsParams{
		TenantID:  filter.TenantID.UUID(),
		CompanyID: companyID,
		Status:    status,
		CursorID:  cursorID,
		Limit:     int32(filter.Limit),
	})
	if err != nil {
		return nil, mapPayrollDatabaseError(err)
	}
	periods := make([]domain.PayrollPeriod, 0, len(rows))
	for _, row := range rows {
		periods = append(periods, mapPayrollPeriod(row))
	}
	return periods, nil
}

func (r *PayrollRepository) GetEmploymentPayrollReference(ctx context.Context, id employee.EmploymentID) (domain.EmploymentReference, error) {
	row, err := r.queries.GetEmploymentPayrollReference(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EmploymentReference{}, employee.ErrEmploymentNotFound
	}
	if err != nil {
		return domain.EmploymentReference{}, mapPayrollDatabaseError(err)
	}
	return domain.EmploymentReference{
		ID:         employee.EmploymentID(row.ID),
		EmployeeID: employee.EmployeeID(row.EmployeeID),
		CompanyID:  organization.CompanyID(row.CompanyID),
		JoinDate:   dateValue(row.JoinDate),
		EndDate:    fromPGDatePtr(row.EndDate),
	}, nil
}

func (r *PayrollRepository) GetEmployeePayrollReference(ctx context.Context, id employee.EmployeeID) (organization.CompanyID, error) {
	companyID, err := r.queries.GetEmployeePayrollReference(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.CompanyID{}, employee.ErrEmployeeNotFound
	}
	if err != nil {
		return organization.CompanyID{}, mapPayrollDatabaseError(err)
	}
	return organization.CompanyID(companyID), nil
}

func (r *PayrollRepository) ListPayrollHistory(ctx context.Context, employeeID employee.EmployeeID, filter apppayroll.HistoryFilter) ([]domain.PayrollHistoryEntry, error) {
	var year *int32
	if filter.Year != nil {
		value := int32(*filter.Year)
		year = &value
	}
	var cursorID *uuid.UUID
	if filter.CursorID != nil {
		value := filter.CursorID.UUID()
		cursorID = &value
	}
	rows, err := r.queries.ListPayrollHistory(ctx, sqlc.ListPayrollHistoryParams{
		TenantID:   filter.TenantID.UUID(),
		EmployeeID: employeeID.UUID(),
		Year:       year,
		CursorID:   cursorID,
		Limit:      int32(filter.Limit),
	})
	if err != nil {
		return nil, mapPayrollDatabaseError(err)
	}
	history := make([]domain.PayrollHistoryEntry, 0, len(rows))
	for _, row := range rows {
		items, itemErr := r.queries.ListPayrollResultItems(ctx, row.ID)
		if itemErr != nil {
			return nil, mapPayrollDatabaseError(itemErr)
		}
		history = append(history, domain.PayrollHistoryEntry{
			Period: domain.PayrollPeriod{
				ID:          domain.PayrollPeriodID(row.PayrollPeriodID),
				TenantID:    organization.TenantID(row.TenantID),
				CompanyID:   organization.CompanyID(row.CompanyID),
				Year:        int(row.PeriodYear),
				Month:       int(row.PeriodMonth),
				Status:      domain.PeriodStatus(row.PeriodStatus),
				OpenedAt:    timestamp(row.PeriodOpenedAt),
				FinalizedAt: nullableTimestamp(row.PeriodFinalizedAt),
			},
			Result: mapPayrollHistoryResult(row),
			Items:  mapPayrollItems(items),
		})
	}
	return history, nil
}

func (t *payrollTransaction) GetPayrollPeriodForUpdate(ctx context.Context, id domain.PayrollPeriodID) (domain.PayrollPeriod, error) {
	row, err := t.queries.GetPayrollPeriodForUpdate(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PayrollPeriod{}, domain.ErrPayrollPeriodNotFound
	}
	if err != nil {
		return domain.PayrollPeriod{}, mapPayrollDatabaseError(err)
	}
	return mapPayrollPeriod(row), nil
}

func (t *payrollTransaction) CreatePayrollResult(ctx context.Context, entity domain.PayrollResult) (domain.PayrollResult, error) {
	row, err := t.queries.CreatePayrollResult(ctx, sqlc.CreatePayrollResultParams{
		ID:              entity.ID.UUID(),
		TenantID:        entity.TenantID.UUID(),
		CompanyID:       entity.CompanyID.UUID(),
		PayrollPeriodID: entity.PayrollPeriodID.UUID(),
		EmployeeID:      entity.EmployeeID.UUID(),
		EmploymentID:    entity.EmploymentID.UUID(),
		GrossIncome:     entity.GrossIncome.Int64(),
		TaxableIncome:   entity.TaxableIncome.Int64(),
		TakeHomePay:     entity.TakeHomePay.Int64(),
		PayrollRunID:    payrollRunID(entity.PayrollRunID), SourceEmployeeNumber: entity.SourceEmployeeNumber,
		SourceSheetName: entity.SourceSheetName, SourceRowNo: int32Ptr(entity.SourceRowNo),
	})
	if err != nil {
		return domain.PayrollResult{}, mapPayrollDatabaseError(err)
	}
	return mapPayrollResult(row), nil
}

func (t *payrollTransaction) CreatePayrollRun(ctx context.Context, entity domain.PayrollRun) (domain.PayrollRun, error) {
	if err := entity.Validate(); err != nil {
		return domain.PayrollRun{}, err
	}
	row, err := t.queries.CreatePayrollRun(ctx, sqlc.CreatePayrollRunParams{
		ID: entity.ID.UUID(), TenantID: entity.TenantID.UUID(), CompanyID: entity.CompanyID.UUID(), PayrollPeriodID: entity.PayrollPeriodID.UUID(),
		RunType: string(entity.RunType), RunDate: toPGDate(&entity.RunDate), CoverageFrom: toPGDate(&entity.CoverageFrom), CoverageTo: toPGDate(&entity.CoverageTo),
		PayDate: toPGDate(entity.PayDate), SequenceNo: int32(entity.SequenceNo), SourceBatchID: entity.SourceBatchID, CorrectionOfRunID: payrollRunID(entity.CorrectionOfRunID),
	})
	if err != nil {
		return domain.PayrollRun{}, mapPayrollDatabaseError(err)
	}
	return mapPayrollRun(row), nil
}

func (t *payrollTransaction) NextPayrollRunSequence(ctx context.Context, periodID domain.PayrollPeriodID, runType domain.RunType, tenantID organization.TenantID, companyID organization.CompanyID) (int, error) {
	value, err := t.queries.NextPayrollRunSequence(ctx, sqlc.NextPayrollRunSequenceParams{TenantID: tenantID.UUID(), CompanyID: companyID.UUID(), PayrollPeriodID: periodID.UUID(), RunType: string(runType)})
	return int(value), mapPayrollDatabaseError(err)
}

func (t *payrollTransaction) CreatePayrollResultItem(ctx context.Context, entity domain.PayrollResultItem) (domain.PayrollResultItem, error) {
	row, err := t.queries.CreatePayrollResultItem(ctx, sqlc.CreatePayrollResultItemParams{
		ID:              entity.ID.UUID(),
		PayrollResultID: entity.PayrollResultID.UUID(),
		ComponentCode:   entity.ComponentCode,
		ComponentType:   string(entity.ComponentType),
		Amount:          entity.Amount.Int64(),
	})
	if err != nil {
		return domain.PayrollResultItem{}, mapPayrollDatabaseError(err)
	}
	return mapPayrollItem(row), nil
}

func (t *payrollTransaction) CountPayrollResults(ctx context.Context, periodID domain.PayrollPeriodID) (int64, error) {
	count, err := t.queries.CountPayrollResults(ctx, periodID.UUID())
	if err != nil {
		return 0, mapPayrollDatabaseError(err)
	}
	return count, nil
}

func (t *payrollTransaction) FinalizePayrollResults(ctx context.Context, periodID domain.PayrollPeriodID, finalizedAt time.Time) error {
	err := t.queries.FinalizePayrollResults(ctx, sqlc.FinalizePayrollResultsParams{
		FinalizedAt:     toPGTimestamp(finalizedAt),
		PayrollPeriodID: periodID.UUID(),
	})
	return mapPayrollDatabaseError(err)
}

func (t *payrollTransaction) FinalizePayrollPeriod(ctx context.Context, periodID domain.PayrollPeriodID, finalizedAt time.Time) (domain.PayrollPeriod, error) {
	row, err := t.queries.FinalizePayrollPeriod(ctx, sqlc.FinalizePayrollPeriodParams{
		ID:          periodID.UUID(),
		FinalizedAt: toPGTimestamp(finalizedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PayrollPeriod{}, domain.ErrPayrollPeriodAlreadyFinalized
	}
	if err != nil {
		return domain.PayrollPeriod{}, mapPayrollDatabaseError(err)
	}
	return mapPayrollPeriod(row), nil
}

func mapPayrollPeriod(row sqlc.PayrollPayrollPeriod) domain.PayrollPeriod {
	return domain.PayrollPeriod{
		ID:          domain.PayrollPeriodID(row.ID),
		TenantID:    organization.TenantID(row.TenantID),
		CompanyID:   organization.CompanyID(row.CompanyID),
		Year:        int(row.Year),
		Month:       int(row.Month),
		Status:      domain.PeriodStatus(row.Status),
		OpenedAt:    timestamp(row.OpenedAt),
		FinalizedAt: nullableTimestamp(row.FinalizedAt),
		CreatedAt:   timestamp(row.CreatedAt),
		UpdatedAt:   timestamp(row.UpdatedAt),
	}
}

func mapPayrollResult(row sqlc.PayrollPayrollResult) domain.PayrollResult {
	var runID *domain.PayrollRunID
	if row.PayrollRunID != nil {
		value := domain.PayrollRunID(*row.PayrollRunID)
		runID = &value
	}
	var sourceRow *int
	if row.SourceRowNo != nil {
		value := int(*row.SourceRowNo)
		sourceRow = &value
	}
	return domain.PayrollResult{
		ID:              domain.PayrollResultID(row.ID),
		TenantID:        organization.TenantID(row.TenantID),
		CompanyID:       organization.CompanyID(row.CompanyID),
		PayrollPeriodID: domain.PayrollPeriodID(row.PayrollPeriodID),
		EmployeeID:      employee.EmployeeID(row.EmployeeID),
		EmploymentID:    employee.EmploymentID(row.EmploymentID),
		GrossIncome:     domain.Money(row.GrossIncome),
		TaxableIncome:   domain.Money(row.TaxableIncome),
		TakeHomePay:     domain.Money(row.TakeHomePay),
		FinalizedAt:     nullableTimestamp(row.FinalizedAt),
		CreatedAt:       timestamp(row.CreatedAt),
		UpdatedAt:       timestamp(row.UpdatedAt),
		PayrollRunID:    runID, SourceEmployeeNumber: row.SourceEmployeeNumber, SourceSheetName: row.SourceSheetName, SourceRowNo: sourceRow,
	}
}

func mapPayrollHistoryResult(row sqlc.ListPayrollHistoryRow) domain.PayrollResult {
	return domain.PayrollResult{
		ID:              domain.PayrollResultID(row.ID),
		TenantID:        organization.TenantID(row.TenantID),
		CompanyID:       organization.CompanyID(row.CompanyID),
		PayrollPeriodID: domain.PayrollPeriodID(row.PayrollPeriodID),
		EmployeeID:      employee.EmployeeID(row.EmployeeID),
		EmploymentID:    employee.EmploymentID(row.EmploymentID),
		GrossIncome:     domain.Money(row.GrossIncome),
		TaxableIncome:   domain.Money(row.TaxableIncome),
		TakeHomePay:     domain.Money(row.TakeHomePay),
		FinalizedAt:     nullableTimestamp(row.FinalizedAt),
		CreatedAt:       timestamp(row.CreatedAt),
		UpdatedAt:       timestamp(row.UpdatedAt),
	}
}

func mapPayrollRun(row sqlc.PayrollPayrollRun) domain.PayrollRun {
	var runID *domain.PayrollRunID
	if row.CorrectionOfRunID != nil {
		value := domain.PayrollRunID(*row.CorrectionOfRunID)
		runID = &value
	}
	return domain.PayrollRun{ID: domain.PayrollRunID(row.ID), TenantID: organization.TenantID(row.TenantID), CompanyID: organization.CompanyID(row.CompanyID), PayrollPeriodID: domain.PayrollPeriodID(row.PayrollPeriodID), RunType: domain.RunType(row.RunType), RunDate: dateValue(row.RunDate), CoverageFrom: dateValue(row.CoverageFrom), CoverageTo: dateValue(row.CoverageTo), PayDate: fromPGDatePtr(row.PayDate), SequenceNo: int(row.SequenceNo), SourceBatchID: row.SourceBatchID, CorrectionOfRunID: runID, Status: row.Status, FinalizedAt: nullableTimestamp(row.FinalizedAt), CreatedAt: timestamp(row.CreatedAt), UpdatedAt: timestamp(row.UpdatedAt)}
}

func payrollRunID(value *domain.PayrollRunID) *uuid.UUID {
	if value == nil {
		return nil
	}
	id := value.UUID()
	return &id
}

func mapPayrollItem(row sqlc.PayrollPayrollResultItem) domain.PayrollResultItem {
	return domain.PayrollResultItem{
		ID:              domain.PayrollResultItemID(row.ID),
		PayrollResultID: domain.PayrollResultID(row.PayrollResultID),
		ComponentCode:   row.ComponentCode,
		ComponentType:   domain.ComponentType(row.ComponentType),
		Amount:          domain.Money(row.Amount),
		CreatedAt:       timestamp(row.CreatedAt),
		UpdatedAt:       timestamp(row.UpdatedAt),
	}
}

func mapPayrollItems(rows []sqlc.PayrollPayrollResultItem) []domain.PayrollResultItem {
	items := make([]domain.PayrollResultItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapPayrollItem(row))
	}
	return items
}

func mapPayrollDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case "payroll_periods_company_month_uq":
		return domain.ErrPayrollPeriodAlreadyExists
	case "payroll_results_employee_period_uq":
		return domain.ErrPayrollResultAlreadyExists
	default:
		return err
	}
}
