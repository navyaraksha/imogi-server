-- Imogi employee number history, migration 000008.
--
-- The employee root keeps a nullable current projection for compatibility.
-- The effective-dated ledger below is the source of truth for payroll and
-- historical identity reconstruction.

-- +goose Up

ALTER TABLE employee.employees
    ALTER COLUMN employee_number DROP NOT NULL;

ALTER TABLE employee.employees
    DROP CONSTRAINT IF EXISTS employees_employee_number_not_blank;

DROP INDEX IF EXISTS employee.employees_company_employee_number_uq;

ALTER TABLE employee.employees
    ADD CONSTRAINT employees_employee_number_check
    CHECK (
        employee_number IS NULL
        OR (btrim(employee_number) <> '' AND char_length(employee_number) <= 100)
    );

ALTER TABLE employee.employees
    ADD COLUMN IF NOT EXISTS employee_number_status text NOT NULL DEFAULT 'unassigned';

UPDATE employee.employees
SET employee_number_status = CASE
    WHEN employee_number IS NULL OR btrim(employee_number) = '' THEN 'unassigned'
    WHEN upper(employee_number) LIKE 'NF%' THEN 'temporary'
    ELSE 'permanent'
END;

ALTER TABLE employee.employees
    ADD CONSTRAINT employees_employee_number_status_check
    CHECK (employee_number_status IN ('unassigned', 'temporary', 'permanent'));

CREATE INDEX employees_company_employee_number_idx
    ON employee.employees (tenant_id, company_id, employee_number)
    WHERE employee_number IS NOT NULL;

-- The root employee number is a read projection and may change when the
-- current number ledger changes. NIK and ownership remain immutable.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION employee.guard_employee_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.nik_lookup_hash IS DISTINCT FROM OLD.nik_lookup_hash
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id THEN
        RAISE EXCEPTION 'Employee identity and ownership are immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TABLE employee.employee_number_history (
    id                  platform.uuid_v7 PRIMARY KEY,
    tenant_id           platform.uuid_v7 NOT NULL,
    company_id          platform.uuid_v7 NOT NULL,
    employee_id         platform.uuid_v7 NOT NULL,
    employment_id       platform.uuid_v7,
    employee_number     text NOT NULL,
    number_type         text NOT NULL,
    effective_from      date NOT NULL,
    effective_to        date,
    source              text NOT NULL DEFAULT 'manual',
    source_batch_id     platform.uuid_v7,
    supersedes_id       platform.uuid_v7,
    correction_reason   text,
    created_by          platform.uuid_v7,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT employee_number_history_company_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT employee_number_history_employee_fk
        FOREIGN KEY (tenant_id, employee_id, company_id)
        REFERENCES employee.employees (tenant_id, id, company_id)
        ON DELETE RESTRICT,
    CONSTRAINT employee_number_history_employment_fk
        FOREIGN KEY (employment_id, employee_id, company_id)
        REFERENCES employee.employments (id, employee_id, company_id)
        ON DELETE RESTRICT,
    CONSTRAINT employee_number_history_number_check
        CHECK (btrim(employee_number) <> '' AND char_length(employee_number) <= 100),
    CONSTRAINT employee_number_history_type_check
        CHECK (number_type IN ('temporary', 'permanent')),
    CONSTRAINT employee_number_history_date_check
        CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CONSTRAINT employee_number_history_end_date_check
        CHECK (effective_to IS NULL OR effective_to < DATE '9999-12-31'),
    CONSTRAINT employee_number_history_source_check
        CHECK (source IN ('manual', 'import', 'correction', 'migration')),
    CONSTRAINT employee_number_history_reason_check
        CHECK (correction_reason IS NULL OR (btrim(correction_reason) <> '' AND char_length(correction_reason) <= 500))
);

COMMENT ON TABLE employee.employee_number_history IS
    'Effective-dated employee numbers. This is the source of truth for payroll identity matching; employees.employee_number is only a current projection.';
COMMENT ON COLUMN employee.employee_number_history.effective_to IS
    'Inclusive end date. A number may be reused only after its previous interval ends.';

CREATE INDEX employee_number_history_employee_idx
    ON employee.employee_number_history (tenant_id, company_id, employee_id, effective_from DESC);
CREATE INDEX employee_number_history_employment_idx
    ON employee.employee_number_history (employment_id, effective_from DESC);
CREATE INDEX employee_number_history_lookup_idx
    ON employee.employee_number_history (tenant_id, company_id, employee_number, effective_from DESC);

ALTER TABLE employee.employee_number_history
    ADD CONSTRAINT employee_number_history_no_number_overlap
    EXCLUDE USING gist (
        tenant_id WITH =,
        company_id WITH =,
        employee_number WITH =,
        daterange(
            effective_from,
            CASE
                WHEN effective_to IS NULL THEN 'infinity'::date
                ELSE effective_to + 1
            END,
            '[)'
        ) WITH &&
    );

ALTER TABLE employee.employee_number_history
    ADD CONSTRAINT employee_number_history_no_employee_overlap
    EXCLUDE USING gist (
        employee_id WITH =,
        daterange(
            effective_from,
            CASE
                WHEN effective_to IS NULL THEN 'infinity'::date
                ELSE effective_to + 1
            END,
            '[)'
        ) WITH &&
    );

-- +goose StatementBegin
CREATE FUNCTION employee.guard_employee_number_history_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.employee_id IS DISTINCT FROM OLD.employee_id
       OR NEW.employment_id IS DISTINCT FROM OLD.employment_id
       OR NEW.employee_number IS DISTINCT FROM OLD.employee_number
       OR NEW.number_type IS DISTINCT FROM OLD.number_type
       OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
       OR NEW.source IS DISTINCT FROM OLD.source
       OR NEW.source_batch_id IS DISTINCT FROM OLD.source_batch_id
       OR NEW.supersedes_id IS DISTINCT FROM OLD.supersedes_id
       OR NEW.correction_reason IS DISTINCT FROM OLD.correction_reason
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'Employee number history is immutable; create a correction record instead'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF OLD.effective_to IS NOT NULL
       AND NEW.effective_to IS DISTINCT FROM OLD.effective_to THEN
        RAISE EXCEPTION 'Closed employee number history is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER employee_number_history_guard_update
BEFORE UPDATE ON employee.employee_number_history
FOR EACH ROW EXECUTE FUNCTION employee.guard_employee_number_history_update();

CREATE TRIGGER employee_number_history_prevent_delete
BEFORE DELETE ON employee.employee_number_history
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

-- +goose Down

DROP TABLE IF EXISTS employee.employee_number_history;
DROP FUNCTION IF EXISTS employee.guard_employee_number_history_update();
DROP INDEX IF EXISTS employee.employees_company_employee_number_idx;
ALTER TABLE employee.employees DROP CONSTRAINT IF EXISTS employees_employee_number_status_check;
ALTER TABLE employee.employees DROP COLUMN IF EXISTS employee_number_status;
ALTER TABLE employee.employees DROP CONSTRAINT IF EXISTS employees_employee_number_check;
ALTER TABLE employee.employees ADD CONSTRAINT employees_employee_number_not_blank
    CHECK (btrim(employee_number) <> '' AND char_length(employee_number) <= 100);
ALTER TABLE employee.employees ALTER COLUMN employee_number SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS employee.employees_company_employee_number_uq
    ON employee.employees (tenant_id, company_id, employee_number);
