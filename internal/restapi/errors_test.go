package restapi

import (
	"errors"
	"net/http"
	"testing"

	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	domain "github.com/navyaraksha/imogi/internal/domain/employee"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

func TestMapErrorPreservesHTTPClassification(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		statusCode int
		code       string
	}{
		{
			name:       "unauthenticated",
			err:        security.ErrUnauthenticated,
			statusCode: http.StatusUnauthorized,
			code:       "UNAUTHENTICATED",
		},
		{
			name:       "not found",
			err:        domain.ErrEmployeeNotFound,
			statusCode: http.StatusNotFound,
			code:       "NOT_FOUND",
		},
		{
			name:       "conflict",
			err:        domain.ErrEmploymentOverlap,
			statusCode: http.StatusConflict,
			code:       "CONFLICT",
		},
		{
			name:       "invalid request",
			err:        appidentity.ErrInvalidMembershipRole,
			statusCode: http.StatusBadRequest,
			code:       "INVALID_REQUEST",
		},
		{
			name:       "internal error",
			err:        errors.New("database unavailable"),
			statusCode: http.StatusInternalServerError,
			code:       "INTERNAL_ERROR",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statusCode, code, _ := mapError(test.err)
			if statusCode != test.statusCode {
				t.Fatalf("status = %d, want %d", statusCode, test.statusCode)
			}
			if code != test.code {
				t.Fatalf("code = %q, want %q", code, test.code)
			}
		})
	}
}
