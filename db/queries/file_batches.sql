-- name: CreateFileObject :one
INSERT INTO file.file_objects (
    id, tenant_id, company_id, storage_provider, object_key, original_filename,
    detected_extension, detected_mime_type, size_bytes, sha256, encryption_mode,
    status, expires_at, created_by
)
VALUES (
    sqlc.arg('id'), sqlc.arg('tenant_id'), sqlc.arg('company_id'),
    sqlc.arg('storage_provider'), sqlc.arg('object_key'), sqlc.arg('original_filename'),
    sqlc.arg('detected_extension'), sqlc.arg('detected_mime_type'), sqlc.arg('size_bytes'),
    sqlc.arg('sha256'), sqlc.arg('encryption_mode'), sqlc.arg('status'),
    sqlc.narg('expires_at'), sqlc.narg('created_by')
)
RETURNING *;

-- name: CancelBackgroundJobsForBatch :exec
UPDATE platform.background_jobs AS job
SET status = CASE
        WHEN job.status IN ('queued', 'retry_scheduled') THEN 'canceled'
        ELSE 'cancel_requested'
    END,
    completed_at = CASE
        WHEN job.status IN ('queued', 'retry_scheduled') THEN clock_timestamp()
        ELSE job.completed_at
    END
FROM file.import_batch_steps AS step
WHERE step.batch_id = sqlc.arg('batch_id')
  AND step.job_id = job.id
  AND job.status IN ('queued', 'retry_scheduled', 'running');

-- name: GetFileObject :one
SELECT * FROM file.file_objects WHERE id = sqlc.arg('id');

-- name: ListImportTemplates :many
SELECT id, tenant_id, company_id, template_type, version, file_format, status
FROM file.import_templates
WHERE status = 'active'
  AND (tenant_id IS NULL OR tenant_id = sqlc.arg('tenant_id'))
ORDER BY template_type ASC, version ASC, file_format ASC, id ASC;

-- name: GetImportTemplate :one
SELECT *
FROM file.import_templates
WHERE id = sqlc.arg('id');

-- name: CreateImportTemplate :one
INSERT INTO file.import_templates (
    id, tenant_id, company_id, template_type, version, file_format, configuration, status
)
VALUES (
    sqlc.arg('id'), sqlc.arg('tenant_id'), sqlc.arg('company_id'), sqlc.arg('template_type'),
    sqlc.arg('version'), sqlc.arg('file_format'), sqlc.arg('configuration'), 'active'
)
RETURNING *;

-- name: RetireImportTemplate :one
UPDATE file.import_templates
SET status = 'retired'
WHERE id = sqlc.arg('id') AND status = 'active'
RETURNING *;

-- name: CreateImportBatchPayrollContext :one
INSERT INTO file.import_batch_payroll_context (
    batch_id, tenant_id, company_id, tax_year, tax_month, coverage_from,
    coverage_to, pay_date, run_type, correction_of_run_id
)
VALUES (
    sqlc.arg('batch_id'), sqlc.arg('tenant_id'), sqlc.arg('company_id'),
    sqlc.arg('tax_year'), sqlc.arg('tax_month'), sqlc.arg('coverage_from'),
    sqlc.arg('coverage_to'), sqlc.narg('pay_date'), sqlc.arg('run_type'), sqlc.narg('correction_of_run_id')
)
RETURNING *;

-- name: GetImportBatchPayrollContext :one
SELECT * FROM file.import_batch_payroll_context WHERE batch_id = sqlc.arg('batch_id');

-- name: LinkImportBatchPayrollContext :one
UPDATE file.import_batch_payroll_context
SET payroll_period_id = sqlc.narg('payroll_period_id'),
    payroll_run_id = sqlc.narg('payroll_run_id'),
    updated_at = clock_timestamp()
WHERE batch_id = sqlc.arg('batch_id')
RETURNING *;

-- name: MarkFileObjectAvailable :one
UPDATE file.file_objects
SET status = 'available', size_bytes = sqlc.arg('size_bytes'), sha256 = sqlc.arg('sha256')
WHERE id = sqlc.arg('id') AND status = 'pending'
RETURNING *;

-- name: CreateImportBatch :one
INSERT INTO file.import_batches (
    id, tenant_id, company_id, operation, input_file_id, template_id, created_by, expires_at
)
VALUES (
    sqlc.arg('id'), sqlc.arg('tenant_id'), sqlc.arg('company_id'), sqlc.arg('operation'),
    sqlc.arg('input_file_id'), sqlc.narg('template_id'), sqlc.arg('created_by'), sqlc.narg('expires_at')
)
RETURNING *;

-- name: GetImportBatch :one
SELECT * FROM file.import_batches WHERE id = sqlc.arg('id');

-- name: UpdateImportBatchStatus :one
UPDATE file.import_batches
SET status = sqlc.arg('status'),
    validation_started_at = COALESCE(sqlc.narg('validation_started_at'), validation_started_at),
    validation_finished_at = COALESCE(sqlc.narg('validation_finished_at'), validation_finished_at),
    commit_started_at = COALESCE(sqlc.narg('commit_started_at'), commit_started_at),
    commit_finished_at = COALESCE(sqlc.narg('commit_finished_at'), commit_finished_at),
    total_rows = COALESCE(sqlc.narg('total_rows'), total_rows),
    valid_rows = COALESCE(sqlc.narg('valid_rows'), valid_rows),
    invalid_rows = COALESCE(sqlc.narg('invalid_rows'), invalid_rows),
    warning_rows = COALESCE(sqlc.narg('warning_rows'), warning_rows),
    committed_rows = COALESCE(sqlc.narg('committed_rows'), committed_rows),
    rejected_rows = COALESCE(sqlc.narg('rejected_rows'), rejected_rows)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: RequestImportBatchCommit :one
UPDATE file.import_batches
SET status = 'commit_requested'
WHERE id = sqlc.arg('id')
  AND status = 'validated'
  AND invalid_rows = 0
RETURNING *;

-- name: CreateImportBatchStep :one
INSERT INTO file.import_batch_steps (id, batch_id, step_type, job_id)
VALUES (sqlc.arg('id'), sqlc.arg('batch_id'), sqlc.arg('step_type'), sqlc.arg('job_id'))
RETURNING *;

-- name: UpdateImportBatchStepStatus :one
UPDATE file.import_batch_steps
SET status = sqlc.arg('status'),
    started_at = COALESCE(sqlc.narg('started_at'), started_at),
    finished_at = COALESCE(sqlc.narg('finished_at'), finished_at)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateBatchArtifact :one
INSERT INTO file.batch_artifacts (id, batch_id, file_object_id, role)
VALUES (sqlc.arg('id'), sqlc.arg('batch_id'), sqlc.arg('file_object_id'), sqlc.arg('role'))
ON CONFLICT (batch_id, role) DO UPDATE
SET file_object_id = EXCLUDED.file_object_id
RETURNING *;

-- name: ListBatchArtifacts :many
SELECT * FROM file.batch_artifacts WHERE batch_id = sqlc.arg('batch_id') ORDER BY created_at ASC, id ASC;

-- name: ListImportValidationSheets :many
SELECT
    sheet_name,
    count(*)::bigint AS total_rows,
    count(*) FILTER (WHERE blocking_issue_count = 0)::bigint AS valid_rows,
    count(*) FILTER (WHERE blocking_issue_count > 0)::bigint AS invalid_rows,
    count(*) FILTER (WHERE blocking_issue_count > 0)::bigint AS blocking_rows
FROM file.import_batch_rows
WHERE batch_id = sqlc.arg('batch_id')
GROUP BY sheet_name
ORDER BY sheet_name ASC;

-- name: ListImportValidationIssues :many
SELECT id, row_id, sheet_name, row_no, field_name, error_code, severity,
       description, masked_value, candidate_count
FROM file.import_row_issues
WHERE batch_id = sqlc.arg('batch_id')
ORDER BY sheet_name ASC, row_no ASC, id ASC;

-- name: ClearImportBatchValidationRows :exec
DELETE FROM file.import_batch_rows
WHERE batch_id = sqlc.arg('batch_id');

-- name: CreateImportBatchRow :one
INSERT INTO file.import_batch_rows (
    id, batch_id, sheet_name, row_no, source_employee_number,
    source_full_name, match_status, normalized_payload, issue_count,
    blocking_issue_count, employee_id, employment_id
)
VALUES (
    sqlc.arg('id'), sqlc.arg('batch_id'), sqlc.arg('sheet_name'), sqlc.arg('row_no'),
    sqlc.narg('source_employee_number'), sqlc.narg('source_full_name'),
    sqlc.arg('match_status'), sqlc.arg('normalized_payload'), sqlc.arg('issue_count'),
    sqlc.arg('blocking_issue_count'), sqlc.narg('employee_id'), sqlc.narg('employment_id')
)
RETURNING *;

-- name: ListImportBatchRows :many
SELECT *
FROM file.import_batch_rows
WHERE batch_id = sqlc.arg('batch_id')
ORDER BY sheet_name ASC, row_no ASC;

-- name: CreateImportRowEffect :one
INSERT INTO file.import_row_effects (id, batch_id, row_id, entity_type, entity_id, action)
VALUES (sqlc.arg('id'), sqlc.arg('batch_id'), sqlc.arg('row_id'), sqlc.arg('entity_type'), sqlc.arg('entity_id'), sqlc.arg('action'))
ON CONFLICT (batch_id, row_id, entity_type, entity_id, action) DO UPDATE SET created_at = file.import_row_effects.created_at
RETURNING *;

-- name: CreateImportRowIssue :one
INSERT INTO file.import_row_issues (
    id, row_id, batch_id, sheet_name, row_no, field_name, error_code,
    severity, description, masked_value, candidate_count, details
)
VALUES (
    sqlc.arg('id'), sqlc.arg('row_id'), sqlc.arg('batch_id'), sqlc.arg('sheet_name'),
    sqlc.arg('row_no'), sqlc.narg('field_name'), sqlc.arg('error_code'),
    sqlc.arg('severity'), sqlc.arg('description'), sqlc.narg('masked_value'),
    sqlc.arg('candidate_count'), sqlc.arg('details')
)
RETURNING *;

-- name: ResolveImportBatchRow :one
WITH selected_batch AS (
    SELECT tenant_id, company_id
    FROM file.import_batches
    WHERE id = sqlc.arg('batch_id')
), updated_row AS (
    UPDATE file.import_batch_rows row
    SET match_status = 'resolved',
        employee_id = sqlc.narg('employee_id'),
        employment_id = sqlc.narg('employment_id'),
        blocking_issue_count = 0,
        updated_at = clock_timestamp()
    WHERE row.id = sqlc.arg('row_id')
      AND row.batch_id = sqlc.arg('batch_id')
      AND (
          sqlc.narg('employee_id')::platform.uuid_v7 IS NULL
          OR EXISTS (
              SELECT 1
              FROM employee.employees employee
              JOIN selected_batch batch ON batch.tenant_id = employee.tenant_id AND batch.company_id = employee.company_id
              WHERE employee.id = sqlc.narg('employee_id')::platform.uuid_v7
          )
      )
    RETURNING row.batch_id
)
UPDATE file.import_batches batch
SET total_rows = counts.total_rows,
    valid_rows = counts.valid_rows,
    invalid_rows = counts.invalid_rows,
    status = CASE WHEN counts.invalid_rows = 0 THEN 'validated' ELSE 'validation_failed' END,
    updated_at = clock_timestamp()
FROM (
    SELECT rows.batch_id,
           count(*)::integer AS total_rows,
           count(*) FILTER (WHERE blocking_issue_count = 0)::integer AS valid_rows,
           count(*) FILTER (WHERE blocking_issue_count > 0)::integer AS invalid_rows
    FROM file.import_batch_rows rows
    WHERE rows.batch_id = sqlc.arg('batch_id')
    GROUP BY rows.batch_id
) counts
WHERE batch.id = counts.batch_id
  AND batch.id IN (SELECT batch_id FROM updated_row)
RETURNING batch.id;

-- name: CreateImportRowDecision :one
INSERT INTO file.import_row_decisions (
    id, row_id, employee_id, employment_id, decision, reason, decided_by
)
VALUES (
    sqlc.arg('id'), sqlc.arg('row_id'), sqlc.narg('employee_id'), sqlc.narg('employment_id'),
    sqlc.arg('decision'), sqlc.arg('reason'), sqlc.arg('decided_by')
)
RETURNING *;
