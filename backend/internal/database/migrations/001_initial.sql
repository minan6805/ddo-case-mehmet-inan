CREATE TABLE roles (
    code text PRIMARY KEY,
    label text NOT NULL
);

CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL UNIQUE CHECK (email = lower(email)),
    display_name text NOT NULL CHECK (length(trim(display_name)) > 0),
    password_hash text NOT NULL,
    role_code text NOT NULL REFERENCES roles(code),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tickets (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    requester_id bigint NOT NULL REFERENCES users(id),
    assignee_id bigint REFERENCES users(id),
    title text NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    description text NOT NULL CHECK (length(trim(description)) > 0),
    status text NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'in_progress', 'waiting_customer', 'resolved', 'closed')),
    priority text NOT NULL DEFAULT 'normal'
        CHECK (priority IN ('low', 'normal', 'high', 'critical')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tickets_requester_created_idx ON tickets (requester_id, created_at DESC, id DESC);
CREATE INDEX tickets_assignee_status_idx ON tickets (assignee_id, status);
