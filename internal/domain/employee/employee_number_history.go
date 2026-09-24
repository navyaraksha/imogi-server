package employee

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type EmployeeNumberType string

const (
	EmployeeNumberTemporary EmployeeNumberType = "temporary"
	EmployeeNumberPermanent EmployeeNumberType = "permanent"
)

type EmployeeNumberSource string

const (
	EmployeeNumberSourceManual     EmployeeNumberSource = "manual"
	EmployeeNumberSourceImport     EmployeeNumberSource = "import"
	EmployeeNumberSourceCorrection EmployeeNumberSource = "correction"
	EmployeeNumberSourceMigration  EmployeeNumberSource = "migration"
)

type EmployeeNumberHistory struct {
	ID               EmployeeNumberHistoryID
	TenantID         organization.TenantID
	CompanyID        organization.CompanyID
	EmployeeID       EmployeeID
	EmploymentID     *EmploymentID
	EmployeeNumber   string
	NumberType       EmployeeNumberType
	EffectiveFrom    time.Time
	EffectiveTo      *time.Time
	Source           EmployeeNumberSource
	SourceBatchID    *uuid.UUID
	SupersedesID     *EmployeeNumberHistoryID
	CorrectionReason *string
	CreatedBy        *uuid.UUID
	CreatedAt        time.Time
}

func NewEmployeeNumberHistory(
	id EmployeeNumberHistoryID,
	tenantID organization.TenantID,
	companyID organization.CompanyID,
	employeeID EmployeeID,
	employmentID *EmploymentID,
	number string,
	numberType EmployeeNumberType,
	effectiveFrom time.Time,
	source EmployeeNumberSource,
	now time.Time,
) (EmployeeNumberHistory, error) {
	if id.UUID() == uuid.Nil || tenantID.UUID() == uuid.Nil || companyID.UUID() == uuid.Nil || employeeID.UUID() == uuid.Nil {
		return EmployeeNumberHistory{}, fmt.Errorf("%w: identifiers are required", ErrInvalidEmployee)
	}
	number = strings.TrimSpace(number)
	if number == "" || len(number) > 100 {
		return EmployeeNumberHistory{}, fmt.Errorf("%w: employee number is required and must be at most 100 characters", ErrInvalidEmployee)
	}
	if numberType != EmployeeNumberTemporary && numberType != EmployeeNumberPermanent {
		return EmployeeNumberHistory{}, fmt.Errorf("%w: invalid employee number type", ErrInvalidEmployee)
	}
	switch source {
	case EmployeeNumberSourceManual, EmployeeNumberSourceImport, EmployeeNumberSourceCorrection, EmployeeNumberSourceMigration:
	default:
		return EmployeeNumberHistory{}, fmt.Errorf("%w: invalid employee number source", ErrInvalidEmployee)
	}
	return EmployeeNumberHistory{
		ID: id, TenantID: tenantID, CompanyID: companyID, EmployeeID: employeeID,
		EmploymentID: employmentID, EmployeeNumber: number, NumberType: numberType,
		EffectiveFrom: dateOnly(effectiveFrom), Source: source, CreatedAt: now.UTC(),
	}, nil
}

func (history *EmployeeNumberHistory) Close(effectiveTo time.Time) error {
	if history == nil || history.ID.UUID() == uuid.Nil {
		return ErrInvalidEmployee
	}
	if history.EffectiveTo != nil {
		return fmt.Errorf("%w: employee number history is already closed", ErrInvalidEmployee)
	}
	effectiveTo = dateOnly(effectiveTo)
	if effectiveTo.Before(history.EffectiveFrom) {
		return fmt.Errorf("%w: effective end date cannot precede start date", ErrInvalidEmployee)
	}
	history.EffectiveTo = &effectiveTo
	return nil
}

func (history EmployeeNumberHistory) IsEffectiveOn(date time.Time) bool {
	date = dateOnly(date)
	if date.Before(history.EffectiveFrom) {
		return false
	}
	return history.EffectiveTo == nil || !date.After(*history.EffectiveTo)
}
