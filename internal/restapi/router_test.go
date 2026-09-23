package restapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/platform/security"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

type testAuthenticator struct {
	principal security.Principal
}

func (a testAuthenticator) Authenticate(context.Context, string) (security.Principal, error) {
	return a.principal, nil
}

func TestRouterEnforcesAuthenticationAndOperationScopes(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	router, err := NewRouter(generated.Unimplemented{}, testAuthenticator{principal: security.Principal{
		Subject:  "user-1",
		TenantID: tenantID,
		Scopes:   map[string]struct{}{},
		Capabilities: map[string]struct{}{
			security.CapabilityEmployeeReadBasic: {},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/employees", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/employees", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing scope status = %d", response.Code)
	}
}

func TestRouterRequiresTenantSelectionForTenantScopedOperation(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	router, err := NewRouter(generated.Unimplemented{}, testAuthenticator{principal: security.Principal{
		Subject:  "user-1",
		Tenants:  []security.TenantAccess{{TenantID: tenantID, Slug: "acme", Name: "Acme", Status: "active", RoleCode: "admin"}},
		Scopes:   map[string]struct{}{"employee:read-basic": {}},
		TenantID: uuid.Nil,
	}})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/employees", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing tenant selector status = %d", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, `"ACTIVE_TENANT_REQUIRED"`) {
		t.Fatalf("missing tenant selection error, body = %s", body)
	}
}

func TestRouterHandlesAllowedCORSPreflight(t *testing.T) {
	router, err := NewRouterWithOptions(generated.Unimplemented{}, testAuthenticator{}, RouterOptions{
		AllowedOrigins: []string{"http://localhost:5173"},
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/me", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "authorization")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("allow origin = %q", response.Header().Get("Access-Control-Allow-Origin"))
	}
	if response.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("allow headers was not set")
	}
	if response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentialed CORS was not enabled")
	}
	if !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), "X-Imogi-CSRF") {
		t.Fatal("CSRF header was not allowed")
	}
}
