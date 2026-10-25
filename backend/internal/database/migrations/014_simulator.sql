CREATE TABLE simulation_runs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    namespace text NOT NULL UNIQUE,
    config jsonb NOT NULL,
    plan_hash text NOT NULL CHECK(length(plan_hash)=64),
    total_steps integer NOT NULL CHECK(total_steps>0),
    completed_steps integer NOT NULL DEFAULT 0 CHECK(completed_steps>=0 AND completed_steps<=total_steps),
    generated_tickets integer NOT NULL DEFAULT 0 CHECK(generated_tickets>=0),
    email_replays integer NOT NULL DEFAULT 0 CHECK(email_replays>=0),
    reply_deliveries integer NOT NULL DEFAULT 0 CHECK(reply_deliveries>=0),
    shift_steps integer NOT NULL DEFAULT 0 CHECK(shift_steps>=0),
    status text NOT NULL DEFAULT 'running' CHECK(status IN ('running','completed','failed')),
    checkpoint jsonb NOT NULL DEFAULT '{}',
    last_error text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    CHECK(status<>'completed' OR completed_steps=total_steps)
);
