package employee

import (
	"context"
	"time"

	domain "github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type EmployeeListFilter struct {
	Search           *string
	TenantID         *organization.TenantID
	CompanyID        *organization.CompanyID
	CursorID         *domain.EmployeeID
	EmploymentStatus *domain.EmploymentStatus
	Limit            int
}

type Repository interface {
	Transaction
	WithinTransaction(context.Context, func(Transaction) error) error
}

type Transaction interface {
	CreateEmployee(context.Context, domain.Employee) (domain.Employee, error)
	GetEmployee(context.Context, domain.EmployeeID) (domain.Employee, error)
	ListEmployeeSummaries(context.Context, EmployeeListFilter) ([]domain.EmployeeSummary, error)
	UpdateEmployee(context.Context, domain.Employee) (domain.Employee, error)

	CreateEmployment(context.Context, domain.Employment) (domain.Employment, error)
	GetEmployment(context.Context, domain.EmploymentID) (domain.Employment, error)
	GetEmploymentForUpdate(context.Context, domain.EmploymentID) (domain.Employment, error)
	ListEmployments(context.Context, domain.EmployeeID) ([]domain.Employment, error)
	EndEmployment(context.Context, domain.Employment) (domain.Employment, error)

	CreateAssignment(context.Context, domain.Assignment) (domain.Assignment, error)
	GetAssignment(context.Context, domain.AssignmentID) (domain.Assignment, error)
	ListAssignments(context.Context, domain.EmploymentID) ([]domain.Assignment, error)
	GetOpenAssignmentForUpdate(context.Context, domain.EmploymentID) (domain.Assignment, error)
	CloseAssignment(context.Context, domain.Assignment) (domain.Assignment, error)

	CreateTaxProfile(context.Context, domain.TaxProfile) (domain.TaxProfile, error)
	GetTaxProfile(context.Context, domain.TaxProfileID) (domain.TaxProfile, error)
	ListTaxProfiles(context.Context, domain.EmployeeID) ([]domain.TaxProfile, error)
	GetOpenTaxProfileForUpdate(context.Context, domain.EmployeeID) (domain.TaxProfile, error)
	CloseTaxProfile(context.Context, domain.TaxProfile) (domain.TaxProfile, error)
}

type Optional[T any] struct {
	Set   bool
	Value *T
}

type CreateEmployeeInput struct {
	CompanyID      organization.CompanyID
	EmployeeNumber string
	NIK            domain.NIK
	FullName       string
	BirthPlace     *string
	BirthDate      *time.Time
	Gender         *domain.Gender
	Email          *string
	Phone          *string
	Address        *string
}

type UpdateEmployeeInput struct {
	FullName   *string
	BirthPlace Optional[string]
	BirthDate  Optional[time.Time]
	Gender     Optional[domain.Gender]
	Email      Optional[string]
	Phone      Optional[string]
	Address    Optional[string]
}

type StartEmploymentInput struct {
	EmployeeID     domain.EmployeeID
	CompanyID      organization.CompanyID
	EmploymentType domain.EmploymentType
	JoinDate       time.Time
}

type RejoinEmploymentInput struct {
	EmployeeID           domain.EmployeeID
	CompanyID            organization.CompanyID
	EmploymentType       domain.EmploymentType
	JoinDate             time.Time
	PreviousEmploymentID *domain.EmploymentID
}

type ResignEmploymentInput struct {
	EmploymentID      domain.EmploymentID
	LastWorkingDate   time.Time
	TerminationReason string
}

type CreateAssignmentInput struct {
	EmploymentID  domain.EmploymentID
	LocationID    *organization.UnitID
	DepartmentID  *organization.UnitID
	PositionID    *organization.UnitID
	GroupID       *organization.UnitID
	EffectiveFrom time.Time
}

type CreateTaxProfileInput struct {
	EmployeeID    domain.EmployeeID
	NIK           domain.NIK
	NPWP          *domain.NPWP
	PTKPCode      string
	TaxMethod     string
	EffectiveFrom time.Time
}
