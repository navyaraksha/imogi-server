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
SELECT id, template_type, version, file_format, status
FROM file.import_templates
WHERE status = 'active'
ORDER BY template_type ASC, version ASC, file_format ASC, id ASC;

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
RETURNING *;

-- name: ListBatchArtifacts :many
SELECT * FROM file.batch_artifacts WHERE batch_id = sqlc.arg('batch_id') ORDER BY created_at ASC, id ASC;
