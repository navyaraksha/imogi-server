package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	domain "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres/sqlc"
)

type IdentityRepository struct {
	queries *sqlc.Queries
	pool    *pgxpool.Pool
}

var _ appidentity.Repository = (*IdentityRepository)(nil)
var _ appidentity.SessionRepository = (*IdentityRepository)(nil)

func NewIdentityRepository(pool *pgxpool.Pool) (*IdentityRepository, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	return &IdentityRepository{
		queries: sqlc.New(pool),
		pool:    pool,
	}, nil
}

func (r *IdentityRepository) FindUserByGoogleSubject(ctx context.Context, subject string) (domain.User, error) {
	row, err := r.queries.GetUserByGoogleSubject(ctx, &subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, appidentity.ErrPlatformUserNotFound
	}
	if err != nil {
		return domain.User{}, mapIdentityDatabaseError(err)
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) FindUserByEmail(ctx context.Context, email string) (domain.User, error) {
	row, err := r.queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, appidentity.ErrPlatformUserNotFound
	}
	if err != nil {
		return domain.User{}, mapIdentityDatabaseError(err)
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) FindUserByID(ctx context.Context, userID domain.UserID) (domain.User, error) {
	row, err := r.queries.GetPlatformUserByID(ctx, userID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, appidentity.ErrPlatformUserNotFound
	}
	if err != nil {
		return domain.User{}, mapIdentityDatabaseError(err)
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) UpsertGoogleUser(ctx context.Context, entity domain.User) (domain.User, error) {
	row, err := r.queries.UpsertGoogleUser(ctx, sqlc.UpsertGoogleUserParams{
		ID:            entity.ID.UUID(),
		GoogleSubject: &entity.GoogleSubject,
		Email:         entity.Email,
		DisplayName:   entity.DisplayName,
	})
	if err != nil {
		return domain.User{}, mapIdentityDatabaseError(err)
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) CreatePendingUser(ctx context.Context, entity domain.User) (domain.User, error) {
	row, err := r.queries.CreatePendingPlatformUser(ctx, sqlc.CreatePendingPlatformUserParams{
		ID:          entity.ID.UUID(),
		Email:       entity.Email,
		DisplayName: entity.DisplayName,
	})
	if err != nil {
		return domain.User{}, mapIdentityDatabaseError(err)
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) LinkGoogleIdentity(ctx context.Context, userID domain.UserID, claims appidentity.GoogleClaims) (domain.User, error) {
	row, err := r.queries.LinkGoogleIdentity(ctx, sqlc.LinkGoogleIdentityParams{
		ID:            userID.UUID(),
		GoogleSubject: &claims.Subject,
		Email:         claims.Email,
		DisplayName:   claims.DisplayName,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, appidentity.ErrPlatformUserNotFound
	}
	if err != nil {
		return domain.User{}, mapIdentityDatabaseError(err)
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) UpdateGoogleUserProfile(ctx context.Context, userID domain.UserID, claims appidentity.GoogleClaims) (domain.User, error) {
	row, err := r.queries.UpdateGoogleUserProfile(ctx, sqlc.UpdateGoogleUserProfileParams{
		ID:            userID.UUID(),
		GoogleSubject: &claims.Subject,
		Email:         claims.Email,
		DisplayName:   claims.DisplayName,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, appidentity.ErrPlatformUserNotFound
	}
	if err != nil {
		return domain.User{}, mapIdentityDatabaseError(err)
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) ListPlatformUsers(ctx context.Context, filter appidentity.UserFilter) ([]domain.User, error) {
	rows, err := r.queries.ListPlatformUsers(ctx, sqlc.ListPlatformUsersParams{
		CursorID: filter.Cursor,
		Search:   optionalString(filter.Search),
		Status:   optionalString(filter.Status),
		Limit:    int32(filter.Limit),
	})
	if err != nil {
		return nil, err
	}
	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, mapUser(row))
	}
	return users, nil
}

func (r *IdentityRepository) GetPlatformUser(ctx context.Context, userID domain.UserID) (domain.User, error) {
	row, err := r.queries.GetPlatformUser(ctx, userID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, appidentity.ErrPlatformUserNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) BlockPlatformUser(ctx context.Context, userID domain.UserID) (domain.User, error) {
	row, err := r.queries.BlockPlatformUser(ctx, userID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, appidentity.ErrPlatformUserNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) GetPlatformAdmin(ctx context.Context, userID domain.UserID) (domain.User, error) {
	row, err := r.queries.GetPlatformAdmin(ctx, userID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, appidentity.ErrPlatformAdminNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return mapUser(row), nil
}

func (r *IdentityRepository) ListActiveMembershipsByUser(ctx context.Context, userID domain.UserID) ([]appidentity.Membership, error) {
	rows, err := r.queries.ListActiveMembershipsByUser(ctx, userID.UUID())
	if err != nil {
		return nil, err
	}
	memberships := make([]appidentity.Membership, 0, len(rows))
	for _, row := range rows {
		memberships = append(memberships, appidentity.Membership{
			ID:           row.ID,
			UserID:       row.UserID,
			TenantID:     row.TenantID,
			TenantSlug:   row.TenantSlug,
			TenantName:   row.TenantName,
			TenantStatus: row.TenantStatus,
			RoleCode:     row.RoleCode,
			Status:       row.Status,
			CreatedAt:    timestamp(row.CreatedAt),
			UpdatedAt:    timestamp(row.UpdatedAt),
		})
	}
	return memberships, nil
}

func (r *IdentityRepository) ListMembershipCompanyAccess(ctx context.Context, membership appidentity.Membership) ([]uuid.UUID, error) {
	return r.queries.ListMembershipCompanyAccess(ctx, sqlc.ListMembershipCompanyAccessParams{
		MembershipID: membership.ID,
		TenantID:     membership.TenantID,
	})
}

func (r *IdentityRepository) ListCompanyIDsByTenant(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error) {
	return r.queries.ListCompanyIDsByTenant(ctx, tenantID)
}

func (r *IdentityRepository) CreateTenantMembership(ctx context.Context, userID domain.UserID, tenantID uuid.UUID, roleCode string) (appidentity.Membership, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return appidentity.Membership{}, err
	}
	row, err := r.queries.CreateTenantMembership(ctx, sqlc.CreateTenantMembershipParams{
		ID:       id,
		UserID:   userID.UUID(),
		TenantID: tenantID,
		RoleCode: roleCode,
	})
	if err != nil {
		return appidentity.Membership{}, mapIdentityDatabaseError(err)
	}
	return appidentity.Membership{
		ID:       row.ID,
		TenantID: row.TenantID,
		RoleCode: row.RoleCode,
	}, nil
}

func (r *IdentityRepository) ProvisionTenantMembership(ctx context.Context, input appidentity.ProvisionMembershipInput) (appidentity.Membership, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return appidentity.Membership{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := r.queries.WithTx(tx)
	user, err := queries.GetUserByEmail(ctx, input.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		userID, idErr := uuid.NewV7()
		if idErr != nil {
			return appidentity.Membership{}, idErr
		}
		user, err = queries.CreatePendingPlatformUser(ctx, sqlc.CreatePendingPlatformUserParams{
			ID:          userID,
			Email:       input.Email,
			DisplayName: input.DisplayName,
		})
	}
	if err != nil {
		return appidentity.Membership{}, mapIdentityDatabaseError(err)
	}
	if user.Status == string(domain.UserBlocked) {
		return appidentity.Membership{}, appidentity.ErrIdentityBlocked
	}

	membershipID, err := uuid.NewV7()
	if err != nil {
		return appidentity.Membership{}, err
	}
	if _, err := queries.InsertTenantMembership(ctx, sqlc.InsertTenantMembershipParams{
		ID:       membershipID,
		UserID:   user.ID,
		TenantID: input.TenantID,
		RoleCode: input.RoleCode,
	}); err != nil {
		return appidentity.Membership{}, mapIdentityDatabaseError(err)
	}
	row, err := queries.GetTenantMembership(ctx, membershipID)
	if err != nil {
		return appidentity.Membership{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return appidentity.Membership{}, err
	}
	return mapMembership(row), nil
}

func (r *IdentityRepository) ListTenantMemberships(ctx context.Context, filter appidentity.TenantMembershipFilter) ([]appidentity.Membership, error) {
	rows, err := r.queries.ListTenantMemberships(ctx, sqlc.ListTenantMembershipsParams{
		TenantID: filter.TenantID,
		CursorID: filter.Cursor,
		Limit:    int32(filter.Limit),
	})
	if err != nil {
		return nil, err
	}
	memberships := make([]appidentity.Membership, 0, len(rows))
	for _, row := range rows {
		memberships = append(memberships, mapListMembership(row))
	}
	return memberships, nil
}

func (r *IdentityRepository) GetTenantMembership(ctx context.Context, membershipID uuid.UUID) (appidentity.Membership, error) {
	row, err := r.queries.GetTenantMembership(ctx, membershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		return appidentity.Membership{}, appidentity.ErrTenantMembershipNotFound
	}
	if err != nil {
		return appidentity.Membership{}, err
	}
	return mapMembership(row), nil
}

func (r *IdentityRepository) UpdateTenantMembershipRole(ctx context.Context, membershipID uuid.UUID, roleCode string) (appidentity.Membership, error) {
	if _, err := r.queries.UpdateTenantMembershipRole(ctx, sqlc.UpdateTenantMembershipRoleParams{
		ID:       membershipID,
		RoleCode: roleCode,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return appidentity.Membership{}, appidentity.ErrTenantMembershipNotFound
		}
		return appidentity.Membership{}, err
	}
	return r.GetTenantMembership(ctx, membershipID)
}

func (r *IdentityRepository) RevokeTenantMembership(ctx context.Context, membershipID uuid.UUID) (appidentity.Membership, error) {
	if _, err := r.queries.RevokeTenantMembership(ctx, membershipID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return appidentity.Membership{}, appidentity.ErrTenantMembershipNotFound
		}
		return appidentity.Membership{}, err
	}
	return r.GetTenantMembership(ctx, membershipID)
}

func (r *IdentityRepository) ReactivateTenantMembership(ctx context.Context, membershipID uuid.UUID) (appidentity.Membership, error) {
	if _, err := r.queries.ReactivateTenantMembership(ctx, membershipID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return appidentity.Membership{}, appidentity.ErrTenantMembershipNotFound
		}
		return appidentity.Membership{}, err
	}
	return r.GetTenantMembership(ctx, membershipID)
}

func (r *IdentityRepository) UpsertPlatformAdmin(ctx context.Context, userID domain.UserID) error {
	_, err := r.queries.UpsertPlatformAdmin(ctx, userID.UUID())
	return err
}

func (r *IdentityRepository) CreateSession(ctx context.Context, entity domain.Session) (domain.Session, error) {
	row, err := r.queries.CreateSession(ctx, sqlc.CreateSessionParams{
		ID:         entity.ID,
		UserID:     entity.UserID.UUID(),
		TokenHash:  entity.TokenHash,
		CreatedAt:  toPGTimestamp(entity.CreatedAt),
		ExpiresAt:  toPGTimestamp(entity.ExpiresAt),
		LastSeenAt: toPGTimestamp(entity.LastSeenAt),
	})
	if err != nil {
		return domain.Session{}, mapIdentityDatabaseError(err)
	}
	return mapSession(row), nil
}

func (r *IdentityRepository) GetActiveSessionByTokenHash(ctx context.Context, tokenHash []byte) (domain.Session, error) {
	row, err := r.queries.GetActiveSessionByTokenHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, appidentity.ErrSessionNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}
	return mapSession(row), nil
}

func (r *IdentityRepository) TouchSession(ctx context.Context, sessionID uuid.UUID) (domain.Session, error) {
	row, err := r.queries.TouchSession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, appidentity.ErrSessionNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}
	return mapSession(row), nil
}

func (r *IdentityRepository) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := r.queries.RevokeSession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return appidentity.ErrSessionNotFound
	}
	return err
}

func mapUser(row sqlc.PlatformUser) domain.User {
	googleSubject := ""
	if row.GoogleSubject != nil {
		googleSubject = *row.GoogleSubject
	}
	return domain.User{
		ID:            domain.UserID(row.ID),
		GoogleSubject: googleSubject,
		Email:         row.Email,
		DisplayName:   row.DisplayName,
		Status:        domain.UserStatus(row.Status),
		CreatedAt:     timestamp(row.CreatedAt),
		UpdatedAt:     timestamp(row.UpdatedAt),
	}
}

func mapSession(row sqlc.PlatformSession) domain.Session {
	return domain.Session{
		ID:         row.ID,
		UserID:     domain.UserID(row.UserID),
		TokenHash:  row.TokenHash,
		CreatedAt:  timestamp(row.CreatedAt),
		ExpiresAt:  timestamp(row.ExpiresAt),
		LastSeenAt: timestamp(row.LastSeenAt),
		RevokedAt:  nullableTimestamp(row.RevokedAt),
	}
}

func mapMembership(row sqlc.GetTenantMembershipRow) appidentity.Membership {
	return appidentity.Membership{
		ID:              row.MembershipID,
		UserID:          row.UserID,
		TenantID:        row.TenantID,
		RoleCode:        row.RoleCode,
		Status:          row.MembershipStatus,
		UserEmail:       row.UserEmail,
		UserDisplayName: row.UserDisplayName,
		UserStatus:      row.UserStatus,
		GoogleLinked:    row.UserGoogleLinked,
		CreatedAt:       timestamp(row.MembershipCreatedAt),
		UpdatedAt:       timestamp(row.MembershipUpdatedAt),
	}
}

func mapListMembership(row sqlc.ListTenantMembershipsRow) appidentity.Membership {
	return appidentity.Membership{
		ID:              row.MembershipID,
		UserID:          row.UserID,
		TenantID:        row.TenantID,
		RoleCode:        row.RoleCode,
		Status:          row.MembershipStatus,
		UserEmail:       row.UserEmail,
		UserDisplayName: row.UserDisplayName,
		UserStatus:      row.UserStatus,
		GoogleLinked:    row.UserGoogleLinked,
		CreatedAt:       timestamp(row.MembershipCreatedAt),
		UpdatedAt:       timestamp(row.MembershipUpdatedAt),
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func mapIdentityDatabaseError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case "users_email_lower_uq", "users_google_subject_uq":
		return fmt.Errorf("platform user already exists: %w", appidentity.ErrPlatformUserAlreadyExists)
	case "tenant_memberships_user_tenant_uq":
		return fmt.Errorf("tenant membership already exists: %w", appidentity.ErrMembershipAlreadyExists)
	case "tenant_memberships_role_check":
		return appidentity.ErrInvalidMembershipRole
	case "tenant_memberships_tenant_fk":
		return appidentity.ErrTenantNotFound
	default:
		return err
	}
}
