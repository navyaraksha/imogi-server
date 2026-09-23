package restapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/navyaraksha/imogi/internal/platform/security"
)

type CSRF struct {
	secureCookie bool
}

func NewCSRF(secureCookie bool) *CSRF {
	return &CSRF{secureCookie: secureCookie}
}

func (c *CSRF) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, sessionErr := r.Cookie(security.SessionCookieName)
		if sessionErr == nil && strings.TrimSpace(sessionCookie.Value) != "" && requiresCSRF(r.Method) && !hasBearerAuthorization(r) {
			if !validCSRF(r) {
				writeError(w, security.ErrForbidden)
				return
			}
		}
		if sessionErr == nil && strings.TrimSpace(sessionCookie.Value) != "" {
			ensureCSRFCookie(w, r, c.secureCookie)
		}
		next.ServeHTTP(w, r)
	})
}

func hasBearerAuthorization(r *http.Request) bool {
	const prefix = "Bearer "
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	return strings.TrimSpace(header[len(prefix):]) != ""
}

func validCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(security.CSRFCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	header := r.Header.Get("X-Imogi-CSRF")
	if header == "" || len(header) != len(cookie.Value) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) == 1
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, secure bool) {
	if cookie, err := r.Cookie(security.CSRFCookieName); err == nil && cookie.Value != "" {
		return
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     security.CSRFCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(tokenBytes),
		Path:     "/",
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func requiresCSRF(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}
