package employee

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeRepository struct {
	employees   map[domain.EmployeeID]domain.Employee
	employments map[domain.EmploymentID]domain.Employment
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{employees: make(map[domain.EmployeeID]domain.Employee), employments: make(map[domain.EmploymentID]domain.Employment)}
}

func (r *fakeRepository) WithinTransaction(ctx context.Context, fn func(Transaction) error) error {
	return fn(r)
}
func (r *fakeRepository) CreateEmployee(_ context.Context, value domain.Employee) (domain.Employee, error) {
	r.employees[value.ID] = value
	return value, nil
}
func (r *fakeRepository) GetEmployee(_ context.Context, id domain.EmployeeID) (domain.Employee, error) {
	value, ok := r.employees[id]
	if !ok {
		return domain.Employee{}, domain.ErrEmployeeNotFound
	}
	return value, nil
}
func (r *fakeRepository) ListEmployeeSummaries(context.Context, EmployeeListFilter) ([]domain.EmployeeSummary, error) {
	return nil, nil
}
func (r *fakeRepository) UpdateEmployee(_ context.Context, value domain.Employee) (domain.Employee, error) {
	r.employees[value.ID] = value
	return value, nil
}
func (r *fakeRepository) CreateEmployeeNumberHistory(context.Context, domain.EmployeeNumberHistory) (domain.EmployeeNumberHistory, error) {
	return domain.EmployeeNumberHistory{}, nil
}
func (r *fakeRepository) CloseOpenEmployeeNumberHistory(context.Context, domain.EmployeeID, time.Time, time.Time) error {
	return nil
}
func (r *fakeRepository) UpdateEmployeeNumberProjection(context.Context, domain.EmployeeID, *string, string) error {
	return nil
}
func (r *fakeRepository) CreateEmployment(_ context.Context, value domain.Employment) (domain.Employment, error) {
	r.employments[value.ID] = value
	return value, nil
}
func (r *fakeRepository) GetEmployment(_ context.Context, id domain.EmploymentID) (domain.Employment, error) {
	value, ok := r.employments[id]
	if !ok {
		return domain.Employment{}, domain.ErrEmploymentNotFound
	}
	return value, nil
}
func (r *fakeRepository) GetEmploymentForUpdate(ctx context.Context, id domain.EmploymentID) (domain.Employment, error) {
	return r.GetEmployment(ctx, id)
}
func (r *fakeRepository) ListEmployments(_ context.Context, id domain.EmployeeID) ([]domain.Employment, error) {
	items := make([]domain.Employment, 0)
	for _, value := range r.employments {
		if value.EmployeeID == id {
			items = append(items, value)
		}
	}
	return items, nil
}
func (r *fakeRepository) EndEmployment(_ context.Context, value domain.Employment) (domain.Employment, error) {
	r.employments[value.ID] = value
	return value, nil
}
func (r *fakeRepository) CreateAssignment(context.Context, domain.Assignment) (domain.Assignment, error) {
	return domain.Assignment{}, nil
}
func (r *fakeRepository) GetAssignment(context.Context, domain.AssignmentID) (domain.Assignment, error) {
	return domain.Assignment{}, domain.ErrAssignmentNotFound
}
func (r *fakeRepository) ListAssignments(context.Context, domain.EmploymentID) ([]domain.Assignment, error) {
	return []domain.Assignment{}, nil
}
func (r *fakeRepository) GetOpenAssignmentForUpdate(context.Context, domain.EmploymentID) (domain.Assignment, error) {
	return domain.Assignment{}, domain.ErrAssignmentNotFound
}
func (r *fakeRepository) CloseAssignment(context.Context, domain.Assignment) (domain.Assignment, error) {
	return domain.Assignment{}, nil
}
func (r *fakeRepository) CreateTaxProfile(context.Context, domain.TaxProfile) (domain.TaxProfile, error) {
	return domain.TaxProfile{}, nil
}
func (r *fakeRepository) GetTaxProfile(context.Context, domain.TaxProfileID) (domain.TaxProfile, error) {
	return domain.TaxProfile{}, domain.ErrTaxProfileNotFound
}
func (r *fakeRepository) ListTaxProfiles(context.Context, domain.EmployeeID) ([]domain.TaxProfile, error) {
	return []domain.TaxProfile{}, nil
}
func (r *fakeRepository) GetOpenTaxProfileForUpdate(context.Context, domain.EmployeeID) (domain.TaxProfile, error) {
	return domain.TaxProfile{}, domain.ErrTaxProfileNotFound
}
func (r *fakeRepository) CloseTaxProfile(context.Context, domain.TaxProfile) (domain.TaxProfile, error) {
	return domain.TaxProfile{}, nil
}

func newServiceFixture(t *testing.T) (*Service, *fakeRepository, domain.EmployeeID, organization.CompanyID, time.Time) {
	t.Helper()
	repo := newFakeRepository()
	now := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)
	service, err := NewService(repo, security.AllowAll{}, fixedClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	employeeID, err := domain.NewEmployeeID()
	if err != nil {
		t.Fatal(err)
	}
	companyUUID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	companyID := organization.CompanyID(companyUUID)
	nik, err := domain.ParseNIK("3174010101010001")
	if err != nil {
		t.Fatal(err)
	}
	entity, err := domain.NewEmployee(employeeID, "EMP-1", nik, "Ardianto", domain.PersonalData{FullName: "Ardianto"}, now)
	if err != nil {
		t.Fatal(err)
	}
	repo.employees[employeeID] = entity
	return service, repo, employeeID, companyID, now
}

func TestRejoinCreatesNewEmploymentAndPreservesHistory(t *testing.T) {
	service, repo, employeeID, companyID, now := newServiceFixture(t)
	firstID, err := domain.NewEmploymentID()
	if err != nil {
		t.Fatal(err)
	}
	first, err := domain.NewEmployment(firstID, employeeID, companyID, domain.EmploymentPermanent, now.AddDate(-2, 0, 0), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Resign(now.AddDate(-1, 0, 0), "resignation", now); err != nil {
		t.Fatal(err)
	}
	repo.employments[first.ID] = first

	rejoined, err := service.RejoinEmployee(context.Background(), RejoinEmploymentInput{
		EmployeeID: employeeID, CompanyID: companyID, EmploymentType: domain.EmploymentPermanent, JoinDate: now,
		PreviousEmploymentID: &firstID,
	})
	if err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	if rejoined.ID == first.ID {
		t.Fatal("rejoin reused ended employment id")
	}
	if repo.employments[first.ID].EndDate == nil {
		t.Fatal("historical employment was changed")
	}
}

func TestResignEndsEmploymentWithoutMutatingOtherHistory(t *testing.T) {
	service, repo, employeeID, companyID, now := newServiceFixture(t)
	employmentID, err := domain.NewEmploymentID()
	if err != nil {
		t.Fatal(err)
	}
	employment, err := domain.NewEmployment(employmentID, employeeID, companyID, domain.EmploymentPermanent, now.AddDate(-1, 0, 0), now)
	if err != nil {
		t.Fatal(err)
	}
	repo.employments[employmentID] = employment
	ended, err := service.ResignEmployment(context.Background(), ResignEmploymentInput{EmploymentID: employmentID, LastWorkingDate: now, TerminationReason: "resignation"})
	if err != nil {
		t.Fatalf("resign: %v", err)
	}
	expectedEndDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	if ended.EndDate == nil || !ended.EndDate.Equal(expectedEndDate) {
		t.Fatalf("unexpected end date: %v", ended.EndDate)
	}
	if _, err := service.ResignEmployment(context.Background(), ResignEmploymentInput{EmploymentID: employmentID, LastWorkingDate: now, TerminationReason: "again"}); !errors.Is(err, domain.ErrEmploymentAlreadyEnded) {
		t.Fatalf("second resign error = %v", err)
	}
}

var _ clock.Clock = fixedClock{}
