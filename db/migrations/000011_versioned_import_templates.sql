-- Tenant/company-scoped versioned import template metadata, migration 000011.

-- +goose Up

ALTER TABLE file.batch_artifacts
    DROP CONSTRAINT IF EXISTS batch_artifacts_role_check;
ALTER TABLE file.batch_artifacts
    ADD CONSTRAINT batch_artifacts_role_check
    CHECK (role IN ('input', 'validation_summary', 'validation_errors', 'normalized', 'ready_import', 'import_receipt', 'output_xml', 'error_report'));

ALTER TABLE file.file_objects
    DROP CONSTRAINT IF EXISTS file_objects_extension_check;
ALTER TABLE file.file_objects
    ADD CONSTRAINT file_objects_extension_check
    CHECK (detected_extension IN ('xlsx', 'xls', 'xlsm', 'xlm', 'csv', 'json', 'xml', 'ndjson'));

ALTER TABLE file.import_templates
    DROP CONSTRAINT IF EXISTS import_templates_format_check;
ALTER TABLE file.import_templates
    ADD CONSTRAINT import_templates_format_check
    CHECK (file_format IN ('xlsx', 'xls', 'xlsm', 'xlm', 'csv', 'json'));

ALTER TABLE file.import_templates
    ADD COLUMN tenant_id platform.uuid_v7,
    ADD COLUMN company_id platform.uuid_v7,
    ADD COLUMN configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN effective_from date,
    ADD COLUMN effective_to date;

ALTER TABLE file.import_templates
    ADD CONSTRAINT import_templates_scope_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT import_templates_scope_check
        CHECK ((tenant_id IS NULL AND company_id IS NULL) OR (tenant_id IS NOT NULL AND company_id IS NOT NULL)),
    ADD CONSTRAINT import_templates_configuration_check
        CHECK (jsonb_typeof(configuration) = 'object'),
    ADD CONSTRAINT import_templates_effective_date_check
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to >= effective_from);

-- Migration 000007 called the template mapping payload `schema`. Preserve
-- existing mappings when the versioned configuration field is introduced.
UPDATE file.import_templates
SET configuration = schema
WHERE schema IS NOT NULL
  AND schema <> '{}'::jsonb;

ALTER TABLE file.import_templates
    DROP CONSTRAINT IF EXISTS import_templates_identity_uq;

CREATE UNIQUE INDEX import_templates_identity_uq
    ON file.import_templates (
        COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(company_id, '00000000-0000-0000-0000-000000000000'::uuid),
        template_type,
        version,
        file_format
    );

CREATE INDEX import_templates_scope_status_idx
    ON file.import_templates (tenant_id, company_id, template_type, status, effective_from DESC);

-- +goose Down

ALTER TABLE file.batch_artifacts
    DROP CONSTRAINT IF EXISTS batch_artifacts_role_check;
ALTER TABLE file.batch_artifacts
    ADD CONSTRAINT batch_artifacts_role_check
    CHECK (role IN ('input', 'validation_summary', 'validation_errors', 'normalized', 'import_receipt', 'output_xml', 'error_report'));

ALTER TABLE file.import_templates
    DROP CONSTRAINT IF EXISTS import_templates_format_check;
ALTER TABLE file.import_templates
    ADD CONSTRAINT import_templates_format_check
    CHECK (file_format IN ('xlsx', 'xls', 'xlsm', 'csv', 'json'));
ALTER TABLE file.file_objects
    DROP CONSTRAINT IF EXISTS file_objects_extension_check;
ALTER TABLE file.file_objects
    ADD CONSTRAINT file_objects_extension_check
    CHECK (detected_extension IN ('xlsx', 'xls', 'xlsm', 'csv', 'json', 'xml', 'ndjson'));

DROP INDEX IF EXISTS import_templates_scope_status_idx;
DROP INDEX IF EXISTS import_templates_identity_uq;
ALTER TABLE file.import_templates
    DROP CONSTRAINT IF EXISTS import_templates_effective_date_check,
    DROP CONSTRAINT IF EXISTS import_templates_configuration_check,
    DROP CONSTRAINT IF EXISTS import_templates_scope_check,
    DROP CONSTRAINT IF EXISTS import_templates_scope_fk,
    DROP COLUMN IF EXISTS effective_to,
    DROP COLUMN IF EXISTS effective_from,
    DROP COLUMN IF EXISTS configuration,
    DROP COLUMN IF EXISTS company_id,
    DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE file.import_templates
    ADD CONSTRAINT import_templates_identity_uq
    UNIQUE (template_type, version, file_format);
