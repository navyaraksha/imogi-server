package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domaidentity "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

type accessRepository struct {
	user         domaidentity.User
	platform     bool
	memberships  []Membership
	companyIDs   map[uuid.UUID][]uuid.UUID
	adminCreated bool
}

func (r *accessRepository) FindUserByGoogleSubject(_ context.Context, subject string) (domaidentity.User, error) {
	if r.user.ID.UUID() != uuid.Nil && r.user.GoogleSubject == subject {
		return r.user, nil
	}
	return domaidentity.User{}, ErrPlatformUserNotFound
}

func (r *accessRepository) FindUserByEmail(_ context.Context, email string) (domaidentity.User, error) {
	if r.user.ID.UUID() != uuid.Nil && r.user.Email == email {
		return r.user, nil
	}
	return domaidentity.User{}, ErrPlatformUserNotFound
}

func (r *accessRepository) FindUserByID(_ context.Context, userID domaidentity.UserID) (domaidentity.User, error) {
	if r.user.ID == userID {
		return r.user, nil
	}
	return domaidentity.User{}, ErrPlatformUserNotFound
}

func (r *accessRepository) UpsertGoogleUser(_ context.Context, user domaidentity.User) (domaidentity.User, error) {
	if r.user.ID.UUID() == uuid.Nil {
		r.user = user
	}
	return r.user, nil
}

func (r *accessRepository) LinkGoogleIdentity(_ context.Context, userID domaidentity.UserID, claims GoogleClaims) (domaidentity.User, error) {
	r.user.ID = userID
	r.user.GoogleSubject = claims.Subject
	r.user.Email = claims.Email
	r.user.DisplayName = claims.DisplayName
	r.user.Status = domaidentity.UserActive
	return r.user, nil
}

func (r *accessRepository) UpdateGoogleUserProfile(_ context.Context, userID domaidentity.UserID, claims GoogleClaims) (domaidentity.User, error) {
	r.user.ID = userID
	r.user.GoogleSubject = claims.Subject
	r.user.Email = claims.Email
	r.user.DisplayName = claims.DisplayName
	r.user.Status = domaidentity.UserActive
	return r.user, nil
}

func (r *accessRepository) GetPlatformAdmin(context.Context, domaidentity.UserID) (domaidentity.User, error) {
	if !r.platform {
		return domaidentity.User{}, ErrPlatformAdminNotFound
	}
	return r.user, nil
}

func (r *accessRepository) ListActiveMembershipsByUser(context.Context, domaidentity.UserID) ([]Membership, error) {
	return r.memberships, nil
}

func (r *accessRepository) ListMembershipCompanyAccess(_ context.Context, membership Membership) ([]uuid.UUID, error) {
	return r.companyIDs[membership.ID], nil
}

func (r *accessRepository) ListCompanyIDsByTenant(_ context.Context, tenantID uuid.UUID) ([]uuid.UUID, error) {
	return r.companyIDs[tenantID], nil
}

func (r *accessRepository) CreateTenantMembership(_ context.Context, userID domaidentity.UserID, tenantID uuid.UUID, roleCode string) (Membership, error) {
	membership := Membership{ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, RoleCode: roleCode}
	r.memberships = append(r.memberships, membership)
	return membership, nil
}

func (r *accessRepository) UpsertPlatformAdmin(_ context.Context, userID domaidentity.UserID) error {
	r.platform = true
	r.adminCreated = true
	return nil
}

func TestAccessServiceBootstrapsPlatformAdminWithoutTenant(t *testing.T) {
	repository := &accessRepository{}
	service, err := NewAccessService(repository, []string{"admin@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	access, err := service.ResolveGoogleClaims(context.Background(), GoogleClaims{
		Subject:       "google-subject",
		Email:         "ADMIN@example.com",
		DisplayName:   "Admin",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !access.PlatformAdmin || !repository.adminCreated {
		t.Fatal("bootstrap email did not create platform admin access")
	}
	if access.TenantID != uuid.Nil {
		t.Fatal("platform-only access unexpectedly selected a tenant")
	}
}

func TestAccessServiceRequiresTenantSelectorForMultipleMemberships(t *testing.T) {
	firstTenant := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e")
	secondTenant := uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-2f2a3b4c5d6e")
	repository := &accessRepository{user: domaidentity.User{ID: domaidentity.UserID(firstTenant), GoogleSubject: "subject", Email: "hr@example.com", Status: domaidentity.UserActive}, memberships: []Membership{
		{ID: uuid.Must(uuid.NewV7()), TenantID: firstTenant, RoleCode: "hr_admin"},
		{ID: uuid.Must(uuid.NewV7()), TenantID: secondTenant, RoleCode: "hr_admin"},
	}}
	service, err := NewAccessService(repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	access, err := service.ResolveGoogleClaims(context.Background(), GoogleClaims{Subject: "subject", Email: "hr@example.com", DisplayName: "HR", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if access.TenantID != uuid.Nil || len(access.Memberships) != 2 {
		t.Fatalf("access = %+v, want tenant selection to remain pending", access)
	}

	ctx := security.WithTenantSelector(context.Background(), secondTenant)
	access, err = service.ResolveGoogleClaims(ctx, GoogleClaims{Subject: "subject", Email: "hr@example.com", DisplayName: "HR", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if access.TenantID != secondTenant {
		t.Fatalf("tenant = %s, want %s", access.TenantID, secondTenant)
	}
}

func TestAccessServiceRejectsUnauthorizedTenantSelector(t *testing.T) {
	repository := &accessRepository{user: domaidentity.User{ID: domaidentity.UserID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e")), GoogleSubject: "subject", Email: "hr@example.com", Status: domaidentity.UserActive}, memberships: []Membership{{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-1f2a3b4c5d6e"), RoleCode: "hr_admin"}}}
	service, err := NewAccessService(repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := security.WithTenantSelector(context.Background(), uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-2f2a3b4c5d6e"))
	_, err = service.ResolveGoogleClaims(ctx, GoogleClaims{Subject: "subject", Email: "hr@example.com", DisplayName: "HR", EmailVerified: true})
	if !errors.Is(err, ErrTenantAccessDenied) {
		t.Fatalf("error = %v, want tenant access denied", err)
	}
}

func TestAccessServiceRejectsUnknownNonBootstrapIdentity(t *testing.T) {
	repository := &accessRepository{}
	service, err := NewAccessService(repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ResolveGoogleClaims(context.Background(), GoogleClaims{Subject: "unknown", Email: "unknown@example.com", EmailVerified: true})
	if !errors.Is(err, ErrIdentityNotProvisioned) {
		t.Fatalf("error = %v, want identity not provisioned", err)
	}
}

func TestAccessServiceLinksPendingIdentityByVerifiedEmail(t *testing.T) {
	userID := domaidentity.UserID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-3f2a3b4c5d6e"))
	repository := &accessRepository{user: domaidentity.User{ID: userID, Email: "invited@example.com", Status: domaidentity.UserPending}, memberships: []Membership{{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.Must(uuid.NewV7()), RoleCode: "hr_admin"}}}
	service, err := NewAccessService(repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	access, err := service.ResolveGoogleClaims(context.Background(), GoogleClaims{Subject: "linked-subject", Email: "INVITED@example.com", DisplayName: "Invited User", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if access.User.ID != userID || access.User.GoogleSubject != "linked-subject" || access.User.Status != domaidentity.UserActive {
		t.Fatalf("linked user = %+v", access.User)
	}
}

func TestAccessServiceRejectsConflictingGoogleSubject(t *testing.T) {
	repository := &accessRepository{user: domaidentity.User{ID: domaidentity.UserID(uuid.MustParse("01933b7e-5f6a-7b8c-9d0e-4f2a3b4c5d6e")), Email: "user@example.com", GoogleSubject: "different-subject", Status: domaidentity.UserActive}}
	service, err := NewAccessService(repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ResolveGoogleClaims(context.Background(), GoogleClaims{Subject: "claimed-subject", Email: "user@example.com", EmailVerified: true})
	if !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("error = %v, want identity conflict", err)
	}
}
