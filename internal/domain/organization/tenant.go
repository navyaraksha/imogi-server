package organization

import (
	"fmt"
	"strings"
	"time"
)

type TenantStatus string

const (
	TenantProvisioned TenantStatus = "provisioned"
	TenantActive      TenantStatus = "active"
	TenantSuspended   TenantStatus = "suspended"
	TenantArchived    TenantStatus = "archived"
)

type Tenant struct {
	ID        TenantID
	Slug      string
	Name      string
	Status    TenantStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewTenant(id TenantID, slug, name string, now time.Time) (Tenant, error) {
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	if slug == "" || len(slug) > 63 || slug != strings.ToLower(slug) {
		return Tenant{}, fmt.Errorf("invalid tenant slug")
	}
	if name == "" || len(name) > 200 {
		return Tenant{}, fmt.Errorf("invalid tenant name")
	}
	now = now.UTC()
	return Tenant{ID: id, Slug: slug, Name: name, Status: TenantProvisioned, CreatedAt: now, UpdatedAt: now}, nil
}

func (t *Tenant) UpdateName(name string, now time.Time) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return fmt.Errorf("invalid tenant name")
	}
	if t.Status == TenantArchived {
		return fmt.Errorf("tenant is archived")
	}
	t.Name, t.UpdatedAt = name, now.UTC()
	return nil
}

func (t *Tenant) Transition(status TenantStatus, now time.Time) error {
	// Archived tenants remain addressable for history and are never moved back
	// into an active lifecycle state.
	if t.Status == TenantArchived && status != TenantArchived {
		return fmt.Errorf("archived tenant cannot be reactivated")
	}
	if status != TenantActive && status != TenantSuspended && status != TenantArchived {
		return fmt.Errorf("invalid tenant status")
	}
	t.Status, t.UpdatedAt = status, now.UTC()
	return nil
}
