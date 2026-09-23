package organization

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/navyaraksha/imogi/internal/domain/identity"
)

type CompanyID uuid.UUID
type UnitID uuid.UUID
type TenantID uuid.UUID

func NewTenantID() (TenantID, error) {
	id, err := identity.NewUUIDv7()
	return TenantID(id), err
}

func NewCompanyID() (CompanyID, error) {
	id, err := identity.NewUUIDv7()
	return CompanyID(id), err
}

func ParseCompanyID(value string) (CompanyID, error) {
	id, err := identity.ParseUUIDv7(value)
	if err != nil {
		return CompanyID(uuid.Nil), fmt.Errorf("company id: %w", err)
	}
	return CompanyID(id), nil
}

func (id CompanyID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id CompanyID) String() string  { return id.UUID().String() }

func ParseTenantID(value string) (TenantID, error) {
	id, err := identity.ParseUUIDv7(value)
	if err != nil {
		return TenantID(uuid.Nil), fmt.Errorf("tenant id: %w", err)
	}
	return TenantID(id), nil
}

func (id TenantID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id TenantID) String() string  { return id.UUID().String() }

func ParseUnitID(value string) (UnitID, error) {
	id, err := identity.ParseUUIDv7(value)
	if err != nil {
		return UnitID(uuid.Nil), fmt.Errorf("organization unit id: %w", err)
	}
	return UnitID(id), nil
}

func NewUnitID() (UnitID, error) {
	id, err := identity.NewUUIDv7()
	return UnitID(id), err
}

func (id UnitID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id UnitID) String() string  { return id.UUID().String() }
