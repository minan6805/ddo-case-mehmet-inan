CREATE TABLE incoming_emails (
    message_id text PRIMARY KEY CHECK (length(message_id) BETWEEN 5 AND 255),
    request_hash bytea NOT NULL CHECK (octet_length(request_hash)=32),
    ticket_id bigint NOT NULL REFERENCES tickets(id),
    comment_id bigint NOT NULL REFERENCES ticket_comments(id),
    sender_id bigint NOT NULL REFERENCES users(id),
    imported_by bigint NOT NULL REFERENCES users(id),
    created_ticket boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE comment_attachments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    comment_id bigint NOT NULL REFERENCES ticket_comments(id),
    filename text NOT NULL,
    media_type text NOT NULL,
    size_bytes integer NOT NULL CHECK(size_bytes BETWEEN 0 AND 1048576),
    sha256 bytea NOT NULL CHECK(octet_length(sha256)=32),
    data bytea NOT NULL CHECK(octet_length(data)=size_bytes),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX comment_attachments_comment_idx ON comment_attachments(comment_id,id);
CREATE TRIGGER incoming_emails_immutable BEFORE UPDATE OR DELETE ON incoming_emails
    FOR EACH ROW EXECUTE FUNCTION reject_audit_change();
CREATE TRIGGER incoming_emails_no_truncate BEFORE TRUNCATE ON incoming_emails
    FOR EACH STATEMENT EXECUTE FUNCTION reject_audit_change();
CREATE TRIGGER comment_attachments_immutable BEFORE UPDATE OR DELETE ON comment_attachments
    FOR EACH ROW EXECUTE FUNCTION reject_audit_change();
CREATE TRIGGER comment_attachments_no_truncate BEFORE TRUNCATE ON comment_attachments
    FOR EACH STATEMENT EXECUTE FUNCTION reject_audit_change();
