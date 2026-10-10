CREATE TABLE IF NOT EXISTS schema_version(version int PRIMARY KEY);
INSERT INTO schema_version VALUES (1) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS users (
 id uuid PRIMARY KEY, email text NOT NULL UNIQUE, name text NOT NULL,
 password_hash text NOT NULL, admin boolean NOT NULL DEFAULT false,
 blocked boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS session_families (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
 revoked boolean NOT NULL DEFAULT false, expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS refresh_tokens (
 hash text PRIMARY KEY, family_id uuid NOT NULL REFERENCES session_families ON DELETE CASCADE,
 used boolean NOT NULL DEFAULT false, expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
 name text NOT NULL, public_key text NOT NULL UNIQUE, blocked boolean NOT NULL DEFAULT false,
 last_seen timestamptz NOT NULL DEFAULT now(), version text NOT NULL DEFAULT '0.1.0',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS devices_user ON devices(user_id);
CREATE SEQUENCE IF NOT EXISTS subnet_sequence MINVALUE 0 START 0 MAXVALUE 57343 NO CYCLE;
CREATE TABLE IF NOT EXISTS networks (
 id uuid PRIMARY KEY, owner_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
 name text NOT NULL, description text NOT NULL DEFAULT '', subnet cidr NOT NULL UNIQUE,
 mode text NOT NULL CHECK(mode IN ('private','password','invite','approval')),
 password_hash text NOT NULL DEFAULT '', max_members int NOT NULL CHECK(max_members BETWEEN 2 AND 253),
 blocked boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS networks_name ON networks(lower(name));
CREATE TABLE IF NOT EXISTS network_members (
 network_id uuid NOT NULL REFERENCES networks ON DELETE CASCADE,
 device_id uuid NOT NULL REFERENCES devices ON DELETE CASCADE,
 virtual_ip inet, status text NOT NULL CHECK(status IN ('approved','pending','blocked')),
 joined_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(network_id,device_id),
 UNIQUE(network_id,virtual_ip), CHECK((status='approved')=(virtual_ip IS NOT NULL))
);
CREATE TABLE IF NOT EXISTS invites (
 hash text PRIMARY KEY, network_id uuid NOT NULL REFERENCES networks ON DELETE CASCADE,
 expires_at timestamptz NOT NULL, remaining int NOT NULL CHECK(remaining>=0)
);
CREATE TABLE IF NOT EXISTS password_resets (
 hash text PRIMARY KEY,user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
 expires_at timestamptz NOT NULL, used boolean NOT NULL DEFAULT false
);
CREATE TABLE IF NOT EXISTS audit_log (
 id bigserial PRIMARY KEY, user_id uuid, action text NOT NULL,
 object_id text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS members_device ON network_members(device_id);
CREATE INDEX IF NOT EXISTS audit_created ON audit_log(created_at DESC);
ALTER TABLE users ADD COLUMN IF NOT EXISTS device_only boolean NOT NULL DEFAULT false;
INSERT INTO schema_version VALUES (2) ON CONFLICT DO NOTHING;
