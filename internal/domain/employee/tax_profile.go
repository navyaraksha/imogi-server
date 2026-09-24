package employee

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type TaxProfileStatus string

const (
	TaxProfileScheduled  TaxProfileStatus = "scheduled"
	TaxProfileCurrent    TaxProfileStatus = "current"
	TaxProfileHistorical TaxProfileStatus = "historical"
)

type TaxProfile struct {
	ID            TaxProfileID
	EmployeeID    EmployeeID
	TenantID      organization.TenantID
	CompanyID     organization.CompanyID
	NIK           NIK
	NPWP          *NPWP
	PTKPCode      string
	TaxMethod     string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (p *TaxProfile) BindOwnership(tenantID organization.TenantID, companyID organization.CompanyID) error {
	if tenantID.UUID() == uuid.Nil || companyID.UUID() == uuid.Nil {
		return fmt.Errorf("%w: tenant and company are required", ErrInvalidTaxProfile)
	}
	p.TenantID, p.CompanyID = tenantID, companyID
	return nil
}

func NewTaxProfile(
	id TaxProfileID,
	employeeID EmployeeID,
	nik NIK,
	npwp *NPWP,
	ptkpCode string,
	taxMethod string,
	effectiveFrom time.Time,
	now time.Time,
) (TaxProfile, error) {
	if _, err := ParseNIK(nik.String()); err != nil {
		return TaxProfile{}, err
	}
	ptkpCode = strings.TrimSpace(ptkpCode)
	if ptkpCode == "" || len(ptkpCode) > 20 {
		return TaxProfile{}, fmt.Errorf("%w: invalid PTKP code", ErrInvalidTaxProfile)
	}
	taxMethod = strings.TrimSpace(taxMethod)
	if taxMethod == "" || len(taxMethod) > 50 {
		return TaxProfile{}, fmt.Errorf("%w: invalid tax method", ErrInvalidTaxProfile)
	}
	return TaxProfile{
		ID:            id,
		EmployeeID:    employeeID,
		NIK:           nik,
		NPWP:          cloneNPWP(npwp),
		PTKPCode:      ptkpCode,
		TaxMethod:     taxMethod,
		EffectiveFrom: dateOnly(effectiveFrom),
		CreatedAt:     dateTimeUTC(now),
		UpdatedAt:     dateTimeUTC(now),
	}, nil
}

func (p TaxProfile) Status(asOf time.Time) TaxProfileStatus {
	// Tax profile status is evaluated at the requested business date to support
	// both current payroll and historical payroll reconstruction.
	asOf = dateOnly(asOf)
	if asOf.Before(p.EffectiveFrom) {
		return TaxProfileScheduled
	}
	if p.EffectiveTo != nil && asOf.After(*p.EffectiveTo) {
		return TaxProfileHistorical
	}
	return TaxProfileCurrent
}

func cloneNPWP(value *NPWP) *NPWP {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}
