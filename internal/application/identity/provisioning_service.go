package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	domain "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/pagination"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

var (
	ErrInvalidMembershipRole     = errors.New("invalid membership role")
	ErrPlatformUserAlreadyExists = errors.New("platform user already exists")
	ErrMembershipAlreadyExists   = errors.New("tenant membership already exists")
	ErrCannotBlockCurrentUser    = errors.New("cannot block the current user")
)

const (
	RoleTenantAdmin  = "tenant_admin"
	RoleHRAdmin      = "hr_admin"
	RoleTaxAdmin     = "tax_admin"
	RolePayrollAdmin = "payroll_admin"
)

type ProvisioningService struct {
	repository ProvisioningRepository
	authorizer security.Authorizer
	clock      clock.Clock
}

func NewProvisioningService(repository ProvisioningRepository, authorizer security.Authorizer, systemClock clock.Clock) (*ProvisioningService, error) {
	if repository == nil || authorizer == nil || systemClock == nil {
		return nil, errors.New("identity provisioning dependencies are required")
	}
	return &ProvisioningService{repository: repository, authorizer: authorizer, clock: systemClock}, nil
}

type CreatePlatformUserInput struct {
	Email       string
	DisplayName string
}

func (s *ProvisioningService) CreatePlatformUser(ctx context.Context, input CreatePlatformUserInput) (domain.User, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformUserCreate); err != nil {
		return domain.User{}, err
	}
	id, err := domain.NewUserID()
	if err != nil {
		return domain.User{}, fmt.Errorf("generate platform user id: %w", err)
	}
	user, err := domain.NewPendingUser(id, input.Email, input.DisplayName, s.clock.Now())
	if err != nil {
		return domain.User{}, err
	}
	user, err = s.repository.CreatePendingUser(ctx, user)
	if errors.Is(err, ErrPlatformUserAlreadyExists) {
		return domain.User{}, err
	}
	return user, err
}

func (s *ProvisioningService) ListPlatformUsers(ctx context.Context, filter UserFilter) ([]domain.User, string, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformUserRead); err != nil {
		return nil, "", err
	}
	filter.Limit = normalizeLimit(filter.Limit)
	users, err := s.repository.ListPlatformUsers(ctx, filterWithExtraLimit(filter))
	if err != nil {
		return nil, "", err
	}
	return trimUsersPage(users, filter.Limit)
}

func (s *ProvisioningService) GetPlatformUser(ctx context.Context, userID domain.UserID) (domain.User, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformUserRead); err != nil {
		return domain.User{}, err
	}
	return s.repository.GetPlatformUser(ctx, userID)
}

func (s *ProvisioningService) BlockPlatformUser(ctx context.Context, userID domain.UserID) (domain.User, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformUserBlock); err != nil {
		return domain.User{}, err
	}
	currentUser, err := security.CurrentUserID(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if currentUser == userID {
		return domain.User{}, ErrCannotBlockCurrentUser
	}
	return s.repository.BlockPlatformUser(ctx, userID)
}

type MembershipListResult struct {
	Items      []Membership
	NextCursor string
}

func (s *ProvisioningService) ListPlatformTenantMemberships(ctx context.Context, tenantID uuid.UUID, filter TenantMembershipFilter) (MembershipListResult, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformMembershipRead); err != nil {
		return MembershipListResult{}, err
	}
	filter.TenantID = tenantID
	return s.listMemberships(ctx, filter)
}

func (s *ProvisioningService) ListTenantMemberships(ctx context.Context, filter TenantMembershipFilter) (MembershipListResult, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityMembershipRead); err != nil {
		return MembershipListResult{}, err
	}
	tenantID, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return MembershipListResult{}, err
	}
	if tenantID == uuid.Nil {
		return MembershipListResult{}, ErrActiveTenantRequired
	}
	filter.TenantID = tenantID
	return s.listMemberships(ctx, filter)
}

func (s *ProvisioningService) listMemberships(ctx context.Context, filter TenantMembershipFilter) (MembershipListResult, error) {
	filter.Limit = normalizeLimit(filter.Limit)
	items, err := s.repository.ListTenantMemberships(ctx, filterWithMembershipExtraLimit(filter))
	if err != nil {
		return MembershipListResult{}, err
	}
	return trimMembershipPage(items, filter.Limit), nil
}

type CreateMembershipInput struct {
	TenantID    uuid.UUID
	Email       string
	DisplayName string
	RoleCode    string
}

func (s *ProvisioningService) CreatePlatformTenantMembership(ctx context.Context, input CreateMembershipInput) (Membership, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformMembershipWrite); err != nil {
		return Membership{}, err
	}
	return s.provisionMembership(ctx, input)
}

func (s *ProvisioningService) CreateTenantMembership(ctx context.Context, input CreateMembershipInput) (Membership, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityMembershipWrite); err != nil {
		return Membership{}, err
	}
	tenantID, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return Membership{}, err
	}
	if tenantID == uuid.Nil {
		return Membership{}, ErrActiveTenantRequired
	}
	input.TenantID = tenantID
	return s.provisionMembership(ctx, input)
}

func (s *ProvisioningService) provisionMembership(ctx context.Context, input CreateMembershipInput) (Membership, error) {
	// Membership creation is scoped to the active tenant and uses a fixed role
	// allowlist so callers cannot grant arbitrary capabilities.
	if !validMembershipRole(input.RoleCode) {
		return Membership{}, ErrInvalidMembershipRole
	}
	return s.repository.ProvisionTenantMembership(ctx, ProvisionMembershipInput{
		TenantID: input.TenantID, Email: strings.TrimSpace(strings.ToLower(input.Email)),
		DisplayName: input.DisplayName, RoleCode: input.RoleCode,
	})
}

func (s *ProvisioningService) UpdatePlatformTenantMembership(ctx context.Context, tenantID, membershipID uuid.UUID, roleCode string) (Membership, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformMembershipWrite); err != nil {
		return Membership{}, err
	}
	return s.updateMembership(ctx, tenantID, membershipID, roleCode)
}

func (s *ProvisioningService) UpdateTenantMembership(ctx context.Context, membershipID uuid.UUID, roleCode string) (Membership, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityMembershipWrite); err != nil {
		return Membership{}, err
	}
	tenantID, err := s.activeTenant(ctx)
	if err != nil {
		return Membership{}, err
	}
	return s.updateMembership(ctx, tenantID, membershipID, roleCode)
}

func (s *ProvisioningService) updateMembership(ctx context.Context, tenantID, membershipID uuid.UUID, roleCode string) (Membership, error) {
	if !validMembershipRole(roleCode) {
		return Membership{}, ErrInvalidMembershipRole
	}
	current, err := s.repository.GetTenantMembership(ctx, membershipID)
	if err != nil {
		return Membership{}, err
	}
	if current.TenantID != tenantID {
		return Membership{}, security.ErrForbidden
	}
	return s.repository.UpdateTenantMembershipRole(ctx, membershipID, roleCode)
}

func (s *ProvisioningService) RevokePlatformTenantMembership(ctx context.Context, tenantID, membershipID uuid.UUID) (Membership, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformMembershipWrite); err != nil {
		return Membership{}, err
	}
	return s.changeMembershipStatus(ctx, tenantID, membershipID, false)
}

func (s *ProvisioningService) RevokeTenantMembership(ctx context.Context, membershipID uuid.UUID) (Membership, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityMembershipWrite); err != nil {
		return Membership{}, err
	}
	tenantID, err := s.activeTenant(ctx)
	if err != nil {
		return Membership{}, err
	}
	return s.changeMembershipStatus(ctx, tenantID, membershipID, false)
}

func (s *ProvisioningService) ReactivatePlatformTenantMembership(ctx context.Context, tenantID, membershipID uuid.UUID) (Membership, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityPlatformMembershipWrite); err != nil {
		return Membership{}, err
	}
	return s.changeMembershipStatus(ctx, tenantID, membershipID, true)
}

func (s *ProvisioningService) ReactivateTenantMembership(ctx context.Context, membershipID uuid.UUID) (Membership, error) {
	if err := s.authorizer.Require(ctx, security.CapabilityMembershipWrite); err != nil {
		return Membership{}, err
	}
	tenantID, err := s.activeTenant(ctx)
	if err != nil {
		return Membership{}, err
	}
	return s.changeMembershipStatus(ctx, tenantID, membershipID, true)
}

func (s *ProvisioningService) changeMembershipStatus(ctx context.Context, tenantID, membershipID uuid.UUID, reactivate bool) (Membership, error) {
	// Platform and tenant APIs share this transition path, but both must resolve
	// the membership inside the caller's authorized tenant scope.
	current, err := s.repository.GetTenantMembership(ctx, membershipID)
	if err != nil {
		return Membership{}, err
	}
	if current.TenantID != tenantID {
		return Membership{}, security.ErrForbidden
	}
	if reactivate {
		return s.repository.ReactivateTenantMembership(ctx, membershipID)
	}
	return s.repository.RevokeTenantMembership(ctx, membershipID)
}

func (s *ProvisioningService) activeTenant(ctx context.Context) (uuid.UUID, error) {
	tenantID, err := security.ActiveTenant(ctx, s.authorizer)
	if err != nil {
		return uuid.Nil, err
	}
	if tenantID == uuid.Nil {
		return uuid.Nil, ErrActiveTenantRequired
	}
	return tenantID, nil
}

func validMembershipRole(role string) bool {
	switch role {
	case RoleTenantAdmin, RoleHRAdmin, RoleTaxAdmin, RolePayrollAdmin:
		return true
	default:
		return false
	}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func filterWithExtraLimit(filter UserFilter) UserFilter {
	filter.Limit++
	return filter
}

func filterWithMembershipExtraLimit(filter TenantMembershipFilter) TenantMembershipFilter {
	filter.Limit++
	return filter
}

func trimUsersPage(items []domain.User, limit int) ([]domain.User, string, error) {
	var next string
	if len(items) > limit {
		last := items[limit-1]
		next = pagination.EncodeUUID(last.ID.UUID())
		items = items[:limit]
	}
	return items, next, nil
}

func trimMembershipPage(items []Membership, limit int) MembershipListResult {
	var next string
	if len(items) > limit {
		next = pagination.EncodeUUID(items[limit-1].ID)
		items = items[:limit]
	}
	return MembershipListResult{Items: items, NextCursor: next}
}
