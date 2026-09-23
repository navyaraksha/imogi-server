package payroll

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	domain "github.com/navyaraksha/imogi/internal/domain/payroll"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type payrollTestClock struct{ now time.Time }

func (c payrollTestClock) Now() time.Time { return c.now }

type payrollTestAuthorizer struct {
	tenantID  uuid.UUID
	companyID uuid.UUID
}

func (a payrollTestAuthorizer) Require(context.Context, string) error { return nil }
func (a payrollTestAuthorizer) RequireCompany(_ context.Context, id uuid.UUID) error {
	if id != a.companyID {
		return security.ErrForbidden
	}
	return nil
}
func (a payrollTestAuthorizer) ActiveTenant(context.Context) (uuid.UUID, error) {
	return a.tenantID, nil
}

type payrollTestRepository struct {
	periods    map[domain.PayrollPeriodID]domain.PayrollPeriod
	results    map[domain.PayrollResultID]domain.PayrollResult
	items      map[domain.PayrollResultItemID]domain.PayrollResultItem
	employment domain.EmploymentReference
	employeeID employee.EmployeeID
	companyID  organization.CompanyID
}

func newPayrollTestRepository(employment domain.EmploymentReference) *payrollTestRepository {
	return &payrollTestRepository{
		periods:    make(map[domain.PayrollPeriodID]domain.PayrollPeriod),
		results:    make(map[domain.PayrollResultID]domain.PayrollResult),
		items:      make(map[domain.PayrollResultItemID]domain.PayrollResultItem),
		employment: employment,
		employeeID: employment.EmployeeID,
		companyID:  employment.CompanyID,
	}
}

func (r *payrollTestRepository) WithinTransaction(_ context.Context, fn func(Transaction) error) error {
	return fn(r)
}
func (r *payrollTestRepository) CreatePayrollPeriod(_ context.Context, value domain.PayrollPeriod) (domain.PayrollPeriod, error) {
	r.periods[value.ID] = value
	return value, nil
}
func (r *payrollTestRepository) GetPayrollPeriod(_ context.Context, id domain.PayrollPeriodID) (domain.PayrollPeriod, error) {
	value, ok := r.periods[id]
	if !ok {
		return domain.PayrollPeriod{}, domain.ErrPayrollPeriodNotFound
	}
	return value, nil
}
func (r *payrollTestRepository) ListPayrollPeriods(context.Context, PeriodFilter) ([]domain.PayrollPeriod, error) {
	return nil, nil
}
func (r *payrollTestRepository) GetEmploymentPayrollReference(context.Context, employee.EmploymentID) (domain.EmploymentReference, error) {
	return r.employment, nil
}
func (r *payrollTestRepository) GetEmployeePayrollReference(context.Context, employee.EmployeeID) (organization.CompanyID, error) {
	return r.companyID, nil
}
func (r *payrollTestRepository) ListPayrollHistory(context.Context, employee.EmployeeID, HistoryFilter) ([]domain.PayrollHistoryEntry, error) {
	return nil, nil
}
func (r *payrollTestRepository) GetPayrollPeriodForUpdate(ctx context.Context, id domain.PayrollPeriodID) (domain.PayrollPeriod, error) {
	return r.GetPayrollPeriod(ctx, id)
}
func (r *payrollTestRepository) CreatePayrollResult(_ context.Context, value domain.PayrollResult) (domain.PayrollResult, error) {
	r.results[value.ID] = value
	return value, nil
}
func (r *payrollTestRepository) CreatePayrollResultItem(_ context.Context, value domain.PayrollResultItem) (domain.PayrollResultItem, error) {
	r.items[value.ID] = value
	return value, nil
}
func (r *payrollTestRepository) CountPayrollResults(_ context.Context, id domain.PayrollPeriodID) (int64, error) {
	var count int64
	for _, result := range r.results {
		if result.PayrollPeriodID == id {
			count++
		}
	}
	return count, nil
}
func (r *payrollTestRepository) FinalizePayrollResults(_ context.Context, id domain.PayrollPeriodID, at time.Time) error {
	for key, result := range r.results {
		if result.PayrollPeriodID == id {
			value := at
			result.FinalizedAt = &value
			result.UpdatedAt = at
			r.results[key] = result
		}
	}
	return nil
}
func (r *payrollTestRepository) FinalizePayrollPeriod(ctx context.Context, id domain.PayrollPeriodID, at time.Time) (domain.PayrollPeriod, error) {
	period, err := r.GetPayrollPeriod(ctx, id)
	if err != nil {
		return domain.PayrollPeriod{}, err
	}
	period, err = period.Finalize(at)
	if err != nil {
		return domain.PayrollPeriod{}, err
	}
	r.periods[id] = period
	return period, nil
}

func TestServiceRecordsResultAgainstEmploymentAndFinalizesHistory(t *testing.T) {
	now := time.Date(2025, 3, 15, 9, 0, 0, 0, time.UTC)
	tenantUUID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e")
	companyUUID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-2f2a3b4c5d6e")
	companyID := organization.CompanyID(companyUUID)
	employeeUUID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-3f2a3b4c5d6e")
	employmentUUID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-4f2a3b4c5d6e")
	employeeID := employee.EmployeeID(employeeUUID)
	employmentID := employee.EmploymentID(employmentUUID)
	repo := newPayrollTestRepository(domain.EmploymentReference{
		ID: employmentID, EmployeeID: employeeID, CompanyID: companyID,
		JoinDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	service, err := NewService(repo, payrollTestAuthorizer{tenantID: tenantUUID, companyID: companyUUID}, payrollTestClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	period, err := service.CreatePayrollPeriod(context.Background(), CreatePayrollPeriodInput{CompanyID: companyID, Year: 2025, Month: 3})
	if err != nil {
		t.Fatal(err)
	}
	history, err := service.RecordPayrollResult(context.Background(), CreatePayrollResultInput{
		PayrollPeriodID: period.ID,
		EmployeeID:      employeeID,
		EmploymentID:    employmentID,
		GrossIncome:     10000000,
		TaxableIncome:   9000000,
		TakeHomePay:     8500000,
		Items:           []CreatePayrollResultItemInput{{ComponentCode: "basic_salary", ComponentType: domain.ComponentEarning, Amount: 10000000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if history.Result.EmploymentID != employmentID || len(history.Items) != 1 {
		t.Fatalf("unexpected history: %+v", history)
	}
	finalized, err := service.FinalizePayrollPeriod(context.Background(), period.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != domain.PeriodFinalized || repo.results[history.Result.ID].FinalizedAt == nil {
		t.Fatal("result or period was not finalized")
	}
	_, err = service.RecordPayrollResult(context.Background(), CreatePayrollResultInput{
		PayrollPeriodID: period.ID, EmployeeID: employeeID, EmploymentID: employmentID,
		GrossIncome: 1, TaxableIncome: 1, TakeHomePay: 1,
		Items: []CreatePayrollResultItemInput{{ComponentCode: "x", ComponentType: domain.ComponentEarning, Amount: 1}},
	})
	if !errors.Is(err, domain.ErrPayrollPeriodAlreadyFinalized) {
		t.Fatalf("record after finalization error = %v", err)
	}
}

func TestServiceRejectsEmploymentOutsidePayrollMonth(t *testing.T) {
	tenantUUID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e")
	companyUUID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-2f2a3b4c5d6e")
	employeeID := employee.EmployeeID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-3f2a3b4c5d6e"))
	employmentID := employee.EmploymentID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-4f2a3b4c5d6e"))
	companyID := organization.CompanyID(companyUUID)
	repo := newPayrollTestRepository(domain.EmploymentReference{ID: employmentID, EmployeeID: employeeID, CompanyID: companyID, JoinDate: time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)})
	service, err := NewService(repo, payrollTestAuthorizer{tenantID: tenantUUID, companyID: companyUUID}, payrollTestClock{now: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	period, err := service.CreatePayrollPeriod(context.Background(), CreatePayrollPeriodInput{CompanyID: companyID, Year: 2025, Month: 3})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RecordPayrollResult(context.Background(), CreatePayrollResultInput{PayrollPeriodID: period.ID, EmployeeID: employeeID, EmploymentID: employmentID, Items: []CreatePayrollResultItemInput{{ComponentCode: "x", ComponentType: domain.ComponentEarning, Amount: 1}}})
	if !errors.Is(err, domain.ErrEmploymentOutsidePeriod) {
		t.Fatalf("outside-period error = %v", err)
	}
}
