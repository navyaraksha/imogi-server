-- Imogi payroll runs within a calendar payroll period, migration 000010.

-- +goose Up

CREATE TABLE payroll.payroll_runs (
    id                  platform.uuid_v7 PRIMARY KEY,
    tenant_id           platform.uuid_v7 NOT NULL,
    company_id          platform.uuid_v7 NOT NULL,
    payroll_period_id   platform.uuid_v7 NOT NULL,
    run_type            text NOT NULL,
    run_date            date NOT NULL,
    sequence_no         integer NOT NULL DEFAULT 1,
    source_batch_id     platform.uuid_v7,
    correction_of_run_id platform.uuid_v7,
    status              text NOT NULL DEFAULT 'draft',
    finalized_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT payroll_runs_period_fk
        FOREIGN KEY (tenant_id, company_id, payroll_period_id)
        REFERENCES payroll.payroll_periods (tenant_id, company_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT payroll_runs_source_batch_fk
        FOREIGN KEY (source_batch_id) REFERENCES file.import_batches (id) ON DELETE RESTRICT,
    CONSTRAINT payroll_runs_correction_fk
        FOREIGN KEY (correction_of_run_id) REFERENCES payroll.payroll_runs (id) ON DELETE RESTRICT,
    CONSTRAINT payroll_runs_run_type_check
        CHECK (run_type IN ('regular', 'overtime', 'thr', 'bonus', 'correction', 'reversal')),
    CONSTRAINT payroll_runs_sequence_check
        CHECK (sequence_no > 0),
    CONSTRAINT payroll_runs_status_check
        CHECK (status IN ('draft', 'finalized', 'voided')),
    CONSTRAINT payroll_runs_finalization_check
        CHECK ((status = 'finalized' AND finalized_at IS NOT NULL) OR (status <> 'finalized' AND finalized_at IS NULL)),
    CONSTRAINT payroll_runs_correction_check
        CHECK ((run_type IN ('correction', 'reversal') AND correction_of_run_id IS NOT NULL) OR run_type NOT IN ('correction', 'reversal')),
    CONSTRAINT payroll_runs_identity_uq
        UNIQUE (tenant_id, company_id, payroll_period_id, run_type, sequence_no),
    CONSTRAINT payroll_runs_scope_id_uq
        UNIQUE (tenant_id, company_id, id)
);

CREATE INDEX payroll_runs_period_idx
    ON payroll.payroll_runs (tenant_id, company_id, payroll_period_id, run_date, sequence_no);
CREATE INDEX payroll_runs_source_batch_idx
    ON payroll.payroll_runs (source_batch_id)
    WHERE source_batch_id IS NOT NULL;

CREATE TRIGGER payroll_runs_set_updated_at
BEFORE UPDATE ON payroll.payroll_runs
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER payroll_runs_prevent_delete
BEFORE DELETE ON payroll.payroll_runs
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

-- +goose StatementBegin
CREATE FUNCTION payroll.guard_run_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.payroll_period_id IS DISTINCT FROM OLD.payroll_period_id
       OR NEW.run_type IS DISTINCT FROM OLD.run_type
       OR NEW.run_date IS DISTINCT FROM OLD.run_date
       OR NEW.sequence_no IS DISTINCT FROM OLD.sequence_no
       OR NEW.source_batch_id IS DISTINCT FROM OLD.source_batch_id
       OR NEW.correction_of_run_id IS DISTINCT FROM OLD.correction_of_run_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'Payroll run identity is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.status = 'finalized'
       AND (NEW.status IS DISTINCT FROM OLD.status OR NEW.finalized_at IS DISTINCT FROM OLD.finalized_at) THEN
        RAISE EXCEPTION 'Finalized payroll run is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER payroll_runs_guard_update
BEFORE UPDATE ON payroll.payroll_runs
FOR EACH ROW EXECUTE FUNCTION payroll.guard_run_update();

ALTER TABLE payroll.payroll_results
    ADD COLUMN payroll_run_id platform.uuid_v7,
    ADD COLUMN source_employee_number text,
    ADD COLUMN source_sheet_name text,
    ADD COLUMN source_row_no integer;

ALTER TABLE payroll.payroll_results
    DROP CONSTRAINT IF EXISTS payroll_results_employee_period_uq;

ALTER TABLE payroll.payroll_results
    ADD CONSTRAINT payroll_results_run_fk
        FOREIGN KEY (tenant_id, company_id, payroll_run_id)
        REFERENCES payroll.payroll_runs (tenant_id, company_id, id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT payroll_results_source_row_check
        CHECK (source_row_no IS NULL OR source_row_no > 0);

CREATE UNIQUE INDEX payroll_results_run_employee_uq
    ON payroll.payroll_results (payroll_run_id, employee_id)
    WHERE payroll_run_id IS NOT NULL;
CREATE INDEX payroll_results_run_idx
    ON payroll.payroll_results (payroll_run_id, id DESC);

COMMENT ON TABLE payroll.payroll_runs IS
    'A source payroll run within a monthly period. Weekly, THR, bonus, and correction files are separate immutable ledger runs.';
COMMENT ON COLUMN payroll.payroll_results.source_employee_number IS
    'Employee number as supplied by the source file; never used as the employee identity.';

-- Extend the existing result guard so source provenance cannot be rewritten
-- after a result is finalized.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION payroll.guard_result_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    period_status text;
BEGIN
    IF TG_OP = 'INSERT' THEN
        SELECT status INTO period_status FROM payroll.payroll_periods WHERE id = NEW.payroll_period_id;
        IF period_status = 'finalized' THEN
            RAISE EXCEPTION 'Cannot add a result to a finalized payroll period'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
        RETURN NEW;
    END IF;

    SELECT status INTO period_status FROM payroll.payroll_periods WHERE id = OLD.payroll_period_id;
    IF period_status = 'finalized' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'Payroll result belongs to a finalized period'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.payroll_period_id IS DISTINCT FROM OLD.payroll_period_id
       OR NEW.payroll_run_id IS DISTINCT FROM OLD.payroll_run_id
       OR NEW.employee_id IS DISTINCT FROM OLD.employee_id
       OR NEW.employment_id IS DISTINCT FROM OLD.employment_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'Payroll result identity is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.finalized_at IS NOT NULL
       AND (
           NEW.gross_income IS DISTINCT FROM OLD.gross_income
           OR NEW.taxable_income IS DISTINCT FROM OLD.taxable_income
           OR NEW.take_home_pay IS DISTINCT FROM OLD.take_home_pay
           OR NEW.source_employee_number IS DISTINCT FROM OLD.source_employee_number
           OR NEW.source_sheet_name IS DISTINCT FROM OLD.source_sheet_name
           OR NEW.source_row_no IS DISTINCT FROM OLD.source_row_no
           OR NEW.finalized_at IS DISTINCT FROM OLD.finalized_at
       ) THEN
        RAISE EXCEPTION 'Finalized payroll result is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

DROP TRIGGER IF EXISTS payroll_runs_guard_update ON payroll.payroll_runs;
DROP FUNCTION IF EXISTS payroll.guard_run_update();
DROP INDEX IF EXISTS payroll_results_run_idx;
DROP INDEX IF EXISTS payroll_results_run_employee_uq;
ALTER TABLE payroll.payroll_results
    DROP CONSTRAINT IF EXISTS payroll_results_source_row_check,
    DROP CONSTRAINT IF EXISTS payroll_results_run_fk,
    DROP COLUMN IF EXISTS source_row_no,
    DROP COLUMN IF EXISTS source_sheet_name,
    DROP COLUMN IF EXISTS source_employee_number,
    DROP COLUMN IF EXISTS payroll_run_id;
ALTER TABLE payroll.payroll_results
    ADD CONSTRAINT payroll_results_employee_period_uq UNIQUE (payroll_period_id, employee_id);
DROP TABLE IF EXISTS payroll.payroll_runs;

-- Restore the result guard from migration 000006 after the provenance columns
-- have been removed. The existing result trigger continues to reference this
-- function, so the function must be replaced rather than dropped.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION payroll.guard_result_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    period_status text;
BEGIN
    IF TG_OP = 'INSERT' THEN
        SELECT status INTO period_status
        FROM payroll.payroll_periods
        WHERE id = NEW.payroll_period_id;
        IF period_status = 'finalized' THEN
            RAISE EXCEPTION 'Cannot add a result to a finalized payroll period'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
        RETURN NEW;
    END IF;

    SELECT status INTO period_status
    FROM payroll.payroll_periods
    WHERE id = OLD.payroll_period_id;

    IF period_status = 'finalized' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'Payroll result belongs to a finalized period'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.payroll_period_id IS DISTINCT FROM OLD.payroll_period_id
       OR NEW.employee_id IS DISTINCT FROM OLD.employee_id
       OR NEW.employment_id IS DISTINCT FROM OLD.employment_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'Payroll result identity is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.finalized_at IS NOT NULL
       AND (
           NEW.gross_income IS DISTINCT FROM OLD.gross_income
           OR NEW.taxable_income IS DISTINCT FROM OLD.taxable_income
           OR NEW.take_home_pay IS DISTINCT FROM OLD.take_home_pay
           OR NEW.finalized_at IS DISTINCT FROM OLD.finalized_at
       ) THEN
        RAISE EXCEPTION 'Finalized payroll result is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
