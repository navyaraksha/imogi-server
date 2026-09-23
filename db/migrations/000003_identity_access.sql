-- Imogi identity and access control, migration 000003.
--
-- Google authenticates the person. These tables decide which Imogi tenants,
-- companies, roles, and capabilities that person may access.

-- +goose Up

CREATE TABLE platform.users (
    id              platform.uuid_v7 PRIMARY KEY,
    google_subject  text NOT NULL,
    email           text NOT NULL,
    display_name    text NOT NULL,
    status          text NOT NULL DEFAULT 'active',
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at      timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT users_google_subject_check
        CHECK (btrim(google_subject) <> '' AND char_length(google_subject) <= 255),
    CONSTRAINT users_email_check
        CHECK (btrim(email) <> '' AND char_length(email) <= 320),
    CONSTRAINT users_display_name_check
        CHECK (btrim(display_name) <> '' AND char_length(display_name) <= 200),
    CONSTRAINT users_status_check
        CHECK (status IN ('active', 'blocked'))
);

CREATE UNIQUE INDEX users_google_subject_uq
    ON platform.users (google_subject);
CREATE UNIQUE INDEX users_email_lower_uq
    ON platform.users (lower(email));
CREATE INDEX users_status_idx
    ON platform.users (status, created_at DESC);

CREATE TRIGGER users_set_updated_at
BEFORE UPDATE ON platform.users
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER users_prevent_delete
BEFORE DELETE ON platform.users
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

CREATE TABLE platform.platform_admins (
    user_id     platform.uuid_v7 PRIMARY KEY,
    created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT platform_admins_user_fk
        FOREIGN KEY (user_id) REFERENCES platform.users (id) ON DELETE RESTRICT
);

CREATE TABLE platform.tenant_memberships (
    id          platform.uuid_v7 PRIMARY KEY,
    user_id     platform.uuid_v7 NOT NULL,
    tenant_id   platform.uuid_v7 NOT NULL,
    role_code   text NOT NULL,
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at  timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT tenant_memberships_user_fk
        FOREIGN KEY (user_id) REFERENCES platform.users (id) ON DELETE RESTRICT,
    CONSTRAINT tenant_memberships_tenant_fk
        FOREIGN KEY (tenant_id) REFERENCES platform.tenants (id) ON DELETE RESTRICT,
    CONSTRAINT tenant_memberships_role_check
        CHECK (role_code IN ('tenant_admin', 'hr_admin', 'tax_admin', 'payroll_admin')),
    CONSTRAINT tenant_memberships_status_check
        CHECK (status IN ('active', 'suspended', 'revoked')),
    CONSTRAINT tenant_memberships_tenant_id_uq
        UNIQUE (id, tenant_id),
    CONSTRAINT tenant_memberships_user_tenant_uq
        UNIQUE (user_id, tenant_id)
);

CREATE INDEX tenant_memberships_tenant_idx
    ON platform.tenant_memberships (tenant_id, status, created_at DESC);
CREATE INDEX tenant_memberships_user_idx
    ON platform.tenant_memberships (user_id, status, created_at DESC);

CREATE TRIGGER tenant_memberships_set_updated_at
BEFORE UPDATE ON platform.tenant_memberships
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER tenant_memberships_prevent_delete
BEFORE DELETE ON platform.tenant_memberships
FOR EACH ROW EXECUTE FUNCTION platform.prevent_delete();

CREATE TABLE platform.membership_company_access (
    membership_id  platform.uuid_v7 NOT NULL,
    tenant_id      platform.uuid_v7 NOT NULL,
    company_id     platform.uuid_v7 NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT clock_timestamp(),

    CONSTRAINT membership_company_access_pk
        PRIMARY KEY (membership_id, company_id),
    CONSTRAINT membership_company_access_membership_fk
        FOREIGN KEY (membership_id, tenant_id)
        REFERENCES platform.tenant_memberships (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT membership_company_access_company_fk
        FOREIGN KEY (tenant_id, company_id)
        REFERENCES organization.companies (tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX membership_company_access_company_idx
    ON platform.membership_company_access (tenant_id, company_id, membership_id);

-- +goose Down

DROP TABLE IF EXISTS platform.membership_company_access;
DROP TABLE IF EXISTS platform.tenant_memberships;
DROP TABLE IF EXISTS platform.platform_admins;
DROP TABLE IF EXISTS platform.users;
