package organization

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func testIDs(t *testing.T) (TenantID, CompanyID, UnitID) {
	t.Helper()
	return TenantID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e")), CompanyID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-2f2a3b4c5d6e")), UnitID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-3f2a3b4c5d6e"))
}

func TestOrganizationLifecycleDoesNotReactivateArchivedRecords(t *testing.T) {
	tenantID, companyID, unitID := testIDs(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tenant, err := NewTenant(tenantID, "acme", "Acme", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenant.Transition(TenantArchived, now); err != nil {
		t.Fatal(err)
	}
	if err := tenant.Transition(TenantActive, now); err == nil {
		t.Fatal("archived tenant was reactivated")
	}

	company, err := NewCompany(companyID, tenantID, "ACME", "Acme Legal", "Acme", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := company.Transition(CompanyArchived, now); err != nil {
		t.Fatal(err)
	}
	if err := company.Update("ACME2", "Acme Legal", "Acme", now); err == nil {
		t.Fatal("archived company was updated")
	}

	unit, err := NewUnit(unitID, tenantID, companyID, UnitDepartment, "HR", "Human Resources", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := unit.Archive(now); err != nil {
		t.Fatal(err)
	}
	if err := unit.Archive(now); err == nil {
		t.Fatal("archived unit was archived twice")
	}
}

func TestUnitTypeIsExplicit(t *testing.T) {
	tenantID, companyID, unitID := testIDs(t)
	if _, err := NewUnit(unitID, tenantID, companyID, UnitType("unknown"), "X", "Unknown", time.Now()); err == nil {
		t.Fatal("unsupported unit type was accepted")
	}
}
