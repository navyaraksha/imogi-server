package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"

	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type RequestAuthenticator struct {
	google   security.Authenticator
	sessions *appidentity.SessionService
}

func NewRequestAuthenticator(google security.Authenticator, sessions *appidentity.SessionService) (*RequestAuthenticator, error) {
	if google == nil || sessions == nil {
		return nil, errors.New("request authenticator dependencies are required")
	}
	return &RequestAuthenticator{google: google, sessions: sessions}, nil
}

func (a *RequestAuthenticator) Authenticate(ctx context.Context, bearerToken string) (security.Principal, error) {
	return a.google.Authenticate(ctx, bearerToken)
}

func (a *RequestAuthenticator) AuthenticateRequest(ctx context.Context, request *http.Request) (security.Principal, error) {
	header := strings.TrimSpace(request.Header.Get("Authorization"))
	if token, ok := bearerToken(header); ok {
		return a.google.Authenticate(ctx, token)
	}
	if cookie, err := request.Cookie(security.SessionCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return a.sessions.Authenticate(ctx, cookie.Value)
	}
	return security.Principal{}, security.ErrUnauthenticated
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}
