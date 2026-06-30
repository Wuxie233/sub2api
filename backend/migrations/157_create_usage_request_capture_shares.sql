-- Store public share links for captured gateway request/response previews.

CREATE TABLE IF NOT EXISTS usage_request_capture_shares (
    id              BIGSERIAL PRIMARY KEY,
    share_id        VARCHAR(64) NOT NULL,
    request_id      VARCHAR(64) NOT NULL,
    api_key_id      BIGINT,
    created_by      BIGINT,
    label           VARCHAR(255),
    expires_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    view_count      INT NOT NULL DEFAULT 0,
    last_viewed_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_request_capture_shares_share_id_unique
    ON usage_request_capture_shares (share_id);

CREATE INDEX IF NOT EXISTS idx_usage_request_capture_shares_request_id
    ON usage_request_capture_shares (request_id);

CREATE INDEX IF NOT EXISTS idx_usage_request_capture_shares_expires_at
    ON usage_request_capture_shares (expires_at);
