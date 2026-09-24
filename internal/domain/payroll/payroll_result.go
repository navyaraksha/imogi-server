package payroll

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type ComponentType string

const (
	ComponentEarning   ComponentType = "earning"
	ComponentDeduction ComponentType = "deduction"
	ComponentBenefit   ComponentType = "benefit"
	ComponentTax       ComponentType = "tax"
)

type PayrollResult struct {
	ID                   PayrollResultID
	TenantID             organization.TenantID
	CompanyID            organization.CompanyID
	PayrollPeriodID      PayrollPeriodID
	EmployeeID           employee.EmployeeID
	EmploymentID         employee.EmploymentID
	GrossIncome          Money
	TaxableIncome        Money
	TakeHomePay          Money
	FinalizedAt          *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	PayrollRunID         *PayrollRunID
	SourceEmployeeNumber *string
	SourceSheetName      *string
	SourceRowNo          *int
}

type PayrollResultItem struct {
	ID              PayrollResultItemID
	PayrollResultID PayrollResultID
	ComponentCode   string
	ComponentType   ComponentType
	Amount          Money
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type EmploymentReference struct {
	ID         employee.EmploymentID
	EmployeeID employee.EmployeeID
	CompanyID  organization.CompanyID
	JoinDate   time.Time
	EndDate    *time.Time
}

type PayrollHistoryEntry struct {
	Result PayrollResult
	Period PayrollPeriod
	Items  []PayrollResultItem
}

func NewPayrollResult(
	id PayrollResultID,
	tenantID organization.TenantID,
	companyID organization.CompanyID,
	periodID PayrollPeriodID,
	employeeID employee.EmployeeID,
	employmentID employee.EmploymentID,
	grossIncome int64,
	taxableIncome int64,
	takeHomePay int64,
	now time.Time,
) (PayrollResult, error) {
	if id.UUID() == uuid.Nil || tenantID.UUID() == uuid.Nil || companyID.UUID() == uuid.Nil ||
		periodID.UUID() == uuid.Nil || employeeID.UUID() == uuid.Nil || employmentID.UUID() == uuid.Nil {
		return PayrollResult{}, fmt.Errorf("%w: identifiers are required", ErrInvalidPayrollResult)
	}
	gross, err := NewMoney(grossIncome)
	if err != nil {
		return PayrollResult{}, err
	}
	taxable, err := NewMoney(taxableIncome)
	if err != nil {
		return PayrollResult{}, err
	}
	takeHome, err := NewMoney(takeHomePay)
	if err != nil {
		return PayrollResult{}, err
	}
	now = now.UTC()
	return PayrollResult{
		ID:              id,
		TenantID:        tenantID,
		CompanyID:       companyID,
		PayrollPeriodID: periodID,
		EmployeeID:      employeeID,
		EmploymentID:    employmentID,
		GrossIncome:     gross,
		TaxableIncome:   taxable,
		TakeHomePay:     takeHome,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func NewPayrollResultItem(
	id PayrollResultItemID,
	resultID PayrollResultID,
	componentCode string,
	componentType ComponentType,
	amount int64,
	now time.Time,
) (PayrollResultItem, error) {
	if id.UUID() == uuid.Nil || resultID.UUID() == uuid.Nil {
		return PayrollResultItem{}, fmt.Errorf("%w: identifiers are required", ErrInvalidPayrollComponent)
	}
	componentCode = strings.ToUpper(strings.TrimSpace(componentCode))
	if componentCode == "" || len(componentCode) > 100 {
		return PayrollResultItem{}, fmt.Errorf("%w: component code is invalid", ErrInvalidPayrollComponent)
	}
	switch componentType {
	case ComponentEarning, ComponentDeduction, ComponentBenefit, ComponentTax:
	default:
		return PayrollResultItem{}, fmt.Errorf("%w: component type is invalid", ErrInvalidPayrollComponent)
	}
	money, err := NewMoney(amount)
	if err != nil {
		return PayrollResultItem{}, fmt.Errorf("%w: %v", ErrInvalidPayrollComponent, err)
	}
	now = now.UTC()
	return PayrollResultItem{
		ID:              id,
		PayrollResultID: resultID,
		ComponentCode:   componentCode,
		ComponentType:   componentType,
		Amount:          money,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func (employment EmploymentReference) CoversPeriod(year, month int) bool {
	// An employment covers a payroll month when it overlaps any day in that
	// month; an open employment has no upper bound.
	start, end, err := PeriodBounds(year, month)
	if err != nil {
		return false
	}
	if employment.JoinDate.After(end) {
		return false
	}
	return employment.EndDate == nil || !employment.EndDate.Before(start)
}
