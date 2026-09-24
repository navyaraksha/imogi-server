package employee

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/navyaraksha/imogi/internal/domain/identity"
)

type EmployeeID uuid.UUID
type EmploymentID uuid.UUID
type AssignmentID uuid.UUID
type TaxProfileID uuid.UUID
type EmployeeNumberHistoryID uuid.UUID

func newID() (uuid.UUID, error) { return identity.NewUUIDv7() }

func NewEmployeeID() (EmployeeID, error) {
	id, err := newID()
	return EmployeeID(id), err
}

func NewEmploymentID() (EmploymentID, error) {
	id, err := newID()
	return EmploymentID(id), err
}

func NewAssignmentID() (AssignmentID, error) {
	id, err := newID()
	return AssignmentID(id), err
}

func NewTaxProfileID() (TaxProfileID, error) {
	id, err := newID()
	return TaxProfileID(id), err
}

func NewEmployeeNumberHistoryID() (EmployeeNumberHistoryID, error) {
	id, err := newID()
	return EmployeeNumberHistoryID(id), err
}

func ParseEmployeeID(value string) (EmployeeID, error) {
	id, err := identity.ParseUUIDv7(value)
	if err != nil {
		return EmployeeID(uuid.Nil), fmt.Errorf("employee id: %w", err)
	}
	return EmployeeID(id), nil
}

func ParseEmploymentID(value string) (EmploymentID, error) {
	id, err := identity.ParseUUIDv7(value)
	if err != nil {
		return EmploymentID(uuid.Nil), fmt.Errorf("employment id: %w", err)
	}
	return EmploymentID(id), nil
}

func ParseAssignmentID(value string) (AssignmentID, error) {
	id, err := identity.ParseUUIDv7(value)
	if err != nil {
		return AssignmentID(uuid.Nil), fmt.Errorf("assignment id: %w", err)
	}
	return AssignmentID(id), nil
}

func ParseTaxProfileID(value string) (TaxProfileID, error) {
	id, err := identity.ParseUUIDv7(value)
	if err != nil {
		return TaxProfileID(uuid.Nil), fmt.Errorf("tax profile id: %w", err)
	}
	return TaxProfileID(id), nil
}

func (id EmployeeID) UUID() uuid.UUID              { return uuid.UUID(id) }
func (id EmploymentID) UUID() uuid.UUID            { return uuid.UUID(id) }
func (id AssignmentID) UUID() uuid.UUID            { return uuid.UUID(id) }
func (id TaxProfileID) UUID() uuid.UUID            { return uuid.UUID(id) }
func (id EmployeeNumberHistoryID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id EmployeeID) String() string               { return id.UUID().String() }
func (id EmploymentID) String() string             { return id.UUID().String() }
func (id AssignmentID) String() string             { return id.UUID().String() }
func (id TaxProfileID) String() string             { return id.UUID().String() }
func (id EmployeeNumberHistoryID) String() string  { return id.UUID().String() }
