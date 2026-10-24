CREATE TABLE skills (
    code text PRIMARY KEY,
    label text NOT NULL
);
INSERT INTO skills(code,label) VALUES
('general','Genel destek'),('network','Ağ ve bağlantı'),('hardware','Donanım'),('software','Yazılım');
CREATE TABLE category_skills (
    category_code text NOT NULL REFERENCES ticket_categories(code),
    skill_code text NOT NULL REFERENCES skills(code),
    PRIMARY KEY(category_code,skill_code)
);
INSERT INTO category_skills(category_code,skill_code) SELECT code,code FROM ticket_categories;
CREATE TABLE expert_profiles (
    user_id bigint PRIMARY KEY REFERENCES users(id),
    capacity integer NOT NULL DEFAULT 8 CHECK (capacity BETWEEN 1 AND 100),
    enabled boolean NOT NULL DEFAULT true,
    on_leave boolean NOT NULL DEFAULT false,
    timezone text NOT NULL DEFAULT 'Europe/Istanbul',
    shift_start_minute integer NOT NULL DEFAULT 0 CHECK (shift_start_minute BETWEEN 0 AND 1439),
    shift_end_minute integer NOT NULL DEFAULT 1440 CHECK (shift_end_minute BETWEEN 1 AND 1440),
    weekdays_mask integer NOT NULL DEFAULT 127 CHECK (weekdays_mask BETWEEN 1 AND 127),
    last_assignment_seq bigint NOT NULL DEFAULT 0,
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    CHECK (shift_start_minute<shift_end_minute)
);
CREATE TABLE expert_skills (
    expert_id bigint NOT NULL REFERENCES expert_profiles(user_id),
    skill_code text NOT NULL REFERENCES skills(code),
    level integer NOT NULL CHECK (level BETWEEN 1 AND 5),
    PRIMARY KEY(expert_id,skill_code)
);
ALTER TABLE tickets ADD CONSTRAINT tickets_expert_fk FOREIGN KEY(assignee_id) REFERENCES expert_profiles(user_id);
ALTER TABLE tickets ADD COLUMN assignment_checked_at timestamptz NOT NULL DEFAULT '-infinity';
CREATE INDEX tickets_active_assignment_check_idx ON tickets(assignment_checked_at,id)
    WHERE status IN ('open','in_progress','waiting_customer');
CREATE INDEX tickets_active_expert_idx ON tickets(assignee_id,id)
    WHERE status IN ('open','in_progress','waiting_customer');
CREATE TABLE assignment_rotation (
    id integer PRIMARY KEY CHECK (id=1),
    last_seq bigint NOT NULL DEFAULT 0
);
INSERT INTO assignment_rotation(id) VALUES(1);
CREATE TABLE assignment_queue (
    ticket_id bigint PRIMARY KEY REFERENCES tickets(id),
    reason text NOT NULL,
    queued_at timestamptz NOT NULL,
    attempted_at timestamptz NOT NULL
);
INSERT INTO assignment_queue(ticket_id,reason,queued_at,attempted_at)
SELECT id,'no_eligible_expert',created_at,created_at FROM tickets
WHERE assignee_id IS NULL AND status IN ('open','in_progress','waiting_customer');
CREATE TABLE assignment_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id bigint NOT NULL REFERENCES tickets(id),
    from_expert_id bigint REFERENCES expert_profiles(user_id),
    to_expert_id bigint REFERENCES expert_profiles(user_id),
    actor_id bigint REFERENCES users(id),
    reason text NOT NULL,
    score jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL
);
CREATE INDEX assignment_history_ticket_id_idx ON assignment_history(ticket_id,id DESC);
CREATE FUNCTION reject_assignment_history_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'assignment_history is append-only';
END;
$$;
CREATE TRIGGER assignment_history_no_update_delete BEFORE UPDATE OR DELETE ON assignment_history
    FOR EACH ROW EXECUTE FUNCTION reject_assignment_history_change();
CREATE TRIGGER assignment_history_no_truncate BEFORE TRUNCATE ON assignment_history
    FOR EACH STATEMENT EXECUTE FUNCTION reject_assignment_history_change();
