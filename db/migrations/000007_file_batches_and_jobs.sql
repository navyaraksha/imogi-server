-- Imogi file validation batches and durable background jobs, migration 000007.
--
-- Files remain in private object storage. PostgreSQL stores workflow state,
-- checksums, leases, and audit-friendly metadata only.

-- +goose Up

CREATE SCHEMA IF NOT EXISTS file;

CREATE TABLE platform.background_jobs (
    id                  platform.uuid_v7 PRIMARY KEY,
    tenant_id           platform.uuid_v7,
    company_id          platform.uuid_v7,
    job_type            text NOT NULL,
    queue_name          text NOT NULL DEFAULT 'default',
    status              text NOT NULL DEFAULT 'queued',
    priority            integer NOT NULL DEFAULT 100,
    payload             jsonb NOT NULL DEFAULT '{}'::jsonb,
    idempotency_key     text,
    available_at        timestamptz NOT NULL DEFAULT clock_timestamp(),
    attempt_count       integer NOT NULL DEFAULT 0,
    max_attempts        integer NOT NULL DEFAULT 5,
    lease_owner         text,
    lease_until         timestamptz,
    heartbeat_at        timestamptz,
    last_error_code     text,
    last_error_message  text,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at          timestamptz,
    completed_at        timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT background_jobs_tenant_company_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT background_jobs_scope_check
        CHECK ((tenant_id IS NULL AND company_id IS NULL) OR (tenant_id IS NOT NULL AND company_id IS NOT NULL)),
    CONSTRAINT background_jobs_type_check
        CHECK (btrim(job_type) <> '' AND char_length(job_type) <= 160),
    CONSTRAINT background_jobs_queue_check
        CHECK (btrim(queue_name) <> '' AND char_length(queue_name) <= 80),
    CONSTRAINT background_jobs_status_check
        CHECK (status IN ('queued', 'running', 'retry_scheduled', 'succeeded', 'failed', 'dead_letter', 'cancel_requested', 'canceled')),
    CONSTRAINT background_jobs_priority_check
        CHECK (priority BETWEEN 0 AND 1000),
    CONSTRAINT background_jobs_attempts_check
        CHECK (attempt_count >= 0 AND max_attempts BETWEEN 1 AND 20),
    CONSTRAINT background_jobs_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT background_jobs_lease_check
        CHECK (lease_until IS NULL OR lease_owner IS NOT NULL),
    CONSTRAINT background_jobs_completion_check
        CHECK (completed_at IS NULL OR status IN ('succeeded', 'failed', 'dead_letter', 'canceled'))
);

CREATE INDEX background_jobs_claim_idx
    ON platform.background_jobs (queue_name, priority DESC, available_at ASC, id ASC)
    WHERE status IN ('queued', 'retry_scheduled');
CREATE INDEX background_jobs_expired_lease_idx
    ON platform.background_jobs (queue_name, lease_until ASC)
    WHERE status = 'running';
CREATE INDEX background_jobs_tenant_idx
    ON platform.background_jobs (tenant_id, created_at DESC);
CREATE UNIQUE INDEX background_jobs_tenant_type_key_uq
    ON platform.background_jobs (tenant_id, job_type, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TRIGGER background_jobs_set_updated_at
BEFORE UPDATE ON platform.background_jobs
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER background_jobs_prevent_delete
BEFORE DELETE ON platform.background_jobs
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

CREATE TABLE platform.background_job_attempts (
    id              platform.uuid_v7 PRIMARY KEY,
    job_id          platform.uuid_v7 NOT NULL,
    attempt_number  integer NOT NULL,
    worker_id       text NOT NULL,
    started_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    finished_at     timestamptz,
    heartbeat_at    timestamptz,
    outcome         text,
    error_code      text,
    error_message   text,

    CONSTRAINT background_job_attempts_job_fk
        FOREIGN KEY (job_id) REFERENCES platform.background_jobs (id) ON DELETE RESTRICT,
    CONSTRAINT background_job_attempts_number_check
        CHECK (attempt_number > 0),
    CONSTRAINT background_job_attempts_outcome_check
        CHECK (outcome IS NULL OR outcome IN ('succeeded', 'retry', 'failed', 'dead_letter', 'canceled')),
    CONSTRAINT background_job_attempts_uq
        UNIQUE (job_id, attempt_number)
);

CREATE INDEX background_job_attempts_job_idx
    ON platform.background_job_attempts (job_id, attempt_number DESC);

CREATE TABLE file.file_objects (
    id                  platform.uuid_v7 PRIMARY KEY,
    tenant_id           platform.uuid_v7,
    company_id          platform.uuid_v7,
    storage_provider    text NOT NULL,
    object_key          text NOT NULL,
    original_filename   text NOT NULL,
    detected_extension  text NOT NULL,
    detected_mime_type  text NOT NULL,
    size_bytes          bigint NOT NULL,
    sha256              bytea NOT NULL,
    encryption_mode     text NOT NULL,
    status              text NOT NULL DEFAULT 'available',
    expires_at          timestamptz,
    legal_hold          boolean NOT NULL DEFAULT false,
    created_by          platform.uuid_v7,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT file_objects_tenant_company_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT file_objects_scope_check
        CHECK ((tenant_id IS NULL AND company_id IS NULL) OR (tenant_id IS NOT NULL AND company_id IS NOT NULL)),
    CONSTRAINT file_objects_created_by_fk
        FOREIGN KEY (created_by) REFERENCES platform.users (id) ON DELETE RESTRICT,
    CONSTRAINT file_objects_provider_check
        CHECK (storage_provider IN ('s3', 'filesystem')),
    CONSTRAINT file_objects_key_check
        CHECK (btrim(object_key) <> '' AND char_length(object_key) <= 1024),
    CONSTRAINT file_objects_filename_check
        CHECK (btrim(original_filename) <> '' AND char_length(original_filename) <= 255),
    CONSTRAINT file_objects_extension_check
        CHECK (detected_extension IN ('xlsx', 'xls', 'xlsm', 'csv', 'json', 'xml', 'ndjson')),
    CONSTRAINT file_objects_size_check
        CHECK (size_bytes >= 0),
    CONSTRAINT file_objects_sha256_check
        CHECK (octet_length(sha256) = 32),
    CONSTRAINT file_objects_encryption_check
        CHECK (encryption_mode IN ('sse-s3', 'sse-kms', 'filesystem-private')),
    CONSTRAINT file_objects_status_check
        CHECK (status IN ('pending', 'available', 'expired', 'deleted'))
);

CREATE UNIQUE INDEX file_objects_storage_key_uq
    ON file.file_objects (storage_provider, object_key);
CREATE INDEX file_objects_expiry_idx
    ON file.file_objects (expires_at)
    WHERE status = 'available' AND legal_hold = false;
CREATE INDEX file_objects_scope_idx
    ON file.file_objects (tenant_id, company_id, created_at DESC);

CREATE TRIGGER file_objects_set_updated_at
BEFORE UPDATE ON file.file_objects
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER file_objects_prevent_delete
BEFORE DELETE ON file.file_objects
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

CREATE TABLE file.import_templates (
    id              platform.uuid_v7 PRIMARY KEY,
    template_type   text NOT NULL,
    version         text NOT NULL,
    file_format     text NOT NULL,
    artifact_id     platform.uuid_v7,
    schema          jsonb NOT NULL DEFAULT '{}'::jsonb,
    status          text NOT NULL DEFAULT 'active',
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT import_templates_artifact_fk
        FOREIGN KEY (artifact_id) REFERENCES file.file_objects (id) ON DELETE RESTRICT,
    CONSTRAINT import_templates_type_check
        CHECK (btrim(template_type) <> '' AND char_length(template_type) <= 100),
    CONSTRAINT import_templates_version_check
        CHECK (btrim(version) <> '' AND char_length(version) <= 50),
    CONSTRAINT import_templates_format_check
        CHECK (file_format IN ('xlsx', 'xls', 'xlsm', 'csv', 'json')),
    CONSTRAINT import_templates_status_check
        CHECK (status IN ('active', 'retired')),
    CONSTRAINT import_templates_schema_check
        CHECK (jsonb_typeof(schema) = 'object'),
    CONSTRAINT import_templates_identity_uq
        UNIQUE (template_type, version, file_format)
);

CREATE TABLE file.import_batches (
    id                  platform.uuid_v7 PRIMARY KEY,
    tenant_id           platform.uuid_v7 NOT NULL,
    company_id          platform.uuid_v7 NOT NULL,
    operation           text NOT NULL,
    input_file_id       platform.uuid_v7 NOT NULL,
    template_id         platform.uuid_v7,
    created_by          platform.uuid_v7 NOT NULL,
    status              text NOT NULL DEFAULT 'uploading',
    total_rows          integer NOT NULL DEFAULT 0,
    valid_rows          integer NOT NULL DEFAULT 0,
    invalid_rows        integer NOT NULL DEFAULT 0,
    warning_rows        integer NOT NULL DEFAULT 0,
    committed_rows      integer NOT NULL DEFAULT 0,
    rejected_rows       integer NOT NULL DEFAULT 0,
    validation_started_at  timestamptz,
    validation_finished_at timestamptz,
    commit_started_at      timestamptz,
    commit_finished_at     timestamptz,
    expires_at          timestamptz,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT import_batches_scope_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT import_batches_input_file_fk
        FOREIGN KEY (input_file_id) REFERENCES file.file_objects (id) ON DELETE RESTRICT,
    CONSTRAINT import_batches_template_fk
        FOREIGN KEY (template_id) REFERENCES file.import_templates (id) ON DELETE RESTRICT,
    CONSTRAINT import_batches_created_by_fk
        FOREIGN KEY (created_by) REFERENCES platform.users (id) ON DELETE RESTRICT,
    CONSTRAINT import_batches_operation_check
        CHECK (operation IN ('employee_master', 'employment_history', 'assignment_history', 'tax_profile_history', 'payroll_ledger')),
    CONSTRAINT import_batches_status_check
        CHECK (status IN ('uploading', 'uploaded', 'validating', 'validated', 'validation_failed', 'commit_requested', 'committing', 'completed', 'commit_failed', 'cancel_requested', 'canceled')),
    CONSTRAINT import_batches_counts_check
        CHECK (total_rows >= 0 AND valid_rows >= 0 AND invalid_rows >= 0 AND warning_rows >= 0 AND committed_rows >= 0 AND rejected_rows >= 0)
);

CREATE INDEX import_batches_scope_idx
    ON file.import_batches (tenant_id, company_id, created_at DESC);
CREATE INDEX import_batches_status_idx
    ON file.import_batches (status, updated_at ASC);

CREATE TRIGGER import_batches_set_updated_at
BEFORE UPDATE ON file.import_batches
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER import_batches_prevent_delete
BEFORE DELETE ON file.import_batches
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

CREATE TABLE file.import_batch_steps (
    id              platform.uuid_v7 PRIMARY KEY,
    batch_id        platform.uuid_v7 NOT NULL,
    step_type       text NOT NULL,
    job_id          platform.uuid_v7 NOT NULL,
    status          text NOT NULL DEFAULT 'queued',
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at      timestamptz,
    finished_at     timestamptz,

    CONSTRAINT import_batch_steps_batch_fk
        FOREIGN KEY (batch_id) REFERENCES file.import_batches (id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_steps_job_fk
        FOREIGN KEY (job_id) REFERENCES platform.background_jobs (id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_steps_type_check
        CHECK (step_type IN ('validation', 'commit')),
    CONSTRAINT import_batch_steps_status_check
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled')),
    CONSTRAINT import_batch_steps_identity_uq
        UNIQUE (batch_id, step_type)
);

CREATE INDEX import_batch_steps_job_idx
    ON file.import_batch_steps (job_id);

CREATE TABLE file.batch_artifacts (
    id              platform.uuid_v7 PRIMARY KEY,
    batch_id        platform.uuid_v7 NOT NULL,
    file_object_id  platform.uuid_v7 NOT NULL,
    role            text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT batch_artifacts_batch_fk
        FOREIGN KEY (batch_id) REFERENCES file.import_batches (id) ON DELETE RESTRICT,
    CONSTRAINT batch_artifacts_file_fk
        FOREIGN KEY (file_object_id) REFERENCES file.file_objects (id) ON DELETE RESTRICT,
    CONSTRAINT batch_artifacts_role_check
        CHECK (role IN ('input', 'validation_summary', 'validation_errors', 'normalized', 'import_receipt', 'output_xml', 'error_report')),
    CONSTRAINT batch_artifacts_role_uq
        UNIQUE (batch_id, role)
);

CREATE INDEX batch_artifacts_file_idx
    ON file.batch_artifacts (file_object_id);

-- +goose Down

DROP TABLE IF EXISTS file.batch_artifacts;
DROP TABLE IF EXISTS file.import_batch_steps;
DROP TABLE IF EXISTS file.import_batches;
DROP TABLE IF EXISTS file.import_templates;
DROP TABLE IF EXISTS file.file_objects;
DROP TABLE IF EXISTS platform.background_job_attempts;
DROP TABLE IF EXISTS platform.background_jobs;
DROP SCHEMA IF EXISTS file;
