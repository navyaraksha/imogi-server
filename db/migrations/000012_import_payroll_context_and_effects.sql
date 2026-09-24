-- Import payroll context, coverage dates, and row-level commit receipts.
-- Goose migration 000012.

-- +goose Up

ALTER TABLE payroll.payroll_runs
    ADD COLUMN coverage_from date,
    ADD COLUMN coverage_to date,
    ADD COLUMN pay_date date;

UPDATE payroll.payroll_runs
SET coverage_from = date_trunc('month', run_date)::date,
    coverage_to = (date_trunc('month', run_date) + INTERVAL '1 month - 1 day')::date
WHERE coverage_from IS NULL OR coverage_to IS NULL;

ALTER TABLE payroll.payroll_runs
    ALTER COLUMN coverage_from SET NOT NULL,
    ALTER COLUMN coverage_to SET NOT NULL,
    ADD CONSTRAINT payroll_runs_coverage_order_check CHECK (coverage_from <= coverage_to),
    ADD CONSTRAINT payroll_runs_pay_date_check CHECK (pay_date IS NULL OR pay_date >= coverage_from),
    ADD CONSTRAINT payroll_runs_source_batch_uq UNIQUE (source_batch_id);

CREATE INDEX payroll_runs_coverage_idx
    ON payroll.payroll_runs (tenant_id, company_id, coverage_from, coverage_to);

-- The import batch stores the exact business context selected by the operator.
-- A payroll file must not infer its period from the upload timestamp or filename.
CREATE TABLE file.import_batch_payroll_context (
    batch_id            platform.uuid_v7 PRIMARY KEY,
    tenant_id           platform.uuid_v7 NOT NULL,
    company_id          platform.uuid_v7 NOT NULL,
    tax_year            integer NOT NULL,
    tax_month           integer NOT NULL,
    coverage_from       date NOT NULL,
    coverage_to         date NOT NULL,
    pay_date            date,
    run_type            text NOT NULL,
    payroll_period_id   platform.uuid_v7,
    payroll_run_id      platform.uuid_v7,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT import_batch_payroll_context_batch_fk
        FOREIGN KEY (batch_id) REFERENCES file.import_batches (id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_payroll_context_company_fk
        FOREIGN KEY (tenant_id, company_id) REFERENCES organization.companies (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_payroll_context_period_fk
        FOREIGN KEY (tenant_id, company_id, payroll_period_id)
        REFERENCES payroll.payroll_periods (tenant_id, company_id, id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_payroll_context_run_fk
        FOREIGN KEY (tenant_id, company_id, payroll_run_id)
        REFERENCES payroll.payroll_runs (tenant_id, company_id, id) ON DELETE RESTRICT,
    CONSTRAINT import_batch_payroll_context_year_check CHECK (tax_year BETWEEN 2000 AND 2200),
    CONSTRAINT import_batch_payroll_context_month_check CHECK (tax_month BETWEEN 1 AND 12),
    CONSTRAINT import_batch_payroll_context_coverage_check CHECK (coverage_from <= coverage_to),
    CONSTRAINT import_batch_payroll_context_run_type_check
        CHECK (run_type IN ('regular', 'overtime', 'thr', 'bonus', 'correction', 'reversal')),
    CONSTRAINT import_batch_payroll_context_period_pair_check
        CHECK ((payroll_period_id IS NULL AND payroll_run_id IS NULL) OR payroll_period_id IS NOT NULL),
    CONSTRAINT import_batch_payroll_context_period_matches_run_check
        CHECK (payroll_run_id IS NULL OR payroll_period_id IS NOT NULL)
);

CREATE INDEX import_batch_payroll_context_scope_idx
    ON file.import_batch_payroll_context (tenant_id, company_id, tax_year, tax_month);
CREATE INDEX import_batch_payroll_context_run_idx
    ON file.import_batch_payroll_context (payroll_run_id)
    WHERE payroll_run_id IS NOT NULL;

CREATE TRIGGER import_batch_payroll_context_set_updated_at
BEFORE UPDATE ON file.import_batch_payroll_context
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER import_batch_payroll_context_prevent_delete
BEFORE DELETE ON file.import_batch_payroll_context
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

-- Effects are the durable bridge from a validated source row to domain records.
-- They make a receipt explainable without retaining raw sensitive source values.
CREATE TABLE file.import_row_effects (
    id              platform.uuid_v7 PRIMARY KEY,
    batch_id        platform.uuid_v7 NOT NULL,
    row_id          platform.uuid_v7 NOT NULL,
    entity_type     text NOT NULL,
    entity_id       platform.uuid_v7 NOT NULL,
    action          text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT import_row_effects_batch_fk
        FOREIGN KEY (batch_id) REFERENCES file.import_batches (id) ON DELETE RESTRICT,
    CONSTRAINT import_row_effects_row_fk
        FOREIGN KEY (row_id) REFERENCES file.import_batch_rows (id) ON DELETE RESTRICT,
    CONSTRAINT import_row_effects_entity_type_check
        CHECK (entity_type IN ('employee', 'employment', 'assignment', 'tax_profile', 'payroll_period', 'payroll_run', 'payroll_result')),
    CONSTRAINT import_row_effects_action_check
        CHECK (action IN ('created', 'updated', 'replaced', 'linked')),
    CONSTRAINT import_row_effects_uq UNIQUE (batch_id, row_id, entity_type, entity_id, action)
);

CREATE INDEX import_row_effects_batch_idx
    ON file.import_row_effects (batch_id, created_at ASC);
CREATE INDEX import_row_effects_entity_idx
    ON file.import_row_effects (entity_type, entity_id, created_at DESC);

-- +goose Down

DROP TABLE IF EXISTS file.import_row_effects;
DROP TABLE IF EXISTS file.import_batch_payroll_context;
DROP INDEX IF EXISTS payroll_runs_coverage_idx;
ALTER TABLE payroll.payroll_runs
    DROP CONSTRAINT IF EXISTS payroll_runs_source_batch_uq,
    DROP CONSTRAINT IF EXISTS payroll_runs_pay_date_check,
    DROP CONSTRAINT IF EXISTS payroll_runs_coverage_order_check,
    DROP COLUMN IF EXISTS pay_date,
    DROP COLUMN IF EXISTS coverage_to,
    DROP COLUMN IF EXISTS coverage_from;
