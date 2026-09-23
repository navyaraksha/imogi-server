package restapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	"github.com/navyaraksha/imogi/internal/platform/security"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

type AuthHandler struct {
	generated.Unimplemented
	sessions            *appidentity.SessionService
	sessionCookieSecure bool
}

func NewAuthHandler() *AuthHandler {
	return &AuthHandler{}
}

func NewAuthHandlerWithSession(sessions *appidentity.SessionService, secureCookie bool) (*AuthHandler, error) {
	if sessions == nil {
		return nil, errors.New("session service is required")
	}
	return &AuthHandler{sessions: sessions, sessionCookieSecure: secureCookie}, nil
}

func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request, _ generated.GetCurrentUserParams) {
	principal, ok := security.PrincipalFromContext(r.Context())
	if !ok || principal.Subject == "" {
		writeError(w, security.ErrUnauthenticated)
		return
	}

	writeJSON(w, http.StatusOK, authMeResponseFromPrincipal(principal))
}

func (h *AuthHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	if h.sessions == nil {
		writeError(w, errors.New("session authentication is not configured"))
		return
	}
	principal, ok := security.PrincipalFromContext(r.Context())
	if !ok || principal.Subject == "" {
		writeError(w, security.ErrUnauthenticated)
		return
	}
	token, expiresAt, err := h.sessions.Create(r.Context(), principal)
	if err != nil {
		writeError(w, err)
		return
	}
	setSessionCookie(w, token, expiresAt, h.sessionCookieSecure)
	ensureCSRFCookie(w, r, h.sessionCookieSecure)
	writeJSON(w, http.StatusOK, authMeResponseFromPrincipal(principal))
}

func (h *AuthHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if h.sessions == nil {
		writeError(w, errors.New("session authentication is not configured"))
		return
	}
	if cookie, err := r.Cookie(security.SessionCookieName); err == nil {
		if err := h.sessions.Revoke(r.Context(), cookie.Value); err != nil {
			writeError(w, err)
			return
		}
	}
	clearCookie(w, security.SessionCookieName, h.sessionCookieSecure)
	clearCookie(w, security.CSRFCookieName, h.sessionCookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

type authMeResponse struct {
	User         authUserResponse     `json:"user"`
	Tenants      []authTenantResponse `json:"tenants"`
	ActiveTenant *authTenantResponse  `json:"activeTenant"`
}

type authUserResponse struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"displayName"`
	PlatformAdmin bool      `json:"platformAdmin"`
}

type authTenantResponse struct {
	TenantID uuid.UUID `json:"tenantId"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Role     string    `json:"role"`
}

func authMeResponseFromPrincipal(principal security.Principal) authMeResponse {
	response := authMeResponse{
		User: authUserResponse{
			ID:            principal.UserID,
			Email:         principal.Email,
			DisplayName:   principal.DisplayName,
			PlatformAdmin: principal.PlatformAdmin,
		},
		Tenants: make([]authTenantResponse, 0, len(principal.Tenants)),
	}
	for _, tenant := range principal.Tenants {
		item := authTenantResponse{
			TenantID: tenant.TenantID,
			Slug:     tenant.Slug,
			Name:     tenant.Name,
			Status:   tenant.Status,
			Role:     tenant.RoleCode,
		}
		response.Tenants = append(response.Tenants, item)
		if principal.TenantID != uuid.Nil && principal.TenantID == tenant.TenantID {
			active := item
			response.ActiveTenant = &active
		}
	}
	return response
}

func setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     security.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: name == security.SessionCookieName,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
