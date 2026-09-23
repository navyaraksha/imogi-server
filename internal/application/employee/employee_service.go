package employee

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type Service struct {
	repository Repository
	authorizer security.Authorizer
	clock      clock.Clock
}

func NewService(repository Repository, authorizer security.Authorizer, systemClock clock.Clock) (*Service, error) {
	if repository == nil || authorizer == nil || systemClock == nil {
		return nil, errors.New("employee service dependencies are required")
	}
	return &Service{repository: repository, authorizer: authorizer, clock: systemClock}, nil
}

func (s *Service) CreateEmployee(ctx context.Context, input CreateEmployeeInput) (domain.Employee, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmployeeCreate); err != nil {
		return domain.Employee{}, err
	}
	if err := s.authorizer.Require(ctx, security.CapabilityEmployeeWriteIdentity); err != nil {
		return domain.Employee{}, err
	}
	if input.CompanyID.UUID() == uuid.Nil {
		return domain.Employee{}, fmt.Errorf("%w: company id is required", domain.ErrInvalidEmployee)
	}
	id, err := domain.NewEmployeeID()
	if err != nil {
		return domain.Employee{}, fmt.Errorf("generate employee id: %w", err)
	}
	personalData := domain.PersonalData{
		FullName:   input.FullName,
		BirthPlace: input.BirthPlace,
		BirthDate:  input.BirthDate,
		Gender:     input.Gender,
		Email:      input.Email,
		Phone:      input.Phone,
		Address:    input.Address,
	}
	entity, err := domain.NewEmployee(id, input.EmployeeNumber, input.NIK, input.FullName, personalData, s.clock.Now())
	if err != nil {
		return domain.Employee{}, err
	}
	tenantID, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return domain.Employee{}, err
	}
	if err := s.authorizer.RequireCompany(ctx, input.CompanyID.UUID()); err != nil {
		return domain.Employee{}, err
	}
	if tenantID != uuid.Nil {
		if err := entity.BindOwnership(organization.TenantID(tenantID), input.CompanyID); err != nil {
			return domain.Employee{}, err
		}
	}
	return s.repository.CreateEmployee(ctx, entity)
}

func (s *Service) GetEmployee(ctx context.Context, id domain.EmployeeID) (domain.Employee, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmployeeReadBasic); err != nil {
		return domain.Employee{}, err
	}
	entity, err := s.repository.GetEmployee(ctx, id)
	if err != nil {
		return domain.Employee{}, err
	}
	if err := s.requireEmployeeTenant(ctx, entity); err != nil {
		return domain.Employee{}, err
	}
	return entity, nil
}

func (s *Service) GetEmployeeIdentity(ctx context.Context, id domain.EmployeeID) (domain.Employee, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmployeeReadIdentity); err != nil {
		return domain.Employee{}, err
	}
	entity, err := s.repository.GetEmployee(ctx, id)
	if err != nil {
		return domain.Employee{}, err
	}
	if err := s.requireEmployeeTenant(ctx, entity); err != nil {
		return domain.Employee{}, err
	}
	if err := s.requireEmployeeCompanyAccess(ctx, id); err != nil {
		return domain.Employee{}, err
	}
	return entity, nil
}

func (s *Service) ListEmployees(ctx context.Context, filter EmployeeListFilter) ([]domain.EmployeeSummary, string, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmployeeReadBasic); err != nil {
		return nil, "", err
	}
	if tenantID, err := security.ActiveTenant(ctx, s.authorizer); err == nil && tenantID != uuid.Nil {
		tenant := organization.TenantID(tenantID)
		filter.TenantID = &tenant
	} else if err != nil {
		return nil, "", err
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}
	queryFilter := filter
	queryFilter.Limit++
	items, err := s.repository.ListEmployeeSummaries(ctx, queryFilter)
	if err != nil {
		return nil, "", err
	}
	if len(items) <= filter.Limit {
		return items, "", nil
	}
	items = items[:filter.Limit]
	return items, encodeCursor(items[len(items)-1].ID), nil
}

func (s *Service) UpdateEmployeePersonalData(ctx context.Context, id domain.EmployeeID, input UpdateEmployeeInput) (domain.Employee, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmployeeUpdatePersonal); err != nil {
		return domain.Employee{}, err
	}
	entity, err := s.repository.GetEmployee(ctx, id)
	if err != nil {
		return domain.Employee{}, err
	}
	if err := s.requireEmployeeTenant(ctx, entity); err != nil {
		return domain.Employee{}, err
	}
	personalData := mergePersonalData(entity, input)
	if err := entity.UpdatePersonalData(personalData, s.clock.Now()); err != nil {
		return domain.Employee{}, err
	}
	return s.repository.UpdateEmployee(ctx, entity)
}

func mergePersonalData(entity domain.Employee, input UpdateEmployeeInput) domain.PersonalData {
	personalData := domain.PersonalData{
		FullName:   entity.FullName,
		BirthPlace: entity.BirthPlace,
		BirthDate:  entity.BirthDate,
		Gender:     entity.Gender,
		Email:      entity.Email,
		Phone:      entity.Phone,
		Address:    entity.Address,
	}
	if input.FullName != nil {
		personalData.FullName = *input.FullName
	}
	if input.BirthPlace.Set {
		personalData.BirthPlace = input.BirthPlace.Value
	}
	if input.BirthDate.Set {
		personalData.BirthDate = input.BirthDate.Value
	}
	if input.Gender.Set {
		personalData.Gender = input.Gender.Value
	}
	if input.Email.Set {
		personalData.Email = input.Email.Value
	}
	if input.Phone.Set {
		personalData.Phone = input.Phone.Value
	}
	if input.Address.Set {
		personalData.Address = input.Address.Value
	}
	return personalData
}

func (s *Service) StartEmployment(ctx context.Context, input StartEmploymentInput) (domain.Employment, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmploymentStart); err != nil {
		return domain.Employment{}, err
	}
	if err := s.authorizer.RequireCompany(ctx, input.CompanyID.UUID()); err != nil {
		return domain.Employment{}, err
	}
	employee, err := s.loadEmployeeForEmployment(ctx, input.EmployeeID, input.CompanyID)
	if err != nil {
		return domain.Employment{}, err
	}
	entity, err := s.newEmployment(input)
	if err != nil {
		return domain.Employment{}, err
	}
	if employee.TenantID.UUID() != uuid.Nil {
		if err := entity.BindTenant(employee.TenantID); err != nil {
			return domain.Employment{}, err
		}
	}
	return s.repository.CreateEmployment(ctx, entity)
}

func (s *Service) RejoinEmployee(ctx context.Context, input RejoinEmploymentInput) (domain.Employment, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmploymentRejoin); err != nil {
		return domain.Employment{}, err
	}
	if err := s.authorizer.RequireCompany(ctx, input.CompanyID.UUID()); err != nil {
		return domain.Employment{}, err
	}
	employee, err := s.loadEmployeeForEmployment(ctx, input.EmployeeID, input.CompanyID)
	if err != nil {
		return domain.Employment{}, err
	}
	history, err := s.repository.ListEmployments(ctx, input.EmployeeID)
	if err != nil {
		return domain.Employment{}, err
	}
	if err := validateRejoinHistory(history, input.PreviousEmploymentID); err != nil {
		return domain.Employment{}, err
	}
	entity, err := s.newEmployment(StartEmploymentInput{
		EmployeeID:     input.EmployeeID,
		CompanyID:      input.CompanyID,
		EmploymentType: input.EmploymentType,
		JoinDate:       input.JoinDate,
	})
	if err != nil {
		return domain.Employment{}, err
	}
	if employee.TenantID.UUID() != uuid.Nil {
		if err := entity.BindTenant(employee.TenantID); err != nil {
			return domain.Employment{}, err
		}
	}
	return s.repository.CreateEmployment(ctx, entity)
}

func (s *Service) loadEmployeeForEmployment(ctx context.Context, employeeID domain.EmployeeID, companyID organization.CompanyID) (domain.Employee, error) {
	employee, err := s.repository.GetEmployee(ctx, employeeID)
	if err != nil {
		return domain.Employee{}, err
	}
	if err := s.requireEmployeeTenant(ctx, employee); err != nil {
		return domain.Employee{}, err
	}
	if employee.CompanyID.UUID() != uuid.Nil && employee.CompanyID != companyID {
		return domain.Employee{}, fmt.Errorf("%w: employment company differs from employee company", domain.ErrInvalidEmployment)
	}
	return employee, nil
}

func (s *Service) newEmployment(input StartEmploymentInput) (domain.Employment, error) {
	employmentID, err := domain.NewEmploymentID()
	if err != nil {
		return domain.Employment{}, fmt.Errorf("generate employment id: %w", err)
	}
	return domain.NewEmployment(
		employmentID,
		input.EmployeeID,
		input.CompanyID,
		input.EmploymentType,
		input.JoinDate,
		s.clock.Now(),
	)
}

func validateRejoinHistory(history []domain.Employment, previousEmploymentID *domain.EmploymentID) error {
	if len(history) == 0 {
		return domain.ErrNoPreviousEmployment
	}

	for _, employment := range history {
		if employment.IsOpen() {
			return domain.ErrActiveEmploymentExists
		}
	}

	if previousEmploymentID == nil {
		return nil
	}

	for _, employment := range history {
		if employment.ID == *previousEmploymentID {
			return nil
		}
	}
	return domain.ErrEmploymentNotFound
}

func (s *Service) GetEmployment(ctx context.Context, id domain.EmploymentID) (domain.Employment, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmploymentRead); err != nil {
		return domain.Employment{}, err
	}
	employment, err := s.repository.GetEmployment(ctx, id)
	if err != nil {
		return domain.Employment{}, err
	}
	if err := s.requireTenant(ctx, employment.TenantID); err != nil {
		return domain.Employment{}, err
	}
	if err := s.authorizer.RequireCompany(ctx, employment.CompanyID.UUID()); err != nil {
		return domain.Employment{}, err
	}
	return employment, nil
}

func (s *Service) ListEmployments(ctx context.Context, id domain.EmployeeID) ([]domain.Employment, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmploymentRead); err != nil {
		return nil, err
	}
	items, err := s.repository.ListEmployments(ctx, id)
	if err != nil {
		return nil, err
	}
	filtered := make([]domain.Employment, 0, len(items))
	for _, item := range items {
		if s.requireTenant(ctx, item.TenantID) == nil && s.authorizer.RequireCompany(ctx, item.CompanyID.UUID()) == nil {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *Service) ResignEmployment(ctx context.Context, input ResignEmploymentInput) (domain.Employment, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityEmploymentResign); err != nil {
		return domain.Employment{}, err
	}
	resignedEmployment := domain.Employment{}
	err := s.repository.WithinTransaction(ctx, func(tx Transaction) error {
		employment, err := tx.GetEmploymentForUpdate(ctx, input.EmploymentID)
		if err != nil {
			return err
		}
		if err := s.requireTenant(ctx, employment.TenantID); err != nil {
			return err
		}
		if err := s.authorizer.RequireCompany(ctx, employment.CompanyID.UUID()); err != nil {
			return err
		}
		if err := employment.Resign(input.LastWorkingDate, input.TerminationReason, s.clock.Now()); err != nil {
			return err
		}
		assignment, err := tx.GetOpenAssignmentForUpdate(ctx, employment.ID)
		if err == nil {
			if !assignment.EffectiveFrom.After(*employment.EndDate) {
				if err := assignment.Close(*employment.EndDate, s.clock.Now()); err != nil {
					return err
				}
				if _, err := tx.CloseAssignment(ctx, assignment); err != nil {
					return err
				}
			} else {
				return domain.ErrAssignmentOutsideEmployment
			}
		} else if !errors.Is(err, domain.ErrAssignmentNotFound) {
			return err
		}
		resignedEmployment, err = tx.EndEmployment(ctx, employment)
		return err
	})
	return resignedEmployment, err
}

func (s *Service) ListAssignments(ctx context.Context, id domain.EmploymentID) ([]domain.Assignment, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityAssignmentRead); err != nil {
		return nil, err
	}
	employment, err := s.repository.GetEmployment(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireTenant(ctx, employment.TenantID); err != nil {
		return nil, err
	}
	if err := s.authorizer.RequireCompany(ctx, employment.CompanyID.UUID()); err != nil {
		return nil, err
	}
	return s.repository.ListAssignments(ctx, id)
}

func (s *Service) GetAssignment(ctx context.Context, id domain.AssignmentID) (domain.Assignment, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityAssignmentRead); err != nil {
		return domain.Assignment{}, err
	}
	assignment, err := s.repository.GetAssignment(ctx, id)
	if err != nil {
		return domain.Assignment{}, err
	}
	employment, err := s.repository.GetEmployment(ctx, assignment.EmploymentID)
	if err != nil {
		return domain.Assignment{}, err
	}
	if err := s.requireTenant(ctx, employment.TenantID); err != nil {
		return domain.Assignment{}, err
	}
	if err := s.authorizer.RequireCompany(ctx, employment.CompanyID.UUID()); err != nil {
		return domain.Assignment{}, err
	}
	return assignment, nil
}

func (s *Service) CreateAssignment(ctx context.Context, input CreateAssignmentInput) (domain.Assignment, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityAssignmentWrite); err != nil {
		return domain.Assignment{}, err
	}
	createdAssignment := domain.Assignment{}
	err := s.repository.WithinTransaction(ctx, func(tx Transaction) error {
		employment, err := tx.GetEmployment(ctx, input.EmploymentID)
		if err != nil {
			return err
		}
		if err := s.requireTenant(ctx, employment.TenantID); err != nil {
			return err
		}
		if err := s.authorizer.RequireCompany(ctx, employment.CompanyID.UUID()); err != nil {
			return err
		}
		if employment.Status(s.clock.Now()) == domain.EmploymentEnded {
			return domain.ErrEmploymentAlreadyEnded
		}
		assignment, err := tx.GetOpenAssignmentForUpdate(ctx, input.EmploymentID)
		if err == nil {
			closeDate := dateOnly(input.EffectiveFrom).AddDate(0, 0, -1)
			if closeDate.Before(assignment.EffectiveFrom) {
				return domain.ErrAssignmentOverlap
			}
			if err := assignment.Close(closeDate, s.clock.Now()); err != nil {
				return err
			}
			if _, err := tx.CloseAssignment(ctx, assignment); err != nil {
				return err
			}
		} else if !errors.Is(err, domain.ErrAssignmentNotFound) {
			return err
		}
		id, err := domain.NewAssignmentID()
		if err != nil {
			return err
		}
		entity, err := domain.NewAssignment(id, input.EmploymentID, input.LocationID, input.DepartmentID, input.PositionID, input.GroupID, input.EffectiveFrom, s.clock.Now())
		if err != nil {
			return err
		}
		createdAssignment, err = tx.CreateAssignment(ctx, entity)
		return err
	})
	return createdAssignment, err
}

func (s *Service) ListTaxProfiles(ctx context.Context, id domain.EmployeeID) ([]domain.TaxProfile, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityTaxProfileRead); err != nil {
		return nil, err
	}
	employee, err := s.repository.GetEmployee(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireEmployeeTenant(ctx, employee); err != nil {
		return nil, err
	}
	if err := s.requireEmployeeCompanyAccess(ctx, id); err != nil {
		return nil, err
	}
	return s.repository.ListTaxProfiles(ctx, id)
}

func (s *Service) GetTaxProfile(ctx context.Context, id domain.TaxProfileID) (domain.TaxProfile, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityTaxProfileRead); err != nil {
		return domain.TaxProfile{}, err
	}
	profile, err := s.repository.GetTaxProfile(ctx, id)
	if err != nil {
		return domain.TaxProfile{}, err
	}
	if err := s.requireTenant(ctx, profile.TenantID); err != nil {
		return domain.TaxProfile{}, err
	}
	if err := s.requireEmployeeCompanyAccess(ctx, profile.EmployeeID); err != nil {
		return domain.TaxProfile{}, err
	}
	return profile, nil
}

func (s *Service) CreateTaxProfile(ctx context.Context, input CreateTaxProfileInput) (domain.TaxProfile, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityTaxProfileWrite); err != nil {
		return domain.TaxProfile{}, err
	}
	employee, err := s.repository.GetEmployee(ctx, input.EmployeeID)
	if err != nil {
		return domain.TaxProfile{}, err
	}
	if err := s.requireEmployeeTenant(ctx, employee); err != nil {
		return domain.TaxProfile{}, err
	}
	if err := s.requireEmployeeCompanyAccess(ctx, input.EmployeeID); err != nil {
		return domain.TaxProfile{}, err
	}
	createdTaxProfile := domain.TaxProfile{}
	err = s.repository.WithinTransaction(ctx, func(tx Transaction) error {
		profile, err := tx.GetOpenTaxProfileForUpdate(ctx, input.EmployeeID)
		if err == nil {
			closeDate := dateOnly(input.EffectiveFrom).AddDate(0, 0, -1)
			if closeDate.Before(profile.EffectiveFrom) {
				return domain.ErrTaxProfileOverlap
			}
			profile.EffectiveTo = &closeDate
			profile.UpdatedAt = s.clock.Now()
			if _, err := tx.CloseTaxProfile(ctx, profile); err != nil {
				return err
			}
		} else if !errors.Is(err, domain.ErrTaxProfileNotFound) {
			return err
		}
		id, err := domain.NewTaxProfileID()
		if err != nil {
			return err
		}
		entity, err := domain.NewTaxProfile(id, input.EmployeeID, input.NIK, input.NPWP, input.PTKPCode, input.TaxMethod, input.EffectiveFrom, s.clock.Now())
		if err != nil {
			return err
		}
		if employee, employeeErr := tx.GetEmployee(ctx, input.EmployeeID); employeeErr == nil && employee.TenantID.UUID() != uuid.Nil {
			if err := entity.BindOwnership(employee.TenantID, employee.CompanyID); err != nil {
				return err
			}
		}
		createdTaxProfile, err = tx.CreateTaxProfile(ctx, entity)
		return err
	})
	return createdTaxProfile, err
}

func (s *Service) requireEmployeeCompanyAccess(ctx context.Context, employeeID domain.EmployeeID) error {
	employments, err := s.repository.ListEmployments(ctx, employeeID)
	if err != nil {
		return err
	}
	// A person can be created before their first employment. There is no
	// company resource to authorize in that state, so capability and tenant
	// authorization remain the boundary until an employment exists.
	if len(employments) == 0 {
		return nil
	}
	for _, employment := range employments {
		if s.authorizer.RequireCompany(ctx, employment.CompanyID.UUID()) == nil {
			return nil
		}
	}
	return security.ErrForbidden
}

func (s *Service) requireEmployeeTenant(ctx context.Context, employee domain.Employee) error {
	return s.requireTenant(ctx, employee.TenantID)
}

func (s *Service) requireTenant(ctx context.Context, tenantID organization.TenantID) error {
	if tenantID.UUID() == uuid.Nil {
		return nil
	}
	active, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return err
	}
	if active != uuid.Nil && active != tenantID.UUID() {
		return security.ErrForbidden
	}
	return nil
}

func encodeCursor(id domain.EmployeeID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String()))
}

func DecodeEmployeeCursor(value string) (domain.EmployeeID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return domain.EmployeeID{}, fmt.Errorf("%w: %v", domain.ErrInvalidCursor, err)
	}
	return domain.ParseEmployeeID(string(decoded))
}

func dateOnly(value time.Time) time.Time {
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
