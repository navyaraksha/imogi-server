package organization

import (
	"fmt"
	"strings"
	"time"
)

type CompanyStatus string

const (
	CompanyProvisioned CompanyStatus = "provisioned"
	CompanyActive      CompanyStatus = "active"
	CompanySuspended   CompanyStatus = "suspended"
	CompanyArchived    CompanyStatus = "archived"
)

type Company struct {
	ID          CompanyID
	TenantID    TenantID
	Code        string
	LegalName   string
	DisplayName string
	Status      CompanyStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewCompany(id CompanyID, tenantID TenantID, code, legalName, displayName string, now time.Time) (Company, error) {
	code, legalName, displayName = strings.TrimSpace(code), strings.TrimSpace(legalName), strings.TrimSpace(displayName)
	if code == "" || len(code) > 50 || legalName == "" || len(legalName) > 300 || displayName == "" || len(displayName) > 200 {
		return Company{}, fmt.Errorf("invalid company")
	}
	now = now.UTC()
	return Company{ID: id, TenantID: tenantID, Code: code, LegalName: legalName, DisplayName: displayName, Status: CompanyProvisioned, CreatedAt: now, UpdatedAt: now}, nil
}

func (c *Company) Update(code, legalName, displayName string, now time.Time) error {
	code, legalName, displayName = strings.TrimSpace(code), strings.TrimSpace(legalName), strings.TrimSpace(displayName)
	if code == "" || len(code) > 50 || legalName == "" || len(legalName) > 300 || displayName == "" || len(displayName) > 200 {
		return fmt.Errorf("invalid company")
	}
	if c.Status == CompanyArchived {
		return fmt.Errorf("company is archived")
	}
	c.Code, c.LegalName, c.DisplayName, c.UpdatedAt = code, legalName, displayName, now.UTC()
	return nil
}

func (c *Company) Transition(status CompanyStatus, now time.Time) error {
	if c.Status == CompanyArchived && status != CompanyArchived {
		return fmt.Errorf("archived company cannot be reactivated")
	}
	if status != CompanyActive && status != CompanySuspended && status != CompanyArchived {
		return fmt.Errorf("invalid company status")
	}
	c.Status, c.UpdatedAt = status, now.UTC()
	return nil
}
