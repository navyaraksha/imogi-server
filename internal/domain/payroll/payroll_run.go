package payroll

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type RunType string

const (
	RunRegular    RunType = "regular"
	RunOvertime   RunType = "overtime"
	RunTHR        RunType = "thr"
	RunBonus      RunType = "bonus"
	RunCorrection RunType = "correction"
	RunReversal   RunType = "reversal"
)

type PayrollRun struct {
	ID                PayrollRunID
	TenantID          organization.TenantID
	CompanyID         organization.CompanyID
	PayrollPeriodID   PayrollPeriodID
	RunType           RunType
	RunDate           time.Time
	CoverageFrom      time.Time
	CoverageTo        time.Time
	PayDate           *time.Time
	SequenceNo        int
	SourceBatchID     *uuid.UUID
	CorrectionOfRunID *PayrollRunID
	Status            string
	FinalizedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (run PayrollRun) Validate() error {
	if run.ID.UUID() == uuid.Nil || run.TenantID.UUID() == uuid.Nil || run.CompanyID.UUID() == uuid.Nil || run.PayrollPeriodID.UUID() == uuid.Nil {
		return fmt.Errorf("%w: payroll run identifiers are required", ErrInvalidPayrollPeriod)
	}
	if run.CoverageFrom.IsZero() || run.CoverageTo.IsZero() || run.CoverageFrom.After(run.CoverageTo) {
		return fmt.Errorf("%w: payroll coverage is invalid", ErrInvalidPayrollPeriod)
	}
	if run.SequenceNo < 1 {
		return fmt.Errorf("%w: payroll run sequence must be positive", ErrInvalidPayrollPeriod)
	}
	switch run.RunType {
	case RunRegular, RunOvertime, RunTHR, RunBonus:
	case RunCorrection, RunReversal:
		if run.CorrectionOfRunID == nil {
			return fmt.Errorf("%w: correction run must reference its source", ErrInvalidPayrollPeriod)
		}
	default:
		return fmt.Errorf("%w: payroll run type is invalid", ErrInvalidPayrollPeriod)
	}
	return nil
}
