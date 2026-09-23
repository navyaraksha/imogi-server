package restapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const (
	allowedOriginHeader  = "Access-Control-Allow-Origin"
	allowedMethodsHeader = "Access-Control-Allow-Methods"
	allowedHeadersHeader = "Access-Control-Allow-Headers"
)

type CORS struct {
	allowedOrigins map[string]struct{}
}

func NewCORS(origins []string) (*CORS, error) {
	allowedOrigins := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" {
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, errors.New("HTTP_ALLOWED_ORIGINS must contain origins such as http://localhost:5173")
		}
		allowedOrigins[origin] = struct{}{}
	}
	return &CORS{allowedOrigins: allowedOrigins}, nil
}

func (c *CORS) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		if _, allowed := c.allowedOrigins[origin]; !allowed {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Vary", "Origin")
		w.Header().Set(allowedOriginHeader, origin)
		w.Header().Set(allowedMethodsHeader, "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set(allowedHeadersHeader, "Authorization, Content-Type, X-Imogi-Tenant-ID, X-Imogi-CSRF")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
