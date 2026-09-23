-- name: CreateEmployee :one
INSERT INTO employee.employees (
    id,
    tenant_id,
    company_id,
    employee_number,
    nik_ciphertext,
    nik_lookup_hash,
    full_name,
    birth_place,
    birth_date,
    gender,
    email,
    phone,
    address
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('tenant_id'),
    sqlc.arg('company_id'),
    sqlc.arg('employee_number'),
    sqlc.arg('nik_ciphertext'),
    sqlc.arg('nik_lookup_hash'),
    sqlc.arg('full_name'),
    sqlc.arg('birth_place'),
    sqlc.arg('birth_date'),
    sqlc.arg('gender'),
    sqlc.arg('email'),
    sqlc.arg('phone'),
    sqlc.arg('address')
)
RETURNING *;

-- name: GetEmployee :one
SELECT *
FROM employee.employees
WHERE id = sqlc.arg('id');

-- name: ListEmployeeSummaries :many
SELECT id, tenant_id, company_id, employee_number, full_name, created_at, updated_at
FROM employee.employees
WHERE (
    sqlc.narg('search')::text IS NULL
    OR full_name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR employee_number ILIKE '%' || sqlc.narg('search')::text || '%'
)
AND (
    sqlc.narg('tenant_id')::platform.uuid_v7 IS NULL
    OR tenant_id = sqlc.narg('tenant_id')::platform.uuid_v7
)
AND (
    sqlc.narg('company_id')::platform.uuid_v7 IS NULL
    OR company_id = sqlc.narg('company_id')::platform.uuid_v7
)
AND (
    sqlc.narg('cursor_id')::platform.uuid_v7 IS NULL
    OR id < sqlc.narg('cursor_id')::platform.uuid_v7
)
AND (
    sqlc.narg('employment_status')::text IS NULL
    OR (
        sqlc.narg('employment_status')::text = 'active'
        AND EXISTS (
            SELECT 1 FROM employee.employments e
            WHERE e.employee_id = employee.employees.id
              AND e.join_date <= CURRENT_DATE
              AND (e.end_date IS NULL OR e.end_date >= CURRENT_DATE)
        )
    )
    OR (
        sqlc.narg('employment_status')::text = 'scheduled'
        AND EXISTS (
            SELECT 1 FROM employee.employments e
            WHERE e.employee_id = employee.employees.id
              AND e.join_date > CURRENT_DATE
              AND e.end_date IS NULL
        )
    )
    OR (
        sqlc.narg('employment_status')::text = 'ended'
        AND NOT EXISTS (
            SELECT 1 FROM employee.employments e
            WHERE e.employee_id = employee.employees.id
              AND e.end_date IS NULL
        )
        AND EXISTS (
            SELECT 1 FROM employee.employments e
            WHERE e.employee_id = employee.employees.id
        )
    )
)
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: UpdateEmployeePersonalData :one
UPDATE employee.employees
SET
    full_name = sqlc.arg('full_name'),
    birth_place = sqlc.arg('birth_place'),
    birth_date = sqlc.arg('birth_date'),
    gender = sqlc.arg('gender'),
    email = sqlc.arg('email'),
    phone = sqlc.arg('phone'),
    address = sqlc.arg('address')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateEmployment :one
INSERT INTO employee.employments (
    id,
    employee_id,
    tenant_id,
    company_id,
    employment_type,
    join_date
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('employee_id'),
    sqlc.arg('tenant_id'),
    sqlc.arg('company_id'),
    sqlc.arg('employment_type'),
    sqlc.arg('join_date')
)
RETURNING *;

-- name: GetEmployment :one
SELECT *
FROM employee.employments
WHERE id = sqlc.arg('id');

-- name: GetEmploymentForUpdate :one
SELECT *
FROM employee.employments
WHERE id = sqlc.arg('id')
FOR UPDATE;

-- name: ListEmploymentsByEmployee :many
SELECT *
FROM employee.employments
WHERE employee_id = sqlc.arg('employee_id')
ORDER BY join_date DESC, id DESC;

-- name: FindOpenEmploymentByEmployeeForUpdate :one
SELECT *
FROM employee.employments
WHERE employee_id = sqlc.arg('employee_id')
  AND end_date IS NULL
FOR UPDATE;

-- name: EndEmployment :one
UPDATE employee.employments
SET
    end_date = sqlc.arg('end_date'),
    termination_reason = sqlc.arg('termination_reason')
WHERE id = sqlc.arg('id')
  AND end_date IS NULL
RETURNING *;

-- name: CreateAssignment :one
INSERT INTO employee.employee_assignments (
    id,
    employment_id,
    location_id,
    department_id,
    position_id,
    group_id,
    effective_from
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('employment_id'),
    sqlc.arg('location_id'),
    sqlc.arg('department_id'),
    sqlc.arg('position_id'),
    sqlc.arg('group_id'),
    sqlc.arg('effective_from')
)
RETURNING *;

-- name: GetAssignment :one
SELECT *
FROM employee.employee_assignments
WHERE id = sqlc.arg('id');

-- name: ListAssignmentsByEmployment :many
SELECT *
FROM employee.employee_assignments
WHERE employment_id = sqlc.arg('employment_id')
ORDER BY effective_from DESC, id DESC;

-- name: FindOpenAssignmentByEmploymentForUpdate :one
SELECT *
FROM employee.employee_assignments
WHERE employment_id = sqlc.arg('employment_id')
  AND effective_to IS NULL
FOR UPDATE;

-- name: CloseAssignment :one
UPDATE employee.employee_assignments
SET effective_to = sqlc.arg('effective_to')
WHERE id = sqlc.arg('id')
  AND effective_to IS NULL
RETURNING *;

-- name: CreateTaxProfile :one
INSERT INTO tax.employee_tax_profiles (
    id,
    employee_id,
    tenant_id,
    company_id,
    nik_ciphertext,
    npwp_ciphertext,
    ptkp_code,
    tax_method,
    effective_from
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('employee_id'),
    sqlc.arg('tenant_id'),
    sqlc.arg('company_id'),
    sqlc.arg('nik_ciphertext'),
    sqlc.arg('npwp_ciphertext'),
    sqlc.arg('ptkp_code'),
    sqlc.arg('tax_method'),
    sqlc.arg('effective_from')
)
RETURNING *;

-- name: GetTaxProfile :one
SELECT *
FROM tax.employee_tax_profiles
WHERE id = sqlc.arg('id');

-- name: ListTaxProfilesByEmployee :many
SELECT *
FROM tax.employee_tax_profiles
WHERE employee_id = sqlc.arg('employee_id')
ORDER BY effective_from DESC, id DESC;

-- name: FindOpenTaxProfileByEmployeeForUpdate :one
SELECT *
FROM tax.employee_tax_profiles
WHERE employee_id = sqlc.arg('employee_id')
  AND effective_to IS NULL
FOR UPDATE;

-- name: CloseTaxProfile :one
UPDATE tax.employee_tax_profiles
SET effective_to = sqlc.arg('effective_to')
WHERE id = sqlc.arg('id')
  AND effective_to IS NULL
RETURNING *;
