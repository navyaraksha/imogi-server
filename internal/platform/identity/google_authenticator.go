package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/idtoken"

	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type GoogleTokenVerifier interface {
	Verify(context.Context, string) (appidentity.GoogleClaims, error)
}

type GoogleAuthenticator struct {
	verifier GoogleTokenVerifier
	access   appidentity.AccessResolver
}

func NewGoogleAuthenticator(ctx context.Context, clientID, hostedDomain string, access appidentity.AccessResolver, httpClient *http.Client) (*GoogleAuthenticator, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, errors.New("google client id is required")
	}
	if access == nil {
		return nil, errors.New("identity access resolver is required")
	}
	validatorOptions := make([]idtoken.ClientOption, 0, 1)
	if httpClient != nil {
		validatorOptions = append(validatorOptions, idtoken.WithHTTPClient(httpClient))
	}
	validator, err := idtoken.NewValidator(ctx, validatorOptions...)
	if err != nil {
		return nil, fmt.Errorf("create google ID token validator: %w", err)
	}
	return &GoogleAuthenticator{
		verifier: googleTokenVerifier{validator: validator, clientID: clientID, hostedDomain: strings.TrimSpace(strings.ToLower(hostedDomain))},
		access:   access,
	}, nil
}

func (a *GoogleAuthenticator) Authenticate(ctx context.Context, bearerToken string) (security.Principal, error) {
	claims, err := a.verifier.Verify(ctx, bearerToken)
	if err != nil {
		return security.Principal{}, fmt.Errorf("validate google ID token: %w", err)
	}
	access, err := a.access.ResolveGoogleClaims(ctx, claims)
	if err != nil {
		switch {
		case errors.Is(err, appidentity.ErrActiveTenantRequired):
			return security.Principal{}, fmt.Errorf("%w: %v", security.ErrTenantSelectionRequired, err)
		case errors.Is(err, appidentity.ErrIdentityNotProvisioned), errors.Is(err, appidentity.ErrIdentityBlocked), errors.Is(err, appidentity.ErrIdentityConflict), errors.Is(err, appidentity.ErrTenantAccessDenied):
			return security.Principal{}, fmt.Errorf("%w: %v", security.ErrForbidden, err)
		default:
			return security.Principal{}, err
		}
	}
	return appidentity.PrincipalFromAccess(access), nil
}

type googleTokenVerifier struct {
	validator    *idtoken.Validator
	clientID     string
	hostedDomain string
}

func (v googleTokenVerifier) Verify(ctx context.Context, token string) (appidentity.GoogleClaims, error) {
	payload, err := v.validator.Validate(ctx, strings.TrimSpace(token), v.clientID)
	if err != nil {
		return appidentity.GoogleClaims{}, err
	}
	if payload.Issuer != "https://accounts.google.com" && payload.Issuer != "accounts.google.com" {
		return appidentity.GoogleClaims{}, errors.New("invalid google token issuer")
	}
	claims := appidentity.GoogleClaims{
		Subject:       payload.Subject,
		Email:         claimString(payload.Claims, "email"),
		DisplayName:   claimString(payload.Claims, "name"),
		EmailVerified: claimBool(payload.Claims, "email_verified"),
		HostedDomain:  strings.ToLower(claimString(payload.Claims, "hd")),
	}
	if claims.Subject == "" || claims.Email == "" {
		return appidentity.GoogleClaims{}, errors.New("google token identity claims are incomplete")
	}
	if !claims.EmailVerified {
		return appidentity.GoogleClaims{}, errors.New("google email is not verified")
	}
	if v.hostedDomain != "" && claims.HostedDomain != v.hostedDomain {
		return appidentity.GoogleClaims{}, errors.New("google hosted domain is not allowed")
	}
	if claims.DisplayName == "" {
		claims.DisplayName = claims.Email
	}
	return claims, nil
}

func claimString(claims map[string]interface{}, name string) string {
	value, _ := claims[name].(string)
	return strings.TrimSpace(value)
}

func claimBool(claims map[string]interface{}, name string) bool {
	value, _ := claims[name].(bool)
	return value
}
