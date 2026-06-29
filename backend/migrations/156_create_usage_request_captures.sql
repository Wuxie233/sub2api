-- Store gzip-compressed gateway request/response capture payloads for later admin preview.
-- Payload bytes are held separately from usage_logs so capture retention can be managed independently.

CREATE TABLE IF NOT EXISTS usage_request_captures (
    id                     BIGSERIAL PRIMARY KEY,
    request_id             VARCHAR(64) NOT NULL,
    api_key_id             BIGINT,
    usage_log_id           BIGINT,
    user_id                BIGINT,
    account_id             BIGINT,
    provider               VARCHAR(50) NOT NULL,
    model                  VARCHAR(100) NOT NULL,
    endpoint               VARCHAR(128) NOT NULL,
    stream                 BOOLEAN NOT NULL DEFAULT FALSE,
    status_code            INT NOT NULL,
    duration_ms            BIGINT NOT NULL,
    request_bytes          BIGINT NOT NULL,
    response_bytes         BIGINT NOT NULL,
    compressed_bytes       BIGINT NOT NULL,
    truncated              BOOLEAN NOT NULL DEFAULT FALSE,
    truncate_reason        VARCHAR(255),
    capture_schema_version INT NOT NULL DEFAULT 1,
    payload_gzip           BYTEA NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at             TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_usage_request_captures_request_id
    ON usage_request_captures (request_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_request_captures_request_id_api_key_unique
    ON usage_request_captures (request_id, api_key_id) NULLS NOT DISTINCT;

CREATE INDEX IF NOT EXISTS idx_usage_request_captures_expires_at
    ON usage_request_captures (expires_at);

CREATE INDEX IF NOT EXISTS idx_usage_request_captures_user_id
    ON usage_request_captures (user_id);
