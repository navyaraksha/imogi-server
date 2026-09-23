package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	domaidentity "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type fakeGoogleVerifier struct {
	claims appidentity.GoogleClaims
	err    error
}

func (v fakeGoogleVerifier) Verify(context.Context, string) (appidentity.GoogleClaims, error) {
	return v.claims, v.err
}

type fakeAccessResolver struct {
	access appidentity.Access
	err    error
}

func (r fakeAccessResolver) ResolveGoogleClaims(context.Context, appidentity.GoogleClaims) (appidentity.Access, error) {
	return r.access, r.err
}

func TestGoogleAuthenticatorMapsValidatedAccessToPrincipal(t *testing.T) {
	userID := uuid.Must(uuid.NewV7())
	tenantID := uuid.Must(uuid.NewV7())
	access := appidentity.Access{
		User:          domaidentity.User{ID: domaidentity.UserID(userID), GoogleSubject: "google-subject", Email: "person@example.com"},
		TenantID:      tenantID,
		PlatformAdmin: true,
		Scopes:        map[string]struct{}{"employee:read-basic": {}},
		Capabilities:  map[string]struct{}{security.CapabilityEmployeeReadBasic: {}},
		CompanyIDs:    map[uuid.UUID]struct{}{},
	}
	authenticator := &GoogleAuthenticator{
		verifier: fakeGoogleVerifier{claims: appidentity.GoogleClaims{EmailVerified: true}},
		access:   fakeAccessResolver{access: access},
	}
	principal, err := authenticator.Authenticate(context.Background(), "token")
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != userID || principal.TenantID != tenantID || principal.Email != "person@example.com" || !principal.PlatformAdmin {
		t.Fatalf("principal identity = %+v", principal)
	}
	if _, ok := principal.Capabilities[security.CapabilityEmployeeReadBasic]; !ok {
		t.Fatal("capabilities were not propagated")
	}
}

func TestGoogleAuthenticatorMapsTenantSelectionFailureToSecurityError(t *testing.T) {
	authenticator := &GoogleAuthenticator{
		verifier: fakeGoogleVerifier{claims: appidentity.GoogleClaims{EmailVerified: true}},
		access:   fakeAccessResolver{err: appidentity.ErrActiveTenantRequired},
	}
	_, err := authenticator.Authenticate(context.Background(), "token")
	if !errors.Is(err, security.ErrTenantSelectionRequired) {
		t.Fatalf("error = %v, want tenant selection error", err)
	}
}
