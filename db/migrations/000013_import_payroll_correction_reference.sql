-- Payroll correction/reversal source reference for import context, goose 000013.

-- +goose Up

ALTER TABLE file.import_batch_payroll_context
    ADD COLUMN IF NOT EXISTS correction_of_run_id platform.uuid_v7;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'import_batch_payroll_context_correction_fk'
          AND conrelid = 'file.import_batch_payroll_context'::regclass
    ) THEN
        ALTER TABLE file.import_batch_payroll_context
            ADD CONSTRAINT import_batch_payroll_context_correction_fk
            FOREIGN KEY (correction_of_run_id) REFERENCES payroll.payroll_runs (id) ON DELETE RESTRICT;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down

ALTER TABLE file.import_batch_payroll_context
    DROP CONSTRAINT IF EXISTS import_batch_payroll_context_correction_fk,
    DROP COLUMN IF EXISTS correction_of_run_id;
