package security

import (
	"context"

	"github.com/google/uuid"
)

const TenantSelectorHeader = "X-Imogi-Tenant-ID"

const SessionCookieName = "imogi_session"
const CSRFCookieName = "imogi_csrf"

type tenantSelectorContextKey struct{}

func WithTenantSelector(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantSelectorContextKey{}, tenantID)
}

func TenantSelectorFromContext(ctx context.Context) uuid.UUID {
	tenantID, _ := ctx.Value(tenantSelectorContextKey{}).(uuid.UUID)
	return tenantID
}
