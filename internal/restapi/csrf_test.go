package restapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/navyaraksha/imogi/internal/platform/security"
)

func TestCSRFMiddlewareRejectsUnsafeSessionRequestWithoutMatchingToken(t *testing.T) {
	handler := NewCSRF(false).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", nil)
	request.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want forbidden", response.Code)
	}
}

func TestCSRFMiddlewareAcceptsMatchingToken(t *testing.T) {
	handler := NewCSRF(false).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", nil)
	request.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: "session-token"})
	request.AddCookie(&http.Cookie{Name: security.CSRFCookieName, Value: "csrf-token"})
	request.Header.Set("X-Imogi-CSRF", "csrf-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want no content", response.Code)
	}
}

func TestCSRFMiddlewareAllowsBearerRecoveryWithStaleSessionCookie(t *testing.T) {
	handler := NewCSRF(false).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", nil)
	request.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: "stale-session-token"})
	request.Header.Set("Authorization", "Bearer google-id-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want no content", response.Code)
	}
}
