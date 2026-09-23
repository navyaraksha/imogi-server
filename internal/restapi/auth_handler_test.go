package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/platform/security"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

func TestGetCurrentUserReturnsTenantDiscoveryWithoutSensitiveData(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	ctx := security.WithPrincipal(context.Background(), security.Principal{
		UserID:      uuid.Must(uuid.NewV7()),
		Subject:     "google-subject",
		Email:       "person@example.com",
		DisplayName: "Person Example",
		TenantID:    tenantID,
		Tenants: []security.TenantAccess{{
			TenantID: tenantID,
			Slug:     "acme",
			Name:     "Acme",
			Status:   "active",
			RoleCode: "hr_admin",
		}},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil).WithContext(ctx)
	response := httptest.NewRecorder()

	NewAuthHandler().GetCurrentUser(response, request, generated.GetCurrentUserParams{})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["user"].(map[string]any)["email"] != "person@example.com" {
		t.Fatalf("unexpected user response: %#v", body["user"])
	}
	if len(body["tenants"].([]any)) != 1 || body["activeTenant"].(map[string]any)["slug"] != "acme" {
		t.Fatalf("unexpected tenant response: %#v", body)
	}
	if _, found := body["nik"]; found {
		t.Fatal("sensitive NIK was returned")
	}
}
