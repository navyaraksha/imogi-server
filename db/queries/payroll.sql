-- name: CreatePayrollPeriod :one
INSERT INTO payroll.payroll_periods (id, tenant_id, company_id, year, month)
VALUES (sqlc.arg('id'), sqlc.arg('tenant_id'), sqlc.arg('company_id'), sqlc.arg('year'), sqlc.arg('month'))
RETURNING *;

-- name: GetPayrollPeriod :one
SELECT *
FROM payroll.payroll_periods
WHERE id = sqlc.arg('id');

-- name: GetPayrollPeriodForUpdate :one
SELECT *
FROM payroll.payroll_periods
WHERE id = sqlc.arg('id')
FOR UPDATE;

-- name: ListPayrollPeriods :many
SELECT *
FROM payroll.payroll_periods
WHERE tenant_id = sqlc.arg('tenant_id')
  AND (sqlc.narg('company_id')::platform.uuid_v7 IS NULL OR company_id = sqlc.narg('company_id')::platform.uuid_v7)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('cursor_id')::platform.uuid_v7 IS NULL OR id < sqlc.narg('cursor_id')::platform.uuid_v7)
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: CreatePayrollResult :one
INSERT INTO payroll.payroll_results (
    id,
    tenant_id,
    company_id,
    payroll_period_id,
    employee_id,
    employment_id,
    gross_income,
    taxable_income,
    take_home_pay
)
VALUES (
    sqlc.arg('id'),
    sqlc.arg('tenant_id'),
    sqlc.arg('company_id'),
    sqlc.arg('payroll_period_id'),
    sqlc.arg('employee_id'),
    sqlc.arg('employment_id'),
    sqlc.arg('gross_income'),
    sqlc.arg('taxable_income'),
    sqlc.arg('take_home_pay')
)
RETURNING *;

-- name: CreatePayrollResultItem :one
INSERT INTO payroll.payroll_result_items (id, payroll_result_id, component_code, component_type, amount)
VALUES (sqlc.arg('id'), sqlc.arg('payroll_result_id'), sqlc.arg('component_code'), sqlc.arg('component_type'), sqlc.arg('amount'))
RETURNING *;

-- name: CountPayrollResults :one
SELECT count(*)::bigint
FROM payroll.payroll_results
WHERE payroll_period_id = sqlc.arg('payroll_period_id');

-- name: FinalizePayrollResults :exec
UPDATE payroll.payroll_results
SET finalized_at = sqlc.arg('finalized_at')
WHERE payroll_period_id = sqlc.arg('payroll_period_id')
  AND finalized_at IS NULL;

-- name: FinalizePayrollPeriod :one
UPDATE payroll.payroll_periods
SET status = 'finalized', finalized_at = sqlc.arg('finalized_at')
WHERE id = sqlc.arg('id')
  AND status = 'open'
RETURNING *;

-- name: GetEmploymentPayrollReference :one
SELECT id, employee_id, company_id, join_date, end_date
FROM employee.employments
WHERE id = sqlc.arg('id');

-- name: GetEmployeePayrollReference :one
SELECT company_id
FROM employee.employees
WHERE id = sqlc.arg('id');

-- name: ListPayrollHistory :many
SELECT
    pr.id,
    pr.tenant_id,
    pr.company_id,
    pr.payroll_period_id,
    pr.employee_id,
    pr.employment_id,
    pr.gross_income,
    pr.taxable_income,
    pr.take_home_pay,
    pr.finalized_at,
    pr.created_at,
    pr.updated_at,
    pp.year AS period_year,
    pp.month AS period_month,
    pp.status AS period_status,
    pp.opened_at AS period_opened_at,
    pp.finalized_at AS period_finalized_at,
    pp.created_at AS period_created_at,
    pp.updated_at AS period_updated_at
FROM payroll.payroll_results pr
JOIN payroll.payroll_periods pp ON pp.id = pr.payroll_period_id
WHERE pr.tenant_id = sqlc.arg('tenant_id')
  AND pr.employee_id = sqlc.arg('employee_id')
  AND (sqlc.narg('year')::integer IS NULL OR pp.year = sqlc.narg('year')::integer)
  AND (sqlc.narg('cursor_id')::platform.uuid_v7 IS NULL OR pr.id < sqlc.narg('cursor_id')::platform.uuid_v7)
ORDER BY pr.id DESC
LIMIT sqlc.arg('limit');

-- name: ListPayrollResultItems :many
SELECT *
FROM payroll.payroll_result_items
WHERE payroll_result_id = sqlc.arg('payroll_result_id')
ORDER BY component_code ASC, id ASC;
