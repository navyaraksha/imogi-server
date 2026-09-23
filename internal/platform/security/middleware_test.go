package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

type contextAuthenticator struct {
	principal Principal
	selector  uuid.UUID
}

func (a *contextAuthenticator) Authenticate(ctx context.Context, _ string) (Principal, error) {
	a.selector = TenantSelectorFromContext(ctx)
	return a.principal, nil
}

func TestMiddlewareValidatesTenantSelectorBeforeAuthentication(t *testing.T) {
	authenticator := &contextAuthenticator{}
	handler := Middleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set(TenantSelectorHeader, "not-a-uuid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid selector status = %d", response.Code)
	}

	tenantID := uuid.Must(uuid.NewV7())
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set(TenantSelectorHeader, tenantID.String())
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || authenticator.selector != tenantID {
		t.Fatalf("selector = %s, status = %d", authenticator.selector, response.Code)
	}
}
