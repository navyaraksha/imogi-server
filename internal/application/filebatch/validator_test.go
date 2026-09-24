package filebatch

import (
	"context"
	"testing"

	"github.com/google/uuid"
	filedomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type identityMatcherStub struct {
	candidates []IdentityCandidate
	tenantID   uuid.UUID
	companyID  uuid.UUID
}

func (stub *identityMatcherStub) FindIdentityCandidates(_ context.Context, tenantID, companyID uuid.UUID, _, _, _ string) ([]IdentityCandidate, error) {
	stub.tenantID = tenantID
	stub.companyID = companyID
	return stub.candidates, nil
}

func TestResolveIdentityUsesHistoricalEmployeeNumber(t *testing.T) {
	tenantID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-111111111111")
	companyID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-222222222222")
	employeeID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-333333333333")
	matcher := &identityMatcherStub{candidates: []IdentityCandidate{{
		EmployeeID: employeeID, EmployeeNumber: "EMP-2019", FullName: "Ardianto",
	}}}
	validator := &Validator{matcher: matcher}
	batch := filedomain.ImportBatch{
		TenantID:  organization.TenantID(tenantID),
		CompanyID: organization.CompanyID(companyID),
	}

	status, _, issue := validator.resolveIdentity(context.Background(), batch, map[string]string{
		"employee_number": "emp 2019",
	}, 2, filedomain.OperationPayrollLedger, "Salary")
	if status != "matched" || issue != nil {
		t.Fatalf("historical number should match, status=%q issue=%v", status, issue)
	}
	if matcher.tenantID != tenantID || matcher.companyID != companyID {
		t.Fatalf("matcher scope = tenant %s company %s", matcher.tenantID, matcher.companyID)
	}
}

func TestResolveIdentityReturnsNonBlockingWarningForNewEmployeeMasterRow(t *testing.T) {
	validator := &Validator{matcher: &identityMatcherStub{}}
	batch := filedomain.ImportBatch{
		TenantID:  organization.TenantID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-111111111111")),
		CompanyID: organization.CompanyID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-222222222222")),
	}

	status, _, issue := validator.resolveIdentity(context.Background(), batch, map[string]string{
		"full_name": "Karyawan Baru",
	}, 2, filedomain.OperationEmployeeMaster, "master_employees")
	if status != "unmatched" || issue == nil {
		t.Fatalf("new employee should produce a warning, status=%q issue=%v", status, issue)
	}
	if issue.Severity != "warning" {
		t.Fatalf("severity = %q, want warning", issue.Severity)
	}
}

func TestValidationCountersDoNotBlockOnWarnings(t *testing.T) {
	issues := []validationError{{Severity: "warning"}}
	if got := countBlockingValidationErrors(issues); got != 0 {
		t.Fatalf("blocking warning count = %d", got)
	}
	if hasBlockingValidationErrors(issues) {
		t.Fatal("warning should not block ready-import artifact")
	}
	issues = append(issues, validationError{Severity: "error"})
	if !hasBlockingValidationErrors(issues) {
		t.Fatal("error should block ready-import artifact")
	}
}

func TestNormalizeRepeatedBlockRowUsesGrossFallbackAndComponents(t *testing.T) {
	row, ok, err := normalizeRepeatedBlockRow("DKM Rekap", 16, []string{
		"", "", "PONIJO", "494", "", "", "898.000", "", "", "", "", "", "", "-37.050", "860.950",
	}, repeatedBlockConfiguration{
		EmployeeNumberColumn: "D",
		FullNameColumn:       "C",
		TakeHomeColumn:       "O",
		Earnings:             []repeatedBlockComponent{{Code: "GAJI", Type: "earning", Column: "G"}},
		Deductions:           []repeatedBlockComponent{{Code: "BPJS", Type: "deduction", Column: "N"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a logical payroll row")
	}
	if row.Values["gross_income"] != "898000" || row.Values["taxable_income"] != "898000" || row.Values["take_home_pay"] != "860950" {
		t.Fatalf("canonical values = %#v", row.Values)
	}
	if len(row.Components) != 2 || row.Components[1].Amount != "-37050" {
		t.Fatalf("components = %#v", row.Components)
	}
}

func TestNormalizeRepeatedBlockRowSkipsConfiguredPlaceholder(t *testing.T) {
	_, ok, err := normalizeRepeatedBlockRow("DKM Rekap", 38, nil, repeatedBlockConfiguration{
		EmployeeNumberColumn: "D",
		FullNameColumn:       "C",
		TakeHomeColumn:       "O",
		IgnoreRows:           []repeatedBlockIgnore{{EmployeeNumber: "NF-00", FullName: "No Name"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("configured placeholder must not become a payroll row")
	}
}

func TestSensitiveBankFieldsAreExcludedFromArtifacts(t *testing.T) {
	values := rowValues([]string{"full_name", "bank", "bank_account", "bank_account_name", "nik"}, []string{"Employee", "BCA", "123456", "Employee", "3174010101010001"})
	if _, ok := values["bank_account"]; ok {
		t.Fatal("bank account must not be written to normalized artifacts")
	}
	if _, ok := values["bank_account_name"]; ok {
		t.Fatal("bank account name must not be written to normalized artifacts")
	}
	if values["nik"] != "************0001" {
		t.Fatalf("masked NIK = %q", values["nik"])
	}
}
