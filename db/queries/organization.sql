-- name: CreateTenant :one
INSERT INTO platform.tenants (id, slug, name)
VALUES (sqlc.arg('id'), sqlc.arg('slug'), sqlc.arg('name'))
RETURNING *;

-- name: GetTenant :one
SELECT * FROM platform.tenants WHERE id = sqlc.arg('id');

-- name: ListTenants :many
SELECT * FROM platform.tenants
WHERE (sqlc.narg('cursor_id')::platform.uuid_v7 IS NULL OR id < sqlc.narg('cursor_id')::platform.uuid_v7)
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: UpdateTenant :one
UPDATE platform.tenants
SET name = sqlc.arg('name')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: TransitionTenant :one
UPDATE platform.tenants
SET status = sqlc.arg('status')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateCompany :one
INSERT INTO organization.companies (id, tenant_id, code, legal_name, display_name)
VALUES (sqlc.arg('id'), sqlc.arg('tenant_id'), sqlc.arg('code'), sqlc.arg('legal_name'), sqlc.arg('display_name'))
RETURNING *;

-- name: GetCompany :one
SELECT * FROM organization.companies WHERE id = sqlc.arg('id');

-- name: ListCompanies :many
SELECT * FROM organization.companies
WHERE tenant_id = sqlc.arg('tenant_id')
  AND (sqlc.narg('cursor_id')::platform.uuid_v7 IS NULL OR id < sqlc.narg('cursor_id')::platform.uuid_v7)
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: UpdateCompany :one
UPDATE organization.companies
SET code = sqlc.arg('code'), legal_name = sqlc.arg('legal_name'), display_name = sqlc.arg('display_name')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: TransitionCompany :one
UPDATE organization.companies
SET status = sqlc.arg('status')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateUnit :one
INSERT INTO organization.units (id, tenant_id, company_id, unit_type, code, name)
VALUES (sqlc.arg('id'), sqlc.arg('tenant_id'), sqlc.arg('company_id'), sqlc.arg('unit_type'), sqlc.arg('code'), sqlc.arg('name'))
RETURNING *;

-- name: GetUnit :one
SELECT * FROM organization.units WHERE id = sqlc.arg('id');

-- name: ListUnits :many
SELECT * FROM organization.units
WHERE tenant_id = sqlc.arg('tenant_id')
  AND unit_type = sqlc.arg('unit_type')
  AND (sqlc.narg('company_id')::platform.uuid_v7 IS NULL OR company_id = sqlc.narg('company_id')::platform.uuid_v7)
  AND (sqlc.narg('cursor_id')::platform.uuid_v7 IS NULL OR id < sqlc.narg('cursor_id')::platform.uuid_v7)
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: UpdateUnit :one
UPDATE organization.units
SET code = sqlc.arg('code'), name = sqlc.arg('name')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ArchiveUnit :one
UPDATE organization.units
SET status = 'archived'
WHERE id = sqlc.arg('id') AND status <> 'archived'
RETURNING *;
