CREATE TABLE IF NOT EXISTS gc_configs (
    id         BIGSERIAL PRIMARY KEY,
    schedule   VARCHAR(100) NOT NULL DEFAULT '0 3 * * *',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO gc_configs (schedule, enabled) VALUES ('0 3 * * *', TRUE);
