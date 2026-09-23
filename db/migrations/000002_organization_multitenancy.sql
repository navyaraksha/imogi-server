-- Imogi organization and multi-tenant boundaries, migration 000002.
--
-- This migration intentionally assumes a fresh database. Migration 000001
-- created employee records without a tenant/company owner, so silently
-- backfilling those rows would create an unsafe authorization boundary.

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM employee.employees)
       OR EXISTS (SELECT 1 FROM employee.employments)
       OR EXISTS (SELECT 1 FROM employee.employee_assignments)
       OR EXISTS (SELECT 1 FROM tax.employee_tax_profiles) THEN
        RAISE EXCEPTION '000002 requires a fresh database; employee history must be explicitly migrated before enabling multi-tenancy';
    END IF;
END;
$$;
-- +goose StatementEnd

CREATE SCHEMA IF NOT EXISTS organization;

CREATE TABLE platform.tenants (
    id          platform.uuid_v7 PRIMARY KEY,
    slug        text NOT NULL,
    name        text NOT NULL,
    status      text NOT NULL DEFAULT 'provisioned',
    created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at  timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT tenants_slug_check
        CHECK (slug = lower(slug) AND slug ~ '^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$' AND char_length(slug) BETWEEN 3 AND 63),
    CONSTRAINT tenants_name_check
        CHECK (btrim(name) <> '' AND char_length(name) <= 200),
    CONSTRAINT tenants_status_check
        CHECK (status IN ('provisioned', 'active', 'suspended', 'archived'))
);

CREATE UNIQUE INDEX tenants_slug_uq ON platform.tenants (slug);
CREATE INDEX tenants_status_idx ON platform.tenants (status, created_at DESC);

CREATE TRIGGER tenants_set_updated_at
BEFORE UPDATE ON platform.tenants
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER tenants_prevent_delete
BEFORE DELETE ON platform.tenants
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

-- Tenant ownership and lifecycle identity are immutable. Mutations happen via
-- explicit application commands, not generic updates.
-- +goose StatementBegin
CREATE FUNCTION platform.guard_tenant_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.slug IS DISTINCT FROM OLD.slug THEN
        RAISE EXCEPTION 'Tenant slug is immutable' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.status = 'archived' AND NEW.status <> OLD.status THEN
        RAISE EXCEPTION 'Archived tenant is immutable' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tenants_guard_update
BEFORE UPDATE ON platform.tenants
FOR EACH ROW EXECUTE FUNCTION platform.guard_tenant_update();

CREATE TABLE organization.companies (
    id           platform.uuid_v7 PRIMARY KEY,
    tenant_id    platform.uuid_v7 NOT NULL,
    code         text NOT NULL,
    legal_name   text NOT NULL,
    display_name text NOT NULL,
    status       text NOT NULL DEFAULT 'provisioned',
    created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at   timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT companies_tenant_fk
        FOREIGN KEY (tenant_id) REFERENCES platform.tenants (id) ON DELETE RESTRICT,
    CONSTRAINT companies_code_check
        CHECK (btrim(code) <> '' AND char_length(code) <= 50),
    CONSTRAINT companies_legal_name_check
        CHECK (btrim(legal_name) <> '' AND char_length(legal_name) <= 300),
    CONSTRAINT companies_display_name_check
        CHECK (btrim(display_name) <> '' AND char_length(display_name) <= 200),
    CONSTRAINT companies_status_check
        CHECK (status IN ('provisioned', 'active', 'suspended', 'archived')),
    CONSTRAINT companies_tenant_id_uq UNIQUE (tenant_id, id)
);

CREATE UNIQUE INDEX companies_tenant_code_uq ON organization.companies (tenant_id, code);
CREATE INDEX companies_tenant_status_idx ON organization.companies (tenant_id, status, created_at DESC);

CREATE TRIGGER companies_set_updated_at
BEFORE UPDATE ON organization.companies
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER companies_prevent_delete
BEFORE DELETE ON organization.companies
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

-- +goose StatementBegin
CREATE FUNCTION organization.guard_company_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'Company tenant ownership is immutable' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.status = 'archived' AND NEW.status <> OLD.status THEN
        RAISE EXCEPTION 'Archived company is immutable' USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER companies_guard_update
BEFORE UPDATE ON organization.companies
FOR EACH ROW EXECUTE FUNCTION organization.guard_company_update();

CREATE TABLE organization.units (
    id          platform.uuid_v7 PRIMARY KEY,
    tenant_id   platform.uuid_v7 NOT NULL,
    company_id  platform.uuid_v7 NOT NULL,
    unit_type   text NOT NULL,
    code        text NOT NULL,
    name        text NOT NULL,
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at  timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT units_company_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT units_type_check
        CHECK (unit_type IN ('location', 'department', 'position', 'group')),
    CONSTRAINT units_code_check
        CHECK (btrim(code) <> '' AND char_length(code) <= 50),
    CONSTRAINT units_name_check
        CHECK (btrim(name) <> '' AND char_length(name) <= 200),
    CONSTRAINT units_status_check
        CHECK (status IN ('active', 'archived')),
    CONSTRAINT units_tenant_company_id_uq UNIQUE (tenant_id, company_id, id)
);

CREATE UNIQUE INDEX units_company_type_code_uq
    ON organization.units (tenant_id, company_id, unit_type, code);
CREATE INDEX units_company_type_status_idx
    ON organization.units (tenant_id, company_id, unit_type, status, created_at DESC);

CREATE TRIGGER units_set_updated_at
BEFORE UPDATE ON organization.units
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER units_prevent_delete
BEFORE DELETE ON organization.units
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

-- +goose StatementBegin
CREATE FUNCTION organization.guard_unit_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.unit_type IS DISTINCT FROM OLD.unit_type THEN
        RAISE EXCEPTION 'Organization unit ownership and type are immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.status = 'archived'
       AND (NEW.status <> OLD.status OR NEW.code IS DISTINCT FROM OLD.code OR NEW.name IS DISTINCT FROM OLD.name) THEN
        RAISE EXCEPTION 'Archived organization unit is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER units_guard_update
BEFORE UPDATE ON organization.units
FOR EACH ROW EXECUTE FUNCTION organization.guard_unit_update();

ALTER TABLE employee.employees
    ADD COLUMN tenant_id platform.uuid_v7,
    ADD COLUMN company_id platform.uuid_v7;

ALTER TABLE employee.employees
    ADD CONSTRAINT employees_tenant_company_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT employees_tenant_company_id_uq UNIQUE (tenant_id, id, company_id),
    ALTER COLUMN tenant_id SET NOT NULL,
    ALTER COLUMN company_id SET NOT NULL;

DROP INDEX employee.employees_employee_number_uq;
DROP INDEX employee.employees_nik_lookup_hash_uq;

CREATE UNIQUE INDEX employees_company_employee_number_uq
    ON employee.employees (tenant_id, company_id, employee_number);
CREATE UNIQUE INDEX employees_company_nik_lookup_hash_uq
    ON employee.employees (tenant_id, company_id, nik_lookup_hash);
CREATE INDEX employees_company_name_idx
    ON employee.employees (tenant_id, company_id, full_name, id DESC);

COMMENT ON COLUMN employee.employees.tenant_id IS
    'SaaS isolation owner. It is immutable after creation.';
COMMENT ON COLUMN employee.employees.company_id IS
    'Legal/payroll company owner. It is immutable after creation and scopes employee number and NIK uniqueness.';
COMMENT ON COLUMN employee.employees.employee_number IS
    'Company-scoped organization employee number. It is immutable after creation.';

-- Extend the guards from migration 000001 to cover the new ownership columns.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION employee.guard_employee_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.employee_number IS DISTINCT FROM OLD.employee_number
       OR NEW.nik_lookup_hash IS DISTINCT FROM OLD.nik_lookup_hash
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id THEN
        RAISE EXCEPTION 'Employee identity and ownership are immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE employee.employments
    ADD COLUMN tenant_id platform.uuid_v7;

ALTER TABLE employee.employments
    ADD CONSTRAINT employments_employee_company_fk
        FOREIGN KEY (tenant_id, employee_id, company_id)
        REFERENCES employee.employees (tenant_id, id, company_id) ON DELETE RESTRICT;

ALTER TABLE employee.employments
    ALTER COLUMN tenant_id SET NOT NULL;

CREATE INDEX employments_tenant_company_history_idx
    ON employee.employments (tenant_id, company_id, join_date DESC);
COMMENT ON COLUMN employee.employments.tenant_id IS
    'SaaS tenant owner copied from the employee company and immutable.';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION employee.guard_employment_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.employee_id IS DISTINCT FROM OLD.employee_id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.employment_type IS DISTINCT FROM OLD.employment_type
       OR NEW.join_date IS DISTINCT FROM OLD.join_date THEN
        RAISE EXCEPTION 'Employment identity, ownership, and start data are immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.end_date IS NOT NULL
       AND (NEW.end_date IS DISTINCT FROM OLD.end_date OR NEW.termination_reason IS DISTINCT FROM OLD.termination_reason) THEN
        RAISE EXCEPTION 'Ended employment history is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE tax.employee_tax_profiles
    ADD COLUMN tenant_id platform.uuid_v7,
    ADD COLUMN company_id platform.uuid_v7;

ALTER TABLE tax.employee_tax_profiles
    ADD CONSTRAINT employee_tax_profiles_employee_company_fk
        FOREIGN KEY (tenant_id, employee_id, company_id)
        REFERENCES employee.employees (tenant_id, id, company_id) ON DELETE RESTRICT,
    ALTER COLUMN tenant_id SET NOT NULL,
    ALTER COLUMN company_id SET NOT NULL;

CREATE INDEX employee_tax_profiles_tenant_company_history_idx
    ON tax.employee_tax_profiles (tenant_id, company_id, effective_from DESC);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tax.guard_tax_profile_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.employee_id IS DISTINCT FROM OLD.employee_id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.nik_ciphertext IS DISTINCT FROM OLD.nik_ciphertext
       OR NEW.npwp_ciphertext IS DISTINCT FROM OLD.npwp_ciphertext
       OR NEW.ptkp_code IS DISTINCT FROM OLD.ptkp_code
       OR NEW.tax_method IS DISTINCT FROM OLD.tax_method
       OR NEW.effective_from IS DISTINCT FROM OLD.effective_from THEN
        RAISE EXCEPTION 'Tax profile ownership and history are immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to THEN
        RAISE EXCEPTION 'Closed tax profile history is immutable'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Assignment unit references are globally opaque UUIDs, but this trigger also
-- verifies that every referenced unit belongs to the employment's tenant and
-- company and has the expected typed unit kind.
-- +goose StatementBegin
CREATE FUNCTION employee.validate_assignment_units()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    employment_tenant_id platform.uuid_v7;
    employment_company_id platform.uuid_v7;
BEGIN
    SELECT e.tenant_id, e.company_id
    INTO employment_tenant_id, employment_company_id
    FROM employee.employments e
    WHERE e.id = NEW.employment_id;

    IF NEW.location_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM organization.units u
        WHERE u.id = NEW.location_id AND u.tenant_id = employment_tenant_id
          AND u.company_id = employment_company_id AND u.unit_type = 'location'
    ) THEN
        RAISE EXCEPTION 'Assignment location does not belong to employment company'
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF NEW.department_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM organization.units u
        WHERE u.id = NEW.department_id AND u.tenant_id = employment_tenant_id
          AND u.company_id = employment_company_id AND u.unit_type = 'department'
    ) THEN
        RAISE EXCEPTION 'Assignment department does not belong to employment company'
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF NEW.position_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM organization.units u
        WHERE u.id = NEW.position_id AND u.tenant_id = employment_tenant_id
          AND u.company_id = employment_company_id AND u.unit_type = 'position'
    ) THEN
        RAISE EXCEPTION 'Assignment position does not belong to employment company'
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF NEW.group_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM organization.units u
        WHERE u.id = NEW.group_id AND u.tenant_id = employment_tenant_id
          AND u.company_id = employment_company_id AND u.unit_type = 'group'
    ) THEN
        RAISE EXCEPTION 'Assignment group does not belong to employment company'
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER employee_assignments_units_match_employment
AFTER INSERT OR UPDATE ON employee.employee_assignments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION employee.validate_assignment_units();

-- +goose Down

DROP TRIGGER IF EXISTS employee_assignments_units_match_employment ON employee.employee_assignments;
DROP FUNCTION IF EXISTS employee.validate_assignment_units();

DROP INDEX IF EXISTS tax.employee_tax_profiles_tenant_company_history_idx;
ALTER TABLE tax.employee_tax_profiles
    DROP CONSTRAINT IF EXISTS employee_tax_profiles_employee_company_fk,
    DROP COLUMN IF EXISTS company_id,
    DROP COLUMN IF EXISTS tenant_id;

DROP INDEX IF EXISTS employee.employments_tenant_company_history_idx;
ALTER TABLE employee.employments
    DROP CONSTRAINT IF EXISTS employments_employee_company_fk,
    DROP COLUMN IF EXISTS tenant_id;

DROP INDEX IF EXISTS employee.employees_company_name_idx;
DROP INDEX IF EXISTS employee.employees_company_employee_number_uq;
DROP INDEX IF EXISTS employee.employees_company_nik_lookup_hash_uq;
ALTER TABLE employee.employees
    DROP CONSTRAINT IF EXISTS employees_tenant_company_fk,
    DROP COLUMN IF EXISTS company_id,
    DROP COLUMN IF EXISTS tenant_id;
CREATE UNIQUE INDEX employee.employees_employee_number_uq ON employee.employees (employee_number);
CREATE UNIQUE INDEX employee.employees_nik_lookup_hash_uq ON employee.employees (nik_lookup_hash);

DROP TABLE IF EXISTS organization.units;
DROP TABLE IF EXISTS organization.companies;
DROP TABLE IF EXISTS platform.tenants;
DROP SCHEMA IF EXISTS organization;
DROP FUNCTION IF EXISTS organization.guard_unit_update();
DROP FUNCTION IF EXISTS organization.guard_company_update();
DROP FUNCTION IF EXISTS platform.guard_tenant_update();
