package payroll

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type PeriodStatus string

const (
	PeriodOpen      PeriodStatus = "open"
	PeriodFinalized PeriodStatus = "finalized"
)

type PayrollPeriod struct {
	ID          PayrollPeriodID
	TenantID    organization.TenantID
	CompanyID   organization.CompanyID
	Year        int
	Month       int
	Status      PeriodStatus
	OpenedAt    time.Time
	FinalizedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewPayrollPeriod(
	id PayrollPeriodID,
	tenantID organization.TenantID,
	companyID organization.CompanyID,
	year int,
	month int,
	now time.Time,
) (PayrollPeriod, error) {
	if id.UUID() == uuid.Nil || tenantID.UUID() == uuid.Nil || companyID.UUID() == uuid.Nil {
		return PayrollPeriod{}, fmt.Errorf("%w: identifiers are required", ErrInvalidPayrollPeriod)
	}
	if year < 2000 || year > 9999 {
		return PayrollPeriod{}, fmt.Errorf("%w: year must be between 2000 and 9999", ErrInvalidPayrollPeriod)
	}
	if month < 1 || month > 12 {
		return PayrollPeriod{}, fmt.Errorf("%w: month must be between 1 and 12", ErrInvalidPayrollPeriod)
	}
	now = now.UTC()
	return PayrollPeriod{
		ID:        id,
		TenantID:  tenantID,
		CompanyID: companyID,
		Year:      year,
		Month:     month,
		Status:    PeriodOpen,
		OpenedAt:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (period PayrollPeriod) Finalize(at time.Time) (PayrollPeriod, error) {
	if period.Status == PeriodFinalized {
		return PayrollPeriod{}, ErrPayrollPeriodAlreadyFinalized
	}
	if period.Status != PeriodOpen {
		return PayrollPeriod{}, ErrPayrollPeriodNotOpen
	}
	at = at.UTC()
	period.Status = PeriodFinalized
	period.FinalizedAt = &at
	period.UpdatedAt = at
	return period, nil
}

func (period PayrollPeriod) IsFinalized() bool {
	return period.Status == PeriodFinalized
}

func PeriodBounds(year, month int) (time.Time, time.Time, error) {
	if year < 2000 || year > 9999 || month < 1 || month > 12 {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: invalid year or month", ErrInvalidPayrollPeriod)
	}
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end, nil
}
