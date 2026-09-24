-- name: CreateEmployeeNumberHistory :one
INSERT INTO employee.employee_number_history (
    id, tenant_id, company_id, employee_id, employment_id, employee_number,
    number_type, effective_from, effective_to, source, source_batch_id,
    supersedes_id, correction_reason, created_by
)
VALUES (
    sqlc.arg('id'), sqlc.arg('tenant_id'), sqlc.arg('company_id'), sqlc.arg('employee_id'),
    sqlc.narg('employment_id'), sqlc.arg('employee_number'), sqlc.arg('number_type'),
    sqlc.arg('effective_from'), sqlc.narg('effective_to'), sqlc.arg('source'),
    sqlc.narg('source_batch_id'), sqlc.narg('supersedes_id'), sqlc.narg('correction_reason'),
    sqlc.narg('created_by')
)
RETURNING *;

-- name: ListEmployeeNumberHistory :many
SELECT *
FROM employee.employee_number_history
WHERE employee_id = sqlc.arg('employee_id')
ORDER BY effective_from DESC, id DESC;

-- name: CloseOpenEmployeeNumberHistory :exec
UPDATE employee.employee_number_history
SET effective_to = sqlc.arg('effective_to')
WHERE employee_id = sqlc.arg('employee_id')
  AND effective_to IS NULL
  AND effective_from < sqlc.arg('effective_from');
