package organization

import "errors"

var (
	ErrTenantNotFound   = errors.New("tenant not found")
	ErrCompanyNotFound  = errors.New("company not found")
	ErrUnitNotFound     = errors.New("organization unit not found")
	ErrUnitTypeMismatch = errors.New("organization unit type mismatch")
)
