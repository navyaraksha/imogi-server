package employee

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type EmploymentStatus string

const (
	EmploymentScheduled EmploymentStatus = "scheduled"
	EmploymentActive    EmploymentStatus = "active"
	EmploymentEnded     EmploymentStatus = "ended"
)

type Employment struct {
	ID                EmploymentID
	EmployeeID        EmployeeID
	TenantID          organization.TenantID
	CompanyID         organization.CompanyID
	EmploymentType    EmploymentType
	JoinDate          time.Time
	EndDate           *time.Time
	TerminationReason *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func NewEmployment(
	id EmploymentID,
	employeeID EmployeeID,
	companyID organization.CompanyID,
	employmentType EmploymentType,
	joinDate time.Time,
	now time.Time,
) (Employment, error) {
	if companyID.UUID().String() == "00000000-0000-0000-0000-000000000000" {
		return Employment{}, fmt.Errorf("%w: company id is required", ErrInvalidEmployment)
	}
	if employmentType != EmploymentPermanent && employmentType != EmploymentNonPermanent {
		return Employment{}, fmt.Errorf("%w: unsupported employment type", ErrInvalidEmployment)
	}
	joinDate = dateOnly(joinDate)
	now = dateTimeUTC(now)
	return Employment{
		ID:             id,
		EmployeeID:     employeeID,
		CompanyID:      companyID,
		EmploymentType: employmentType,
		JoinDate:       joinDate,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (e *Employment) Resign(lastWorkingDate time.Time, reason string, now time.Time) error {
	if e == nil {
		return ErrEmploymentNotFound
	}
	if e.EndDate != nil {
		return ErrEmploymentAlreadyEnded
	}
	lastWorkingDate = dateOnly(lastWorkingDate)
	if lastWorkingDate.Before(e.JoinDate) {
		return fmt.Errorf("%w: last working date is before join date", ErrInvalidEmployment)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 100 {
		return fmt.Errorf("%w: termination reason is required", ErrInvalidEmployment)
	}
	e.EndDate = &lastWorkingDate
	e.TerminationReason = &reason
	e.UpdatedAt = dateTimeUTC(now)
	return nil
}

func (e *Employment) BindTenant(tenantID organization.TenantID) error {
	if tenantID.UUID() == uuid.Nil {
		return fmt.Errorf("%w: tenant id is required", ErrInvalidEmployment)
	}
	e.TenantID = tenantID
	return nil
}

func (e Employment) Status(asOf time.Time) EmploymentStatus {
	asOf = dateOnly(asOf)
	if asOf.Before(e.JoinDate) {
		return EmploymentScheduled
	}
	if e.EndDate != nil && asOf.After(*e.EndDate) {
		return EmploymentEnded
	}
	return EmploymentActive
}

func (e Employment) IsOpen() bool { return e.EndDate == nil }
