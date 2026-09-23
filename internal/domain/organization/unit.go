package organization

import (
	"fmt"
	"strings"
	"time"
)

type UnitType string

const (
	UnitLocation   UnitType = "location"
	UnitDepartment UnitType = "department"
	UnitPosition   UnitType = "position"
	UnitGroup      UnitType = "group"
)

type UnitStatus string

const (
	UnitActive   UnitStatus = "active"
	UnitArchived UnitStatus = "archived"
)

type Unit struct {
	ID        UnitID
	TenantID  TenantID
	CompanyID CompanyID
	Type      UnitType
	Code      string
	Name      string
	Status    UnitStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewUnit(id UnitID, tenantID TenantID, companyID CompanyID, unitType UnitType, code, name string, now time.Time) (Unit, error) {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if !validUnitType(unitType) || code == "" || len(code) > 50 || name == "" || len(name) > 200 {
		return Unit{}, fmt.Errorf("invalid organization unit")
	}
	now = now.UTC()
	return Unit{ID: id, TenantID: tenantID, CompanyID: companyID, Type: unitType, Code: code, Name: name, Status: UnitActive, CreatedAt: now, UpdatedAt: now}, nil
}

func (u *Unit) Update(code, name string, now time.Time) error {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if code == "" || len(code) > 50 || name == "" || len(name) > 200 {
		return fmt.Errorf("invalid organization unit")
	}
	if u.Status == UnitArchived {
		return fmt.Errorf("organization unit is archived")
	}
	u.Code, u.Name, u.UpdatedAt = code, name, now.UTC()
	return nil
}

func (u *Unit) Archive(now time.Time) error {
	if u.Status == UnitArchived {
		return fmt.Errorf("organization unit is already archived")
	}
	u.Status, u.UpdatedAt = UnitArchived, now.UTC()
	return nil
}

func validUnitType(value UnitType) bool {
	return value == UnitLocation || value == UnitDepartment || value == UnitPosition || value == UnitGroup
}
