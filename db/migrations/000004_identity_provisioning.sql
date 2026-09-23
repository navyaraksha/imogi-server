-- Imogi identity provisioning, migration 000004.
--
-- Platform users may be pre-registered by email before their first Google
-- login. The Google subject is linked only after Google returns a verified
-- email claim.

-- +goose Up

ALTER TABLE platform.users
    ALTER COLUMN google_subject DROP NOT NULL;

ALTER TABLE platform.users
    DROP CONSTRAINT users_status_check,
    ADD CONSTRAINT users_status_check
        CHECK (status IN ('pending', 'active', 'blocked'));

-- +goose Down

ALTER TABLE platform.users
    DROP CONSTRAINT users_status_check,
    ADD CONSTRAINT users_status_check
        CHECK (status IN ('active', 'blocked'));

ALTER TABLE platform.users
    ALTER COLUMN google_subject SET NOT NULL;
