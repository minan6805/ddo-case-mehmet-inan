CREATE TABLE ticket_comments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id bigint NOT NULL REFERENCES tickets(id),
    author_id bigint NOT NULL REFERENCES users(id),
    visibility text NOT NULL CHECK (visibility IN ('external','internal')),
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 10000),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX ticket_comments_page_idx ON ticket_comments(ticket_id,id DESC);
CREATE TRIGGER ticket_comments_immutable BEFORE UPDATE OR DELETE ON ticket_comments
    FOR EACH ROW EXECUTE FUNCTION reject_audit_change();
CREATE TRIGGER ticket_comments_no_truncate BEFORE TRUNCATE ON ticket_comments
    FOR EACH STATEMENT EXECUTE FUNCTION reject_audit_change();
