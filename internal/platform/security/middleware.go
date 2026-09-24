package security

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const requiredScopesContextKey = "OAuth2.Scopes"

type Authenticator interface {
	Authenticate(ctx context.Context, bearerToken string) (Principal, error)
}

type RequestAuthenticator interface {
	AuthenticateRequest(ctx context.Context, request *http.Request) (Principal, error)
}

func Middleware(authenticator Authenticator) func(http.Handler) http.Handler {
	// Tenant selection is validated before authentication and then carried in
	// trusted context; downstream handlers never trust a raw client header.
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			selector := strings.TrimSpace(r.Header.Get(TenantSelectorHeader))
			if selector != "" {
				tenantID, err := parseTenantSelector(selector)
				if err != nil {
					writeSecurityError(w, http.StatusBadRequest, err)
					return
				}
				ctx = WithTenantSelector(ctx, tenantID)
			}
			principal, err := authenticateRequest(ctx, r, authenticator)
			if err != nil {
				status := http.StatusUnauthorized
				if errors.Is(err, ErrForbidden) {
					status = http.StatusForbidden
				} else if errors.Is(err, ErrTenantSelectionRequired) {
					status = http.StatusBadRequest
				}
				writeSecurityError(w, status, err)
				return
			}
			if err := requireScopes(r.Context(), principal); err != nil {
				status := http.StatusForbidden
				if errors.Is(err, ErrTenantSelectionRequired) {
					status = http.StatusBadRequest
				}
				writeSecurityError(w, status, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(ctx, principal)))
		})
	}
}

func authenticateRequest(ctx context.Context, request *http.Request, authenticator Authenticator) (Principal, error) {
	if requestAuthenticator, ok := authenticator.(RequestAuthenticator); ok {
		return requestAuthenticator.AuthenticateRequest(ctx, request)
	}
	header := strings.TrimSpace(request.Header.Get("Authorization"))
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return Principal{}, ErrUnauthenticated
	}
	return authenticator.Authenticate(ctx, strings.TrimSpace(header[len("Bearer "):]))
}

func requireScopes(ctx context.Context, principal Principal) error {
	required, ok := ctx.Value(requiredScopesContextKey).([]string)
	if !ok {
		return nil
	}
	if principal.TenantID == uuid.Nil {
		for _, scope := range required {
			if !strings.HasPrefix(scope, "platform:") {
				return ErrTenantSelectionRequired
			}
		}
	}
	for _, scope := range required {
		if _, ok := principal.Scopes[scope]; !ok {
			return ErrForbidden
		}
	}
	return nil
}

func writeSecurityError(w http.ResponseWriter, status int, err error) {
	code := "UNAUTHENTICATED"
	message := "Authentication is required"
	if status == http.StatusForbidden {
		code = "FORBIDDEN"
		message = "You do not have permission to perform this action"
	} else if status == http.StatusBadRequest {
		code = "INVALID_REQUEST"
		message = "The request is invalid"
		if errors.Is(err, ErrTenantSelectionRequired) {
			code = "ACTIVE_TENANT_REQUIRED"
			message = "An active tenant must be selected"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}{Code: code, Message: message, Details: map[string]any{}})
}

func parseTenantSelector(value string) (uuid.UUID, error) {
	tenantID, err := uuid.Parse(value)
	if err != nil || tenantID == uuid.Nil || tenantID.Version() != 7 || tenantID.Variant() != uuid.RFC4122 {
		return uuid.Nil, errors.New("invalid active tenant selector")
	}
	return tenantID, nil
}
