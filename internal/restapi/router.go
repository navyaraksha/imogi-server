package restapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/navyaraksha/imogi/internal/platform/logging"
	"github.com/navyaraksha/imogi/internal/platform/security"
	generated "github.com/navyaraksha/imogi/internal/restapi/generated"
)

// NewRouter wires the generated OpenAPI routes. Authorization middleware is
// installed inside the generated operation wrapper so it can see the scopes
// declared on each OpenAPI operation.
func NewRouter(handler generated.ServerInterface, authenticator security.Authenticator) (chi.Router, error) {
	return NewRouterWithOptions(handler, authenticator, RouterOptions{})
}

type RouterOptions struct {
	AllowedOrigins      []string
	SessionCookieSecure bool
}

func NewRouterWithOptions(handler generated.ServerInterface, authenticator security.Authenticator, options RouterOptions) (chi.Router, error) {
	if handler == nil {
		return nil, errors.New("generated API handler is required")
	}
	if authenticator == nil {
		return nil, errors.New("authenticator is required")
	}
	cors, err := NewCORS(options.AllowedOrigins)
	if err != nil {
		return nil, err
	}
	router := chi.NewRouter()
	router.Use(logging.Middleware(slog.Default()))
	router.Use(cors.Middleware)
	router.Use(NewCSRF(options.SessionCookieSecure).Middleware)
	generated.HandlerWithOptions(handler, generated.ChiServerOptions{
		BaseRouter: router,
		Middlewares: []generated.MiddlewareFunc{
			security.Middleware(authenticator),
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			status, code, message := mapError(err)
			if status >= http.StatusInternalServerError {
				status = http.StatusBadRequest
				code = "INVALID_REQUEST"
				message = "The request is invalid"
			}
			writeJSON(w, status, generated.ErrorResponse{Code: code, Message: message, Details: map[string]interface{}{}})
		},
	})
	return router, nil
}
