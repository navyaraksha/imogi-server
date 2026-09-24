package organization

import (
	"testing"

	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

func TestTenantTransitionCapability(t *testing.T) {
	tests := []struct {
		status     organization.TenantStatus
		capability string
	}{
		{status: organization.TenantActive, capability: security.CapabilityPlatformTenantActivate},
		{status: organization.TenantSuspended, capability: security.CapabilityPlatformTenantSuspend},
		{status: organization.TenantArchived, capability: security.CapabilityPlatformTenantArchive},
		{status: organization.TenantProvisioned, capability: security.CapabilityPlatformTenantUpdate},
	}

	for _, test := range tests {
		if got := tenantTransitionCapability(test.status); got != test.capability {
			t.Errorf("capability for %q = %q, want %q", test.status, got, test.capability)
		}
	}
}

func TestCompanyTransitionCapability(t *testing.T) {
	tests := []struct {
		status     organization.CompanyStatus
		capability string
	}{
		{status: organization.CompanyActive, capability: security.CapabilityCompanyActivate},
		{status: organization.CompanySuspended, capability: security.CapabilityCompanySuspend},
		{status: organization.CompanyArchived, capability: security.CapabilityCompanyArchive},
		{status: organization.CompanyProvisioned, capability: security.CapabilityCompanyUpdate},
	}

	for _, test := range tests {
		if got := companyTransitionCapability(test.status); got != test.capability {
			t.Errorf("capability for %q = %q, want %q", test.status, got, test.capability)
		}
	}
}
