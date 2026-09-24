package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	filedomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	payrolldomain "github.com/navyaraksha/imogi/internal/domain/payroll"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres/sqlc"
	"github.com/navyaraksha/imogi/internal/platform/clock"
)

// PayrollImportWriter is the persistence adapter for a validated payroll
// source. It owns one database transaction so a run never contains a partial
// set of results.
type PayrollImportWriter struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

func NewPayrollImportWriter(pool *pgxpool.Pool, systemClock clock.Clock) (*PayrollImportWriter, error) {
	if pool == nil || systemClock == nil {
		return nil, fmt.Errorf("payroll import writer dependencies are required")
	}
	return &PayrollImportWriter{pool: pool, clock: systemClock}, nil
}

func (writer *PayrollImportWriter) CommitPayrollImport(ctx context.Context, batch filedomain.ImportBatch, rows []appfilebatch.PayrollImportRow) (appfilebatch.PayrollImportReceipt, error) {
	if batch.PayrollContext == nil {
		return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("payroll context is required")
	}
	if len(rows) == 0 {
		return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("payroll import contains no rows")
	}
	contextValue := *batch.PayrollContext
	tx, err := writer.pool.Begin(ctx)
	if err != nil {
		return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("begin payroll import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := sqlc.New(tx)

	periodRow, err := queries.GetPayrollPeriodByTenantCompanyMonth(ctx, sqlc.GetPayrollPeriodByTenantCompanyMonthParams{TenantID: batch.TenantID.UUID(), CompanyID: batch.CompanyID.UUID(), Year: int32(contextValue.TaxYear), Month: int32(contextValue.TaxMonth)})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return appfilebatch.PayrollImportReceipt{}, mapPayrollDatabaseError(err)
		}
		periodID, idErr := payrolldomain.NewPayrollPeriodID()
		if idErr != nil {
			return appfilebatch.PayrollImportReceipt{}, idErr
		}
		periodRow, err = queries.CreatePayrollPeriod(ctx, sqlc.CreatePayrollPeriodParams{ID: periodID.UUID(), TenantID: batch.TenantID.UUID(), CompanyID: batch.CompanyID.UUID(), Year: int32(contextValue.TaxYear), Month: int32(contextValue.TaxMonth)})
		if err != nil {
			return appfilebatch.PayrollImportReceipt{}, mapPayrollDatabaseError(err)
		}
	}
	if periodRow.Status == "finalized" {
		return appfilebatch.PayrollImportReceipt{}, payrolldomain.ErrPayrollPeriodAlreadyFinalized
	}

	runID, err := payrolldomain.NewPayrollRunID()
	if err != nil {
		return appfilebatch.PayrollImportReceipt{}, err
	}
	sequence, err := queries.NextPayrollRunSequence(ctx, sqlc.NextPayrollRunSequenceParams{TenantID: batch.TenantID.UUID(), CompanyID: batch.CompanyID.UUID(), PayrollPeriodID: periodRow.ID, RunType: contextValue.RunType})
	if err != nil {
		return appfilebatch.PayrollImportReceipt{}, mapPayrollDatabaseError(err)
	}
	runDate := writer.clock.Now().UTC()
	runRow, err := queries.CreatePayrollRun(ctx, sqlc.CreatePayrollRunParams{ID: runID.UUID(), TenantID: batch.TenantID.UUID(), CompanyID: batch.CompanyID.UUID(), PayrollPeriodID: periodRow.ID, RunType: contextValue.RunType, RunDate: toPGDate(&runDate), CoverageFrom: toPGDate(&contextValue.CoverageFrom), CoverageTo: toPGDate(&contextValue.CoverageTo), PayDate: toPGDate(contextValue.PayDate), SequenceNo: sequence, SourceBatchID: uuidPtr(batch.ID.UUID())})
	if err != nil {
		return appfilebatch.PayrollImportReceipt{}, mapPayrollDatabaseError(err)
	}

	for _, item := range rows {
		if item.EmployeeID == nil || item.EmploymentID == nil {
			return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("payroll row %s has no resolved employment", item.ID.UUID().String())
		}
		employment, err := queries.GetEmploymentForPayrollCoverage(ctx, sqlc.GetEmploymentForPayrollCoverageParams{
			ID:           *item.EmploymentID,
			CoverageFrom: toPGDate(&contextValue.CoverageFrom),
			CoverageTo:   toPGDate(&contextValue.CoverageTo),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("row %d employment is outside payroll coverage", item.RowNumber)
			}
			return appfilebatch.PayrollImportReceipt{}, mapPayrollDatabaseError(err)
		}
		if employment.EmployeeID != *item.EmployeeID || employment.CompanyID != batch.CompanyID.UUID() {
			return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("row %d employment does not belong to the payroll company", item.RowNumber)
		}
		gross, err := parseImportMoney(item.Values["gross_income"])
		if err != nil {
			return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("row %d gross_income: %w", item.RowNumber, err)
		}
		taxable, err := parseOptionalImportMoney(item.Values["taxable_income"], gross)
		if err != nil {
			return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("row %d taxable_income: %w", item.RowNumber, err)
		}
		takeHome, err := parseOptionalImportMoney(item.Values["take_home_pay"], gross)
		if err != nil {
			return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("row %d take_home_pay: %w", item.RowNumber, err)
		}
		resultID, idErr := payrolldomain.NewPayrollResultID()
		if idErr != nil {
			return appfilebatch.PayrollImportReceipt{}, idErr
		}
		var sourceNumber *string
		if value := strings.TrimSpace(item.Values["employee_number"]); value != "" {
			sourceNumber = &value
		}
		rowNumber := int32(item.RowNumber)
		if _, err := queries.CreatePayrollResult(ctx, sqlc.CreatePayrollResultParams{ID: resultID.UUID(), TenantID: batch.TenantID.UUID(), CompanyID: batch.CompanyID.UUID(), PayrollPeriodID: periodRow.ID, EmployeeID: *item.EmployeeID, EmploymentID: *item.EmploymentID, GrossIncome: gross, TaxableIncome: taxable, TakeHomePay: takeHome, PayrollRunID: &runRow.ID, SourceEmployeeNumber: sourceNumber, SourceSheetName: &item.SheetName, SourceRowNo: &rowNumber}); err != nil {
			return appfilebatch.PayrollImportReceipt{}, mapPayrollDatabaseError(err)
		}
		itemID, idErr := payrolldomain.NewPayrollResultItemID()
		if idErr != nil {
			return appfilebatch.PayrollImportReceipt{}, idErr
		}
		if _, err := queries.CreatePayrollResultItem(ctx, sqlc.CreatePayrollResultItemParams{ID: itemID.UUID(), PayrollResultID: resultID.UUID(), ComponentCode: "GROSS_INCOME", ComponentType: string(payrolldomain.ComponentEarning), Amount: gross}); err != nil {
			return appfilebatch.PayrollImportReceipt{}, mapPayrollDatabaseError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return appfilebatch.PayrollImportReceipt{}, fmt.Errorf("commit payroll import: %w", err)
	}
	return appfilebatch.PayrollImportReceipt{PayrollPeriodID: periodRow.ID, PayrollRunID: runRow.ID, CoverageFrom: contextValue.CoverageFrom, CoverageTo: contextValue.CoverageTo, PayDate: contextValue.PayDate, RowsCommitted: len(rows)}, nil
}

func parseOptionalImportMoney(value string, fallback int64) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return parseImportMoney(value)
}

func parseImportMoney(value string) (int64, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "Rp"), "rp"))
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, ",", "")
	value = strings.ReplaceAll(value, ".", "")
	if value == "" {
		return 0, fmt.Errorf("amount is required")
	}
	negative := strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")")
	value = strings.Trim(value, "()")
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid IDR amount")
	}
	if negative {
		parsed = -parsed
	}
	return parsed, nil
}

var _ appfilebatch.PayrollImportWriter = (*PayrollImportWriter)(nil)
