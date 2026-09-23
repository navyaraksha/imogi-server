-- name: CreateBackgroundJob :one
INSERT INTO platform.background_jobs (
    id, tenant_id, company_id, job_type, queue_name, payload, idempotency_key,
    available_at, max_attempts
)
VALUES (
    sqlc.arg('id'), sqlc.narg('tenant_id'), sqlc.narg('company_id'),
    sqlc.arg('job_type'), sqlc.arg('queue_name'), sqlc.arg('payload'),
    sqlc.narg('idempotency_key'), sqlc.arg('available_at'), sqlc.arg('max_attempts')
)
RETURNING *;

-- name: GetBackgroundJob :one
SELECT *
FROM platform.background_jobs
WHERE id = sqlc.arg('id');

-- name: ClaimBackgroundJob :one
WITH candidate AS (
    SELECT id
    FROM platform.background_jobs AS candidate_job
    WHERE candidate_job.queue_name = sqlc.arg('queue_name')
      AND (
          (candidate_job.status IN ('queued', 'retry_scheduled') AND candidate_job.available_at <= clock_timestamp())
          OR
          (candidate_job.status = 'running' AND candidate_job.lease_until < clock_timestamp())
      )
    ORDER BY candidate_job.priority DESC, candidate_job.available_at ASC, candidate_job.id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE platform.background_jobs AS job
SET status = 'running',
    lease_owner = sqlc.arg('worker_id'),
    lease_until = clock_timestamp() + sqlc.arg('lease_duration')::interval,
    heartbeat_at = clock_timestamp(),
    attempt_count = job.attempt_count + 1,
    started_at = COALESCE(job.started_at, clock_timestamp()),
    updated_at = clock_timestamp()
FROM candidate
WHERE job.id = candidate.id
RETURNING job.*;

-- name: HeartbeatBackgroundJob :exec
UPDATE platform.background_jobs
SET lease_until = clock_timestamp() + sqlc.arg('lease_duration')::interval,
    heartbeat_at = clock_timestamp()
WHERE id = sqlc.arg('id')
  AND status = 'running'
  AND lease_owner = sqlc.arg('worker_id');

-- name: MarkBackgroundJobSucceeded :one
UPDATE platform.background_jobs
SET status = 'succeeded',
    lease_owner = NULL,
    lease_until = NULL,
    heartbeat_at = NULL,
    completed_at = clock_timestamp()
WHERE id = sqlc.arg('id')
  AND status = 'running'
  AND lease_owner = sqlc.arg('worker_id')
RETURNING *;

-- name: MarkBackgroundJobRetry :one
UPDATE platform.background_jobs
SET status = 'retry_scheduled',
    available_at = sqlc.arg('available_at'),
    lease_owner = NULL,
    lease_until = NULL,
    heartbeat_at = NULL,
    last_error_code = sqlc.arg('error_code'),
    last_error_message = sqlc.arg('error_message')
WHERE id = sqlc.arg('id')
  AND status = 'running'
  AND lease_owner = sqlc.arg('worker_id')
RETURNING *;

-- name: MarkBackgroundJobFailed :one
UPDATE platform.background_jobs
SET status = CASE WHEN attempt_count >= max_attempts THEN 'dead_letter' ELSE 'failed' END,
    lease_owner = NULL,
    lease_until = NULL,
    heartbeat_at = NULL,
    completed_at = clock_timestamp(),
    last_error_code = sqlc.arg('error_code'),
    last_error_message = sqlc.arg('error_message')
WHERE id = sqlc.arg('id')
  AND status = 'running'
  AND lease_owner = sqlc.arg('worker_id')
RETURNING *;

-- name: MarkBackgroundJobCanceled :one
UPDATE platform.background_jobs
SET status = 'canceled',
    lease_owner = NULL,
    lease_until = NULL,
    heartbeat_at = NULL,
    completed_at = clock_timestamp()
WHERE id = sqlc.arg('id')
  AND status IN ('running', 'cancel_requested')
  AND (lease_owner = sqlc.arg('worker_id') OR status = 'cancel_requested')
RETURNING *;

-- name: RequestBackgroundJobCancellation :one
UPDATE platform.background_jobs
SET status = CASE WHEN status = 'queued' OR status = 'retry_scheduled' THEN 'canceled' ELSE 'cancel_requested' END,
    completed_at = CASE WHEN status = 'queued' OR status = 'retry_scheduled' THEN clock_timestamp() ELSE completed_at END
WHERE id = sqlc.arg('id')
  AND status IN ('queued', 'retry_scheduled', 'running')
RETURNING *;

-- name: FinishBackgroundJobAttempt :exec
UPDATE platform.background_job_attempts
SET finished_at = clock_timestamp(),
    outcome = sqlc.arg('outcome'),
    error_code = sqlc.narg('error_code'),
    error_message = sqlc.narg('error_message')
WHERE job_id = sqlc.arg('job_id')
  AND attempt_number = sqlc.arg('attempt_number');

-- name: CreateBackgroundJobAttempt :one
INSERT INTO platform.background_job_attempts (id, job_id, attempt_number, worker_id)
VALUES (sqlc.arg('id'), sqlc.arg('job_id'), sqlc.arg('attempt_number'), sqlc.arg('worker_id'))
RETURNING *;
