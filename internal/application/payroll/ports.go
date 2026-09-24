package payroll

import (
	"context"
	"time"

	"github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	domain "github.com/navyaraksha/imogi/internal/domain/payroll"
)

type PeriodFilter struct {
	TenantID  organization.TenantID
	CompanyID organization.CompanyID
	Status    *domain.PeriodStatus
	CursorID  *domain.PayrollPeriodID
	Limit     int
}

type HistoryFilter struct {
	TenantID organization.TenantID
	Year     *int
	CursorID *domain.PayrollResultID
	Limit    int
}

type CreatePayrollPeriodInput struct {
	CompanyID organization.CompanyID
	Year      int
	Month     int
}

type CreatePayrollResultItemInput struct {
	ComponentCode string
	ComponentType domain.ComponentType
	Amount        int64
}

type CreatePayrollResultInput struct {
	PayrollPeriodID domain.PayrollPeriodID
	EmployeeID      employee.EmployeeID
	EmploymentID    employee.EmploymentID
	GrossIncome     int64
	TaxableIncome   int64
	TakeHomePay     int64
	Items           []CreatePayrollResultItemInput
}

type Repository interface {
	WithinTransaction(context.Context, func(Transaction) error) error

	CreatePayrollPeriod(context.Context, domain.PayrollPeriod) (domain.PayrollPeriod, error)
	GetPayrollPeriodByScope(context.Context, organization.TenantID, organization.CompanyID, int, int) (domain.PayrollPeriod, error)
	GetPayrollPeriod(context.Context, domain.PayrollPeriodID) (domain.PayrollPeriod, error)
	ListPayrollPeriods(context.Context, PeriodFilter) ([]domain.PayrollPeriod, error)
	GetEmploymentPayrollReference(context.Context, employee.EmploymentID) (domain.EmploymentReference, error)
	GetEmployeePayrollReference(context.Context, employee.EmployeeID) (organization.CompanyID, error)
	ListPayrollHistory(context.Context, employee.EmployeeID, HistoryFilter) ([]domain.PayrollHistoryEntry, error)
}

type Transaction interface {
	GetPayrollPeriodForUpdate(context.Context, domain.PayrollPeriodID) (domain.PayrollPeriod, error)
	CreatePayrollResult(context.Context, domain.PayrollResult) (domain.PayrollResult, error)
	CreatePayrollRun(context.Context, domain.PayrollRun) (domain.PayrollRun, error)
	NextPayrollRunSequence(context.Context, domain.PayrollPeriodID, domain.RunType, organization.TenantID, organization.CompanyID) (int, error)
	CreatePayrollResultItem(context.Context, domain.PayrollResultItem) (domain.PayrollResultItem, error)
	CountPayrollResults(context.Context, domain.PayrollPeriodID) (int64, error)
	FinalizePayrollResults(context.Context, domain.PayrollPeriodID, time.Time) error
	FinalizePayrollPeriod(context.Context, domain.PayrollPeriodID, time.Time) (domain.PayrollPeriod, error)
}
