package payroll

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	domain "github.com/navyaraksha/imogi/internal/domain/payroll"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type Service struct {
	repository Repository
	authorizer security.Authorizer
	clock      clock.Clock
}

func NewService(repository Repository, authorizer security.Authorizer, systemClock clock.Clock) (*Service, error) {
	if repository == nil || authorizer == nil || systemClock == nil {
		return nil, errors.New("payroll service dependencies are required")
	}
	return &Service{repository: repository, authorizer: authorizer, clock: systemClock}, nil
}

func (s *Service) CreatePayrollPeriod(ctx context.Context, input CreatePayrollPeriodInput) (domain.PayrollPeriod, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPayrollPeriodWrite); err != nil {
		return domain.PayrollPeriod{}, err
	}
	if err := s.requireCompany(ctx, input.CompanyID); err != nil {
		return domain.PayrollPeriod{}, err
	}
	tenantID, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return domain.PayrollPeriod{}, err
	}
	periodID, err := domain.NewPayrollPeriodID()
	if err != nil {
		return domain.PayrollPeriod{}, fmt.Errorf("generate payroll period id: %w", err)
	}
	period, err := domain.NewPayrollPeriod(
		periodID,
		organization.TenantID(tenantID),
		input.CompanyID,
		input.Year,
		input.Month,
		s.clock.Now(),
	)
	if err != nil {
		return domain.PayrollPeriod{}, err
	}
	return s.repository.CreatePayrollPeriod(ctx, period)
}

func (s *Service) GetPayrollPeriod(ctx context.Context, id domain.PayrollPeriodID) (domain.PayrollPeriod, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPayrollPeriodRead); err != nil {
		return domain.PayrollPeriod{}, err
	}
	period, err := s.repository.GetPayrollPeriod(ctx, id)
	if err != nil {
		return domain.PayrollPeriod{}, err
	}
	if err := s.requirePeriodAccess(ctx, period); err != nil {
		return domain.PayrollPeriod{}, err
	}
	return period, nil
}

func (s *Service) ListPayrollPeriods(ctx context.Context, filter PeriodFilter) ([]domain.PayrollPeriod, string, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPayrollPeriodRead); err != nil {
		return nil, "", err
	}
	if err := s.requireCompany(ctx, filter.CompanyID); err != nil {
		return nil, "", err
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}
	tenantID, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return nil, "", err
	}
	filter.TenantID = organization.TenantID(tenantID)
	queryFilter := filter
	queryFilter.Limit++
	items, err := s.repository.ListPayrollPeriods(ctx, queryFilter)
	if err != nil {
		return nil, "", err
	}
	if len(items) <= filter.Limit {
		return items, "", nil
	}
	items = items[:filter.Limit]
	return items, encodePeriodCursor(items[len(items)-1].ID), nil
}

func (s *Service) RecordPayrollResult(ctx context.Context, input CreatePayrollResultInput) (domain.PayrollHistoryEntry, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPayrollResultWrite); err != nil {
		return domain.PayrollHistoryEntry{}, err
	}
	period, err := s.repository.GetPayrollPeriod(ctx, input.PayrollPeriodID)
	if err != nil {
		return domain.PayrollHistoryEntry{}, err
	}
	if err := s.requirePeriodAccess(ctx, period); err != nil {
		return domain.PayrollHistoryEntry{}, err
	}
	if period.IsFinalized() {
		return domain.PayrollHistoryEntry{}, domain.ErrPayrollPeriodAlreadyFinalized
	}
	employment, err := s.repository.GetEmploymentPayrollReference(ctx, input.EmploymentID)
	if err != nil {
		return domain.PayrollHistoryEntry{}, err
	}
	if employment.EmployeeID != input.EmployeeID || employment.CompanyID != period.CompanyID {
		return domain.PayrollHistoryEntry{}, fmt.Errorf("%w: employee, employment, and period do not match", domain.ErrInvalidPayrollResult)
	}
	if !employment.CoversPeriod(period.Year, period.Month) {
		return domain.PayrollHistoryEntry{}, domain.ErrEmploymentOutsidePeriod
	}
	if len(input.Items) == 0 {
		return domain.PayrollHistoryEntry{}, fmt.Errorf("%w: at least one payroll item is required", domain.ErrInvalidPayrollResult)
	}

	resultID, err := domain.NewPayrollResultID()
	if err != nil {
		return domain.PayrollHistoryEntry{}, fmt.Errorf("generate payroll result id: %w", err)
	}
	result, err := domain.NewPayrollResult(
		resultID,
		period.TenantID,
		period.CompanyID,
		period.ID,
		input.EmployeeID,
		input.EmploymentID,
		input.GrossIncome,
		input.TaxableIncome,
		input.TakeHomePay,
		s.clock.Now(),
	)
	if err != nil {
		return domain.PayrollHistoryEntry{}, err
	}
	items, err := s.newResultItems(result.ID, input.Items)
	if err != nil {
		return domain.PayrollHistoryEntry{}, err
	}

	var createdResult domain.PayrollResult
	createdItems := make([]domain.PayrollResultItem, 0, len(items))
	err = s.repository.WithinTransaction(ctx, func(tx Transaction) error {
		createdResult, err = tx.CreatePayrollResult(ctx, result)
		if err != nil {
			return err
		}
		for _, item := range items {
			createdItem, itemErr := tx.CreatePayrollResultItem(ctx, item)
			if itemErr != nil {
				return itemErr
			}
			createdItems = append(createdItems, createdItem)
		}
		return nil
	})
	if err != nil {
		return domain.PayrollHistoryEntry{}, err
	}
	return domain.PayrollHistoryEntry{Result: createdResult, Period: period, Items: createdItems}, nil
}

func (s *Service) FinalizePayrollPeriod(ctx context.Context, id domain.PayrollPeriodID) (domain.PayrollPeriod, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPayrollFinalize); err != nil {
		return domain.PayrollPeriod{}, err
	}
	var finalized domain.PayrollPeriod
	err := s.repository.WithinTransaction(ctx, func(tx Transaction) error {
		period, err := tx.GetPayrollPeriodForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := s.requirePeriodAccess(ctx, period); err != nil {
			return err
		}
		if period.IsFinalized() {
			return domain.ErrPayrollPeriodAlreadyFinalized
		}
		count, err := tx.CountPayrollResults(ctx, id)
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: payroll period has no results", domain.ErrInvalidPayrollPeriod)
		}
		finalizedPeriod, err := period.Finalize(s.clock.Now())
		if err != nil {
			return err
		}
		if err := tx.FinalizePayrollResults(ctx, id, *finalizedPeriod.FinalizedAt); err != nil {
			return err
		}
		finalized, err = tx.FinalizePayrollPeriod(ctx, id, *finalizedPeriod.FinalizedAt)
		return err
	})
	return finalized, err
}

func (s *Service) ListPayrollHistory(ctx context.Context, employeeID employee.EmployeeID, filter HistoryFilter) ([]domain.PayrollHistoryEntry, string, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPayrollResultRead); err != nil {
		return nil, "", err
	}
	companyID, err := s.repository.GetEmployeePayrollReference(ctx, employeeID)
	if err != nil {
		return nil, "", err
	}
	if err := s.requireCompany(ctx, companyID); err != nil {
		return nil, "", err
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}
	tenantID, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return nil, "", err
	}
	filter.TenantID = organization.TenantID(tenantID)
	queryFilter := filter
	queryFilter.Limit++
	items, err := s.repository.ListPayrollHistory(ctx, employeeID, queryFilter)
	if err != nil {
		return nil, "", err
	}
	if len(items) <= filter.Limit {
		return items, "", nil
	}
	items = items[:filter.Limit]
	return items, encodeResultCursor(items[len(items)-1].Result.ID), nil
}

func (s *Service) newResultItems(resultID domain.PayrollResultID, inputs []CreatePayrollResultItemInput) ([]domain.PayrollResultItem, error) {
	items := make([]domain.PayrollResultItem, 0, len(inputs))
	seenCodes := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		itemID, err := domain.NewPayrollResultItemID()
		if err != nil {
			return nil, fmt.Errorf("generate payroll result item id: %w", err)
		}
		item, err := domain.NewPayrollResultItem(itemID, resultID, input.ComponentCode, input.ComponentType, input.Amount, s.clock.Now())
		if err != nil {
			return nil, err
		}
		if _, exists := seenCodes[item.ComponentCode]; exists {
			return nil, fmt.Errorf("%w: duplicate component code %s", domain.ErrInvalidPayrollComponent, item.ComponentCode)
		}
		seenCodes[item.ComponentCode] = struct{}{}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) requirePeriodAccess(ctx context.Context, period domain.PayrollPeriod) error {
	activeTenant, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return err
	}
	if activeTenant != period.TenantID.UUID() {
		return security.ErrForbidden
	}
	return s.requireCompany(ctx, period.CompanyID)
}

func (s *Service) requireCompany(ctx context.Context, companyID organization.CompanyID) error {
	if companyID.UUID() == uuid.Nil {
		return fmt.Errorf("%w: company id is required", domain.ErrInvalidPayrollPeriod)
	}
	return s.authorizer.RequireCompany(ctx, companyID.UUID())
}

func encodePeriodCursor(id domain.PayrollPeriodID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String()))
}

func DecodePeriodCursor(value string) (domain.PayrollPeriodID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return domain.PayrollPeriodID{}, fmt.Errorf("%w: %v", domain.ErrInvalidPayrollCursor, err)
	}
	return domain.ParsePayrollPeriodID(string(decoded))
}

func encodeResultCursor(id domain.PayrollResultID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String()))
}

func DecodeResultCursor(value string) (domain.PayrollResultID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return domain.PayrollResultID{}, fmt.Errorf("%w: %v", domain.ErrInvalidPayrollCursor, err)
	}
	return domain.ParsePayrollResultID(string(decoded))
}
