package employee

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

func TestEmploymentResignAndStatus(t *testing.T) {
	employmentID := EmploymentID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e"))
	employeeID := EmployeeID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-2f2a3b4c5d6e"))
	companyID := organization.CompanyID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-3f2a3b4c5d6e"))
	joinDate := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	employment, err := NewEmployment(employmentID, employeeID, companyID, EmploymentPermanent, joinDate, joinDate)
	if err != nil {
		t.Fatalf("new employment: %v", err)
	}
	if got := employment.Status(time.Date(2025, 1, 9, 0, 0, 0, 0, time.UTC)); got != EmploymentScheduled {
		t.Fatalf("status before join = %q", got)
	}
	if got := employment.Status(joinDate); got != EmploymentActive {
		t.Fatalf("status on join = %q", got)
	}
	lastWorkingDate := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)
	if err := employment.Resign(lastWorkingDate, "resignation", lastWorkingDate); err != nil {
		t.Fatalf("resign: %v", err)
	}
	if employment.EndDate == nil || !employment.EndDate.Equal(lastWorkingDate) {
		t.Fatalf("end date = %v", employment.EndDate)
	}
	if got := employment.Status(time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)); got != EmploymentEnded {
		t.Fatalf("status after end = %q", got)
	}
	if err := employment.Resign(lastWorkingDate, "again", lastWorkingDate); !errors.Is(err, ErrEmploymentAlreadyEnded) {
		t.Fatalf("second resign error = %v", err)
	}
}

func TestAssignmentRequiresOrganizationPlacementAndCannotCloseTwice(t *testing.T) {
	assignmentID := AssignmentID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-4f2a3b4c5d6e"))
	employmentID := EmploymentID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-5f2a3b4c5d6e"))
	if _, err := NewAssignment(assignmentID, employmentID, nil, nil, nil, nil, time.Now(), time.Now()); !errors.Is(err, ErrInvalidAssignment) {
		t.Fatalf("empty assignment error = %v", err)
	}
	unitID := organization.UnitID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-6f2a3b4c5d6e"))
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	assignment, err := NewAssignment(assignmentID, employmentID, &unitID, nil, nil, nil, start, start)
	if err != nil {
		t.Fatalf("new assignment: %v", err)
	}
	end := start.AddDate(0, 1, 0)
	if err := assignment.Close(end, end); err != nil {
		t.Fatalf("close assignment: %v", err)
	}
	if err := assignment.Close(end, end); !errors.Is(err, ErrInvalidAssignment) {
		t.Fatalf("second close error = %v", err)
	}
}

func TestTaxProfileStatusIsDerivedFromEffectiveDates(t *testing.T) {
	profileID := TaxProfileID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-7f2a3b4c5d6e"))
	employeeID := EmployeeID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-8f2a3b4c5d6e"))
	nik, err := ParseNIK("3174010101010001")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	profile, err := NewTaxProfile(profileID, employeeID, nik, nil, "TK/0", "gross", start, start)
	if err != nil {
		t.Fatalf("new tax profile: %v", err)
	}
	if got := profile.Status(start.AddDate(0, -1, 0)); got != TaxProfileScheduled {
		t.Fatalf("scheduled status = %q", got)
	}
	end := start.AddDate(0, 3, 0)
	profile.EffectiveTo = &end
	if got := profile.Status(end.AddDate(0, 0, 1)); got != TaxProfileHistorical {
		t.Fatalf("historical status = %q", got)
	}
}

func TestEmployeeNumberHistorySupportsTemporaryToPermanentCorrection(t *testing.T) {
	tenantID := organization.TenantID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-111111111111"))
	companyID := organization.CompanyID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-222222222222"))
	employeeID := EmployeeID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-333333333333"))
	historyID := EmployeeNumberHistoryID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-444444444444"))
	start := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	history, err := NewEmployeeNumberHistory(historyID, tenantID, companyID, employeeID, nil, "NF001", EmployeeNumberTemporary, start, EmployeeNumberSourceImport, start)
	if err != nil {
		t.Fatal(err)
	}
	if !history.IsEffectiveOn(start.AddDate(0, 1, 0)) {
		t.Fatal("temporary number should be effective before correction")
	}
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	if err := history.Close(end); err != nil {
		t.Fatal(err)
	}
	if history.IsEffectiveOn(end.AddDate(0, 0, 1)) {
		t.Fatal("closed number should not remain effective")
	}
}
