package employee

import (
	"fmt"
	"time"

	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type AssignmentStatus string

const (
	AssignmentScheduled  AssignmentStatus = "scheduled"
	AssignmentCurrent    AssignmentStatus = "current"
	AssignmentHistorical AssignmentStatus = "historical"
)

type Assignment struct {
	ID            AssignmentID
	EmploymentID  EmploymentID
	LocationID    *organization.UnitID
	DepartmentID  *organization.UnitID
	PositionID    *organization.UnitID
	GroupID       *organization.UnitID
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewAssignment(
	id AssignmentID,
	employmentID EmploymentID,
	locationID, departmentID, positionID, groupID *organization.UnitID,
	effectiveFrom time.Time,
	now time.Time,
) (Assignment, error) {
	if locationID == nil && departmentID == nil && positionID == nil && groupID == nil {
		return Assignment{}, fmt.Errorf("%w: at least one organizational unit is required", ErrInvalidAssignment)
	}
	return Assignment{
		ID:            id,
		EmploymentID:  employmentID,
		LocationID:    cloneUnitID(locationID),
		DepartmentID:  cloneUnitID(departmentID),
		PositionID:    cloneUnitID(positionID),
		GroupID:       cloneUnitID(groupID),
		EffectiveFrom: dateOnly(effectiveFrom),
		CreatedAt:     dateTimeUTC(now),
		UpdatedAt:     dateTimeUTC(now),
	}, nil
}

func (a *Assignment) Close(effectiveTo time.Time, now time.Time) error {
	if a == nil {
		return ErrAssignmentNotFound
	}
	effectiveTo = dateOnly(effectiveTo)
	if a.EffectiveTo != nil {
		return fmt.Errorf("%w: assignment is already closed", ErrInvalidAssignment)
	}
	if effectiveTo.Before(a.EffectiveFrom) {
		return fmt.Errorf("%w: effective end is before effective start", ErrInvalidAssignment)
	}
	a.EffectiveTo = &effectiveTo
	a.UpdatedAt = dateTimeUTC(now)
	return nil
}

func (a Assignment) Status(asOf time.Time) AssignmentStatus {
	asOf = dateOnly(asOf)
	if asOf.Before(a.EffectiveFrom) {
		return AssignmentScheduled
	}
	if a.EffectiveTo != nil && asOf.After(*a.EffectiveTo) {
		return AssignmentHistorical
	}
	return AssignmentCurrent
}

func cloneUnitID(value *organization.UnitID) *organization.UnitID {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}
