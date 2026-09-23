package payroll

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/organization"
)

func TestPayrollPeriodFinalizeIsOneWay(t *testing.T) {
	id, err := NewPayrollPeriodID()
	if err != nil {
		t.Fatal(err)
	}
	tenantID := organization.TenantID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e"))
	companyID := organization.CompanyID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-2f2a3b4c5d6e"))
	period, err := NewPayrollPeriod(id, tenantID, companyID, 2025, 1, time.Date(2025, 1, 1, 9, 0, 0, 0, time.FixedZone("WIB", 7*60*60)))
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := period.Finalize(time.Date(2025, 2, 1, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != PeriodFinalized || finalized.FinalizedAt == nil {
		t.Fatalf("period was not finalized: %+v", finalized)
	}
	if _, err := finalized.Finalize(time.Now()); !errors.Is(err, ErrPayrollPeriodAlreadyFinalized) {
		t.Fatalf("second finalize error = %v", err)
	}
}

func TestEmploymentCoversPayrollMonthAtBoundaries(t *testing.T) {
	start, end, err := PeriodBounds(2025, 3)
	if err != nil {
		t.Fatal(err)
	}
	employment := EmploymentReference{JoinDate: end, EndDate: &start}
	if !employment.CoversPeriod(2025, 3) {
		t.Fatal("employment ending at period start should cover the period")
	}
	employment.JoinDate = end.AddDate(0, 0, 1)
	if employment.CoversPeriod(2025, 3) {
		t.Fatal("employment starting after period end should not cover the period")
	}
}

func TestMoneyRejectsNegativeValues(t *testing.T) {
	if _, err := NewMoney(-1); !errors.Is(err, ErrInvalidPayrollResult) {
		t.Fatalf("negative money error = %v", err)
	}
}
