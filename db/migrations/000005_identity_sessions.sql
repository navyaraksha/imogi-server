-- Imogi server-side authentication sessions, migration 000005.

-- +goose Up

CREATE TABLE platform.sessions (
    id          platform.uuid_v7 PRIMARY KEY,
    user_id     platform.uuid_v7 NOT NULL,
    token_hash  bytea NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at  timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    revoked_at  timestamptz,

    CONSTRAINT sessions_user_fk
        FOREIGN KEY (user_id) REFERENCES platform.users (id) ON DELETE RESTRICT,
    CONSTRAINT sessions_token_hash_check
        CHECK (octet_length(token_hash) = 32),
    CONSTRAINT sessions_expiry_check
        CHECK (expires_at > created_at),
    CONSTRAINT sessions_last_seen_check
        CHECK (last_seen_at >= created_at),
    CONSTRAINT sessions_token_hash_uq
        UNIQUE (token_hash)
);

CREATE INDEX sessions_user_active_idx
    ON platform.sessions (user_id, expires_at DESC)
    WHERE revoked_at IS NULL;

CREATE INDEX sessions_expiry_idx
    ON platform.sessions (expires_at)
    WHERE revoked_at IS NULL;

CREATE TRIGGER sessions_prevent_delete
BEFORE DELETE ON platform.sessions
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

-- +goose Down

DROP TABLE IF EXISTS platform.sessions;
