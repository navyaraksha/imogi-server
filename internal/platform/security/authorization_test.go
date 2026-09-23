package security

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestContextAuthorizerRequiresTrustedTenantAndCompanyClaims(t *testing.T) {
	tenantID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e")
	companyID := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-2f2a3b4c5d6e")
	authorizer := ContextAuthorizer{}
	ctx := WithPrincipal(context.Background(), Principal{
		Subject: "user-1", TenantID: tenantID,
		Capabilities: map[string]struct{}{CapabilityCompanyRead: {}},
		CompanyIDs:   map[uuid.UUID]struct{}{companyID: {}},
	})
	if got, err := authorizer.ActiveTenant(ctx); err != nil || got != tenantID {
		t.Fatalf("active tenant = %v, %v", got, err)
	}
	if err := authorizer.RequireCompany(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.RequireCompany(ctx, uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-3f2a3b4c5d6e")); err == nil {
		t.Fatal("unauthorized company was accepted")
	}
}

func TestContextAuthorizerRejectsMissingTenant(t *testing.T) {
	ctx := WithPrincipal(context.Background(), Principal{Subject: "user-1"})
	if _, err := (ContextAuthorizer{}).ActiveTenant(ctx); err == nil {
		t.Fatal("missing tenant was accepted")
	}
}
