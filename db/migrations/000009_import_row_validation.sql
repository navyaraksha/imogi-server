-- Imogi row-level validation state, migration 000009.

-- +goose Up

CREATE TABLE file.import_batch_rows (
    id                      platform.uuid_v7 PRIMARY KEY,
    batch_id                platform.uuid_v7 NOT NULL,
    sheet_name              text NOT NULL DEFAULT 'Sheet1',
    row_no                  integer NOT NULL,
    source_employee_number  text,
    source_full_name        text,
    match_status            text NOT NULL DEFAULT 'unresolved',
    employee_id             platform.uuid_v7,
    employment_id           platform.uuid_v7,
    normalized_payload      jsonb NOT NULL DEFAULT '{}'::jsonb,
    issue_count             integer NOT NULL DEFAULT 0,
    blocking_issue_count    integer NOT NULL DEFAULT 0,
    created_at              timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at              timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT import_batch_rows_batch_fk
        FOREIGN KEY (batch_id) REFERENCES file.import_batches (id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_rows_employee_fk
        FOREIGN KEY (employee_id) REFERENCES employee.employees (id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_rows_employment_fk
        FOREIGN KEY (employment_id) REFERENCES employee.employments (id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_rows_sheet_check
        CHECK (btrim(sheet_name) <> '' AND char_length(sheet_name) <= 255),
    CONSTRAINT import_batch_rows_number_check
        CHECK (row_no > 0),
    CONSTRAINT import_batch_rows_match_check
        CHECK (match_status IN ('matched', 'unmatched', 'ambiguous', 'conflict', 'unresolved', 'resolved')),
    CONSTRAINT import_batch_rows_payload_check
        CHECK (jsonb_typeof(normalized_payload) = 'object'),
    CONSTRAINT import_batch_rows_count_check
        CHECK (issue_count >= 0 AND blocking_issue_count >= 0 AND blocking_issue_count <= issue_count),
    CONSTRAINT import_batch_rows_position_uq
        UNIQUE (batch_id, sheet_name, row_no)
);

CREATE INDEX import_batch_rows_batch_status_idx
    ON file.import_batch_rows (batch_id, match_status, row_no);
CREATE INDEX import_batch_rows_employee_idx
    ON file.import_batch_rows (employee_id, created_at DESC)
    WHERE employee_id IS NOT NULL;

CREATE TABLE file.import_row_issues (
    id              platform.uuid_v7 PRIMARY KEY,
    row_id          platform.uuid_v7 NOT NULL,
    batch_id        platform.uuid_v7 NOT NULL,
    sheet_name      text NOT NULL,
    row_no          integer NOT NULL,
    field_name      text,
    error_code      text NOT NULL,
    severity        text NOT NULL,
    description     text NOT NULL,
    masked_value    text,
    candidate_count integer NOT NULL DEFAULT 0,
    details         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT import_row_issues_row_fk
        FOREIGN KEY (row_id) REFERENCES file.import_batch_rows (id) ON DELETE CASCADE,
    CONSTRAINT import_row_issues_batch_fk
        FOREIGN KEY (batch_id) REFERENCES file.import_batches (id) ON DELETE RESTRICT,
    CONSTRAINT import_row_issues_severity_check
        CHECK (severity IN ('warning', 'error')),
    CONSTRAINT import_row_issues_code_check
        CHECK (btrim(error_code) <> '' AND char_length(error_code) <= 100),
    CONSTRAINT import_row_issues_description_check
        CHECK (btrim(description) <> '' AND char_length(description) <= 1000),
    CONSTRAINT import_row_issues_candidate_count_check
        CHECK (candidate_count >= 0),
    CONSTRAINT import_row_issues_details_check
        CHECK (jsonb_typeof(details) = 'object')
);

CREATE INDEX import_row_issues_batch_idx
    ON file.import_row_issues (batch_id, severity, row_no);
CREATE INDEX import_row_issues_row_idx
    ON file.import_row_issues (row_id, created_at ASC);

CREATE TABLE file.import_row_decisions (
    id              platform.uuid_v7 PRIMARY KEY,
    row_id          platform.uuid_v7 NOT NULL,
    employee_id     platform.uuid_v7,
    employment_id   platform.uuid_v7,
    decision        text NOT NULL,
    reason          text NOT NULL,
    decided_by      platform.uuid_v7 NOT NULL,
    decided_at      timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT import_row_decisions_row_fk
        FOREIGN KEY (row_id) REFERENCES file.import_batch_rows (id) ON DELETE CASCADE,
    CONSTRAINT import_row_decisions_employee_fk
        FOREIGN KEY (employee_id) REFERENCES employee.employees (id) ON DELETE RESTRICT,
    CONSTRAINT import_row_decisions_employment_fk
        FOREIGN KEY (employment_id) REFERENCES employee.employments (id) ON DELETE RESTRICT,
    CONSTRAINT import_row_decisions_decision_check
        CHECK (decision IN ('accept', 'reject', 'map')),
    CONSTRAINT import_row_decisions_reason_check
        CHECK (btrim(reason) <> '' AND char_length(reason) <= 1000),
    CONSTRAINT import_row_decisions_target_check
        CHECK (
            (decision = 'map' AND employee_id IS NOT NULL)
            OR (decision IN ('accept', 'reject'))
        )
);

CREATE INDEX import_row_decisions_row_idx
    ON file.import_row_decisions (row_id, decided_at DESC);

-- +goose Down

DROP TABLE IF EXISTS file.import_row_decisions;
DROP TABLE IF EXISTS file.import_row_issues;
DROP TABLE IF EXISTS file.import_batch_rows;
