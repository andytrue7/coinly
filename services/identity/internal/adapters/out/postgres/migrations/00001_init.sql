-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id            uuid        PRIMARY KEY,
    -- citext makes the unique index case-insensitive as a second line of
    -- defence; the domain already lower-cases on the way in.
    email         citext      NOT NULL UNIQUE,
    password_hash text        NOT NULL,
    status        text        NOT NULL CHECK (status IN ('active', 'suspended')),
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL
);

CREATE TABLE refresh_tokens (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Only the SHA-256 of the secret is stored; the plaintext never
    -- touches the database.
    token_hash text        NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);

-- RevokeAllForUser and per-user session listings only care about live
-- tokens; a partial index keeps that lookup small as revoked rows pile up.
CREATE INDEX refresh_tokens_live_by_user_idx
    ON refresh_tokens (user_id)
    WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE refresh_tokens;
DROP TABLE users;
-- +goose StatementEnd
