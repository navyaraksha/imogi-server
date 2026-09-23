-- Imogi payroll ledger, migration 000006.
--
-- V0 stores payroll results imported from an external calculation process. It
-- deliberately does not implement a salary calculation engine.

-- +goose Up

CREATE SCHEMA IF NOT EXISTS payroll;

-- The composite key lets payroll results prove that their employment belongs
-- to the same employee and company recorded on the result.
ALTER TABLE employee.employments
    ADD CONSTRAINT employments_id_employee_company_uq
    UNIQUE (id, employee_id, company_id);

CREATE TABLE payroll.payroll_periods (
    id              platform.uuid_v7 PRIMARY KEY,
    tenant_id       platform.uuid_v7 NOT NULL,
    company_id      platform.uuid_v7 NOT NULL,
    year            integer NOT NULL,
    month           integer NOT NULL,
    status          text NOT NULL DEFAULT 'open',
    opened_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    finalized_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at      timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT payroll_periods_company_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT payroll_periods_tenant_company_id_uq
        UNIQUE (tenant_id, company_id, id),
    CONSTRAINT payroll_periods_year_check
        CHECK (year BETWEEN 2000 AND 9999),
    CONSTRAINT payroll_periods_month_check
        CHECK (month BETWEEN 1 AND 12),
    CONSTRAINT payroll_periods_status_check
        CHECK (status IN ('open', 'finalized')),
    CONSTRAINT payroll_periods_finalization_state_check
        CHECK (
            (status = 'open' AND finalized_at IS NULL)
            OR
            (status = 'finalized' AND finalized_at IS NOT NULL)
        )
);

COMMENT ON TABLE payroll.payroll_periods IS
    'Company-scoped monthly payroll ledger period. Finalized periods are immutable.';
COMMENT ON COLUMN payroll.payroll_periods.month IS
    'Calendar month from 1 through 12.';

CREATE UNIQUE INDEX payroll_periods_company_month_uq
    ON payroll.payroll_periods (tenant_id, company_id, year, month);
CREATE INDEX payroll_periods_company_status_idx
    ON payroll.payroll_periods (tenant_id, company_id, status, id DESC);

-- A period may only move once from open to finalized. Its business identity
-- and timestamps cannot be rewritten after creation.
-- +goose StatementBegin
CREATE FUNCTION payroll.guard_period_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.year IS DISTINCT FROM OLD.year
       OR NEW.month IS DISTINCT FROM OLD.month
       OR NEW.opened_at IS DISTINCT FROM OLD.opened_at THEN
        RAISE EXCEPTION 'Payroll period identity is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF OLD.status = 'finalized'
       AND (
           NEW.status IS DISTINCT FROM OLD.status
           OR NEW.finalized_at IS DISTINCT FROM OLD.finalized_at
       ) THEN
        RAISE EXCEPTION 'Finalized payroll period is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF OLD.status = 'open' AND NEW.status NOT IN ('open', 'finalized') THEN
        RAISE EXCEPTION 'Invalid payroll period transition'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER payroll_periods_guard_update
BEFORE UPDATE ON payroll.payroll_periods
FOR EACH ROW EXECUTE FUNCTION payroll.guard_period_update();

CREATE TRIGGER payroll_periods_set_updated_at
BEFORE UPDATE ON payroll.payroll_periods
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER payroll_periods_prevent_delete
BEFORE DELETE ON payroll.payroll_periods
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

CREATE TABLE payroll.payroll_results (
    id                  platform.uuid_v7 PRIMARY KEY,
    tenant_id           platform.uuid_v7 NOT NULL,
    company_id          platform.uuid_v7 NOT NULL,
    payroll_period_id   platform.uuid_v7 NOT NULL,
    employee_id         platform.uuid_v7 NOT NULL,
    employment_id       platform.uuid_v7 NOT NULL,
    gross_income        bigint NOT NULL,
    taxable_income      bigint NOT NULL,
    take_home_pay      bigint NOT NULL,
    finalized_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT payroll_results_period_fk
        FOREIGN KEY (tenant_id, company_id, payroll_period_id)
        REFERENCES payroll.payroll_periods (tenant_id, company_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT payroll_results_employee_fk
        FOREIGN KEY (tenant_id, employee_id, company_id)
        REFERENCES employee.employees (tenant_id, id, company_id)
        ON DELETE RESTRICT,
    CONSTRAINT payroll_results_employment_fk
        FOREIGN KEY (employment_id, employee_id, company_id)
        REFERENCES employee.employments (id, employee_id, company_id)
        ON DELETE RESTRICT,
    CONSTRAINT payroll_results_employee_period_uq
        UNIQUE (payroll_period_id, employee_id),
    CONSTRAINT payroll_results_gross_income_check
        CHECK (gross_income >= 0),
    CONSTRAINT payroll_results_taxable_income_check
        CHECK (taxable_income >= 0),
    CONSTRAINT payroll_results_take_home_pay_check
        CHECK (take_home_pay >= 0)
);

COMMENT ON TABLE payroll.payroll_results IS
    'Historical payroll result for one employee in one payroll period.';
COMMENT ON COLUMN payroll.payroll_results.finalized_at IS
    'Set when the containing payroll period is finalized; finalized results cannot be edited.';

CREATE INDEX payroll_results_employee_history_idx
    ON payroll.payroll_results (tenant_id, company_id, employee_id, id DESC);
CREATE INDEX payroll_results_employment_history_idx
    ON payroll.payroll_results (employment_id, id DESC);
CREATE INDEX payroll_results_period_idx
    ON payroll.payroll_results (payroll_period_id, id DESC);

-- Results may be finalized as part of period finalization, but no identity or
-- financial value can be changed after finalization or after the period closes.
-- +goose StatementBegin
CREATE FUNCTION payroll.guard_result_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    period_status text;
BEGIN
    IF TG_OP = 'INSERT' THEN
        SELECT status
          INTO period_status
          FROM payroll.payroll_periods
         WHERE id = NEW.payroll_period_id;

        IF period_status = 'finalized' THEN
            RAISE EXCEPTION 'Cannot add a result to a finalized payroll period'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
        RETURN NEW;
    END IF;

    SELECT status
      INTO period_status
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

CREATE TRIGGER payroll_results_guard_write
BEFORE INSERT OR UPDATE ON payroll.payroll_results
FOR EACH ROW EXECUTE FUNCTION payroll.guard_result_write();

CREATE TRIGGER payroll_results_set_updated_at
BEFORE UPDATE ON payroll.payroll_results
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER payroll_results_prevent_delete
BEFORE DELETE ON payroll.payroll_results
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

CREATE TABLE payroll.payroll_result_items (
    id                  platform.uuid_v7 PRIMARY KEY,
    payroll_result_id   platform.uuid_v7 NOT NULL,
    component_code      text NOT NULL,
    component_type      text NOT NULL,
    amount              bigint NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT payroll_result_items_result_fk
        FOREIGN KEY (payroll_result_id)
        REFERENCES payroll.payroll_results (id)
        ON DELETE RESTRICT,
    CONSTRAINT payroll_result_items_code_check
        CHECK (btrim(component_code) <> '' AND char_length(component_code) <= 100),
    CONSTRAINT payroll_result_items_type_check
        CHECK (component_type IN ('earning', 'deduction', 'benefit', 'tax')),
    CONSTRAINT payroll_result_items_amount_check
        CHECK (amount >= 0),
    CONSTRAINT payroll_result_items_result_code_uq
        UNIQUE (payroll_result_id, component_code)
);

COMMENT ON TABLE payroll.payroll_result_items IS
    'Normalized payroll component amounts. Component codes are stable import/domain codes.';

CREATE INDEX payroll_result_items_result_idx
    ON payroll.payroll_result_items (payroll_result_id, component_code);

-- Items cannot be added, changed, or removed after their result or period is
-- finalized. Hard deletion is also disabled by the standard platform trigger.
-- +goose StatementBegin
CREATE FUNCTION payroll.guard_result_item_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    result_id platform.uuid_v7;
    result_finalized_at timestamptz;
    period_status text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        result_id := OLD.payroll_result_id;
    ELSE
        result_id := NEW.payroll_result_id;
    END IF;

    SELECT pr.finalized_at, pp.status
      INTO result_finalized_at, period_status
      FROM payroll.payroll_results pr
      JOIN payroll.payroll_periods pp ON pp.id = pr.payroll_period_id
     WHERE pr.id = result_id;

    IF result_finalized_at IS NOT NULL OR period_status = 'finalized' THEN
        RAISE EXCEPTION 'Payroll result items are immutable after finalization'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF TG_OP = 'UPDATE' AND NEW.payroll_result_id IS DISTINCT FROM OLD.payroll_result_id THEN
        RAISE EXCEPTION 'Payroll result item ownership is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER payroll_result_items_guard_write
BEFORE INSERT OR UPDATE OR DELETE ON payroll.payroll_result_items
FOR EACH ROW EXECUTE FUNCTION payroll.guard_result_item_write();

CREATE TRIGGER payroll_result_items_set_updated_at
BEFORE UPDATE ON payroll.payroll_result_items
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER payroll_result_items_prevent_delete
BEFORE DELETE ON payroll.payroll_result_items
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

-- +goose Down

DROP TABLE IF EXISTS payroll.payroll_result_items;
DROP TABLE IF EXISTS payroll.payroll_results;
DROP TABLE IF EXISTS payroll.payroll_periods;
DROP FUNCTION IF EXISTS payroll.guard_result_item_write();
DROP FUNCTION IF EXISTS payroll.guard_result_write();
DROP FUNCTION IF EXISTS payroll.guard_period_update();
DROP SCHEMA IF EXISTS payroll;

ALTER TABLE employee.employments
    DROP CONSTRAINT IF EXISTS employments_id_employee_company_uq;
