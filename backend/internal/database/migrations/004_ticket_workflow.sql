CREATE TABLE ticket_categories (
    code text PRIMARY KEY,
    label text NOT NULL
);
INSERT INTO ticket_categories(code, label) VALUES
    ('general', 'Genel destek'), ('network', 'Ağ ve bağlantı'),
    ('hardware', 'Donanım'), ('software', 'Yazılım');
ALTER TABLE tickets ADD COLUMN category_code text NOT NULL DEFAULT 'general'
    REFERENCES ticket_categories(code);
ALTER TABLE tickets ADD CONSTRAINT ticket_description_length CHECK (length(description) <= 10000);
CREATE INDEX tickets_requester_id_desc_idx ON tickets (requester_id, id DESC);
CREATE INDEX tickets_status_id_desc_idx ON tickets (status, id DESC);

CREATE TABLE idempotency_keys (
    user_id bigint NOT NULL REFERENCES users(id),
    operation text NOT NULL,
    key text NOT NULL CHECK (length(key) BETWEEN 1 AND 128),
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    response_body jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, operation, key)
);
