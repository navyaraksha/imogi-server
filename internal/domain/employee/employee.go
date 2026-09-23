package employee

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type Employee struct {
	ID             EmployeeID
	TenantID       organization.TenantID
	CompanyID      organization.CompanyID
	EmployeeNumber string
	NIK            NIK
	FullName       string
	BirthPlace     *string
	BirthDate      *time.Time
	Gender         *Gender
	Email          *string
	Phone          *string
	Address        *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type EmployeeSummary struct {
	ID             EmployeeID
	TenantID       organization.TenantID
	CompanyID      organization.CompanyID
	EmployeeNumber string
	FullName       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (e *Employee) BindOwnership(tenantID organization.TenantID, companyID organization.CompanyID) error {
	if tenantID.UUID() == uuid.Nil || companyID.UUID() == uuid.Nil {
		return fmt.Errorf("%w: tenant and company are required", ErrInvalidEmployee)
	}
	e.TenantID, e.CompanyID = tenantID, companyID
	return nil
}

type PersonalData struct {
	FullName   string
	BirthPlace *string
	BirthDate  *time.Time
	Gender     *Gender
	Email      *string
	Phone      *string
	Address    *string
}

func NewEmployee(
	id EmployeeID,
	employeeNumber string,
	nik NIK,
	fullName string,
	personal PersonalData,
	now time.Time,
) (Employee, error) {
	employeeNumber, err := validateText(employeeNumber, "employee number", 100, ErrInvalidEmployee)
	if err != nil {
		return Employee{}, err
	}
	fullName, err = validateText(fullName, "full name", 200, ErrInvalidEmployee)
	if err != nil {
		return Employee{}, err
	}
	if _, err := ParseNIK(nik.String()); err != nil {
		return Employee{}, err
	}
	if err := validatePersonalData(personal); err != nil {
		return Employee{}, err
	}

	now = dateTimeUTC(now)
	return Employee{
		ID:             id,
		EmployeeNumber: employeeNumber,
		NIK:            nik,
		FullName:       fullName,
		BirthPlace:     cloneString(personal.BirthPlace),
		BirthDate:      cloneDate(personal.BirthDate),
		Gender:         cloneGender(personal.Gender),
		Email:          cloneString(personal.Email),
		Phone:          cloneString(personal.Phone),
		Address:        cloneString(personal.Address),
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (e *Employee) UpdatePersonalData(data PersonalData, now time.Time) error {
	if e == nil {
		return ErrEmployeeNotFound
	}
	if err := validatePersonalData(data); err != nil {
		return err
	}
	fullName, err := validateText(data.FullName, "full name", 200, ErrInvalidEmployee)
	if err != nil {
		return err
	}
	e.FullName = fullName
	e.BirthPlace = cloneString(data.BirthPlace)
	e.BirthDate = cloneDate(data.BirthDate)
	e.Gender = cloneGender(data.Gender)
	e.Email = cloneString(data.Email)
	e.Phone = cloneString(data.Phone)
	e.Address = cloneString(data.Address)
	e.UpdatedAt = dateTimeUTC(now)
	return nil
}

func validatePersonalData(data PersonalData) error {
	if data.BirthPlace != nil {
		if _, err := validateText(*data.BirthPlace, "birth place", 200, ErrInvalidEmployee); err != nil {
			return err
		}
	}
	if data.Email != nil {
		value, err := validateText(*data.Email, "email", 320, ErrInvalidEmployee)
		if err != nil {
			return err
		}
		if _, err := mail.ParseAddress(value); err != nil {
			return fmt.Errorf("%w: invalid email", ErrInvalidEmployee)
		}
	}
	if data.Phone != nil {
		if _, err := validateText(*data.Phone, "phone", 50, ErrInvalidEmployee); err != nil {
			return err
		}
	}
	if data.Address != nil {
		if _, err := validateText(*data.Address, "address", 2000, ErrInvalidEmployee); err != nil {
			return err
		}
	}
	return nil
}

func dateOnly(value time.Time) time.Time {
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func dateTimeUTC(value time.Time) time.Time { return value.UTC() }

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	return &v
}

func cloneDate(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := dateOnly(*value)
	return &v
}

func cloneGender(value *Gender) *Gender {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}
