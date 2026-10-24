ALTER TABLE tickets ADD COLUMN merged_into_ticket_id bigint REFERENCES tickets(id),
    ADD CONSTRAINT ticket_no_self_merge CHECK (merged_into_ticket_id IS NULL OR merged_into_ticket_id<>id),
    ADD CONSTRAINT merged_ticket_closed CHECK (merged_into_ticket_id IS NULL OR status='closed');
CREATE INDEX tickets_merge_target_idx ON tickets(merged_into_ticket_id) WHERE merged_into_ticket_id IS NOT NULL;
-- Original comment rows remain immutable; only their current thread can change.
CREATE TABLE comment_locations (
    comment_id bigint PRIMARY KEY REFERENCES ticket_comments(id),
    ticket_id bigint NOT NULL REFERENCES tickets(id)
);
CREATE INDEX comment_locations_ticket_idx ON comment_locations(ticket_id,comment_id DESC);
CREATE TABLE ticket_operations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK(kind IN ('merge','split')),
    source_id bigint NOT NULL REFERENCES tickets(id),
    target_id bigint NOT NULL REFERENCES tickets(id),
    actor_id bigint NOT NULL REFERENCES users(id),
    comment_ids bigint[] NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK(source_id<>target_id)
);
CREATE INDEX ticket_operations_source_idx ON ticket_operations(source_id,id DESC);
CREATE INDEX ticket_operations_target_idx ON ticket_operations(target_id,id DESC);
CREATE TRIGGER ticket_operations_immutable BEFORE UPDATE OR DELETE ON ticket_operations
    FOR EACH ROW EXECUTE FUNCTION reject_audit_change();
CREATE TRIGGER ticket_operations_no_truncate BEFORE TRUNCATE ON ticket_operations
    FOR EACH STATEMENT EXECUTE FUNCTION reject_audit_change();
