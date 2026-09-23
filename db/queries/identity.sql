-- name: UpsertGoogleUser :one
INSERT INTO platform.users (
    id,
    google_subject,
    email,
    display_name
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('google_subject'),
    sqlc.arg('email'),
    sqlc.arg('display_name')
)
ON CONFLICT (google_subject)
DO UPDATE SET
    email = EXCLUDED.email,
    display_name = EXCLUDED.display_name
RETURNING *;

-- name: GetUserByGoogleSubject :one
SELECT *
FROM platform.users
WHERE google_subject = sqlc.arg('google_subject');

-- name: GetUserByEmail :one
SELECT *
FROM platform.users
WHERE lower(email) = lower(sqlc.arg('email'));

-- name: GetPlatformUserByID :one
SELECT *
FROM platform.users
WHERE id = sqlc.arg('id');

-- name: CreatePendingPlatformUser :one
INSERT INTO platform.users (
    id,
    email,
    display_name,
    status
)
VALUES (
    sqlc.arg('id'),
    lower(sqlc.arg('email')),
    sqlc.arg('display_name'),
    'pending'
)
RETURNING *;

-- name: LinkGoogleIdentity :one
UPDATE platform.users
SET google_subject = sqlc.arg('google_subject'),
    email = lower(sqlc.arg('email')),
    display_name = sqlc.arg('display_name'),
    status = 'active'
WHERE id = sqlc.arg('id')
  AND google_subject IS NULL
  AND status = 'pending'
RETURNING *;

-- name: UpdateGoogleUserProfile :one
UPDATE platform.users
SET email = lower(sqlc.arg('email')),
    display_name = sqlc.arg('display_name')
WHERE id = sqlc.arg('id')
  AND google_subject = sqlc.arg('google_subject')
RETURNING *;

-- name: ListPlatformUsers :many
SELECT *
FROM platform.users
WHERE (sqlc.narg('cursor_id')::platform.uuid_v7 IS NULL OR id < sqlc.narg('cursor_id')::platform.uuid_v7)
  AND (
      sqlc.narg('search')::text IS NULL
      OR lower(email) LIKE '%' || lower(sqlc.narg('search')::text) || '%'
      OR lower(display_name) LIKE '%' || lower(sqlc.narg('search')::text) || '%'
  )
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: GetPlatformUser :one
SELECT *
FROM platform.users
WHERE id = sqlc.arg('id');

-- name: BlockPlatformUser :one
UPDATE platform.users
SET status = 'blocked'
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: GetPlatformAdmin :one
SELECT u.*
FROM platform.platform_admins a
JOIN platform.users u ON u.id = a.user_id
WHERE a.user_id = sqlc.arg('user_id')
  AND u.status = 'active';

-- name: UpsertPlatformAdmin :one
INSERT INTO platform.platform_admins (user_id)
VALUES (sqlc.arg('user_id'))
ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING user_id;

-- name: ListActiveMembershipsByUser :many
SELECT
    tm.id,
    tm.user_id,
    tm.tenant_id,
    t.slug AS tenant_slug,
    t.name AS tenant_name,
    t.status AS tenant_status,
    tm.role_code,
    tm.status,
    tm.created_at,
    tm.updated_at
FROM platform.tenant_memberships tm
JOIN platform.tenants t ON t.id = tm.tenant_id
WHERE tm.user_id = sqlc.arg('user_id')
  AND tm.status = 'active'
  AND t.status = 'active'
ORDER BY t.name, tm.tenant_id;

-- name: ListMembershipCompanyAccess :many
SELECT company_id
FROM platform.membership_company_access
WHERE membership_id = sqlc.arg('membership_id')
  AND tenant_id = sqlc.arg('tenant_id')
ORDER BY company_id;

-- name: ListCompanyIDsByTenant :many
SELECT id
FROM organization.companies
WHERE tenant_id = sqlc.arg('tenant_id')
  AND status <> 'archived'
ORDER BY id;

-- name: CreateTenantMembership :one
INSERT INTO platform.tenant_memberships (
    id,
    user_id,
    tenant_id,
    role_code
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('user_id'),
    sqlc.arg('tenant_id'),
    sqlc.arg('role_code')
)
ON CONFLICT (user_id, tenant_id)
DO UPDATE SET
    role_code = EXCLUDED.role_code,
    status = 'active'
RETURNING *;

-- name: InsertTenantMembership :one
INSERT INTO platform.tenant_memberships (
    id,
    user_id,
    tenant_id,
    role_code
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('user_id'),
    sqlc.arg('tenant_id'),
    sqlc.arg('role_code')
)
RETURNING *;

-- name: ListTenantMemberships :many
SELECT
    tm.id AS membership_id,
    tm.user_id,
    tm.tenant_id,
    tm.role_code,
    tm.status AS membership_status,
    tm.created_at AS membership_created_at,
    tm.updated_at AS membership_updated_at,
    u.email AS user_email,
    u.display_name AS user_display_name,
    u.status AS user_status,
    CASE WHEN u.google_subject IS NULL THEN false ELSE true END AS user_google_linked
FROM platform.tenant_memberships tm
JOIN platform.users u ON u.id = tm.user_id
WHERE tm.tenant_id = sqlc.arg('tenant_id')
  AND (sqlc.narg('cursor_id')::platform.uuid_v7 IS NULL OR tm.id < sqlc.narg('cursor_id')::platform.uuid_v7)
ORDER BY tm.id DESC
LIMIT sqlc.arg('limit');

-- name: GetTenantMembership :one
SELECT
    tm.id AS membership_id,
    tm.user_id,
    tm.tenant_id,
    tm.role_code,
    tm.status AS membership_status,
    tm.created_at AS membership_created_at,
    tm.updated_at AS membership_updated_at,
    u.email AS user_email,
    u.display_name AS user_display_name,
    u.status AS user_status,
    CASE WHEN u.google_subject IS NULL THEN false ELSE true END AS user_google_linked
FROM platform.tenant_memberships tm
JOIN platform.users u ON u.id = tm.user_id
WHERE tm.id = sqlc.arg('id');

-- name: UpdateTenantMembershipRole :one
UPDATE platform.tenant_memberships
SET role_code = sqlc.arg('role_code')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: RevokeTenantMembership :one
UPDATE platform.tenant_memberships
SET status = 'revoked'
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ReactivateTenantMembership :one
UPDATE platform.tenant_memberships
SET status = 'active'
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateSession :one
INSERT INTO platform.sessions (
    id,
    user_id,
    token_hash,
    created_at,
    expires_at,
    last_seen_at
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('user_id'),
    sqlc.arg('token_hash'),
    sqlc.arg('created_at'),
    sqlc.arg('expires_at'),
    sqlc.arg('last_seen_at')
)
RETURNING *;

-- name: GetActiveSessionByTokenHash :one
SELECT *
FROM platform.sessions
WHERE token_hash = sqlc.arg('token_hash')
  AND revoked_at IS NULL
  AND expires_at > clock_timestamp();

-- name: TouchSession :one
UPDATE platform.sessions
SET last_seen_at = clock_timestamp()
WHERE id = sqlc.arg('id')
  AND revoked_at IS NULL
  AND expires_at > clock_timestamp()
RETURNING *;

-- name: RevokeSession :one
UPDATE platform.sessions
SET revoked_at = clock_timestamp()
WHERE id = sqlc.arg('id')
  AND revoked_at IS NULL
RETURNING *;
