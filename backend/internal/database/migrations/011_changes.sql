CREATE TABLE change_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    requester_id bigint NOT NULL REFERENCES users(id),
    title text NOT NULL CHECK(char_length(btrim(title)) BETWEEN 1 AND 200),
    description text NOT NULL CHECK(char_length(btrim(description)) BETWEEN 1 AND 10000),
    rollback_plan text NOT NULL CHECK(char_length(btrim(rollback_plan)) BETWEEN 1 AND 10000),
    risk text NOT NULL CHECK(risk IN ('low','normal','high')),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL CHECK(ends_at>starts_at),
    status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','submitted','approved','implementing','completed','failed','rejected','cancelled')),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    graph_version bigint,
    impact_snapshot jsonb,
    approved_by bigint REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK(approved_by IS NULL OR approved_by<>requester_id),
    CHECK(status NOT IN ('approved','implementing','completed','failed') OR (approved_by IS NOT NULL AND graph_version IS NOT NULL AND impact_snapshot IS NOT NULL))
);
CREATE INDEX change_requests_status_id_idx ON change_requests(status,id DESC);
CREATE INDEX change_requests_requester_id_idx ON change_requests(requester_id,id DESC);
CREATE INDEX change_requests_reserved_window_idx ON change_requests USING gist(tstzrange(starts_at,ends_at,'[)')) WHERE status IN ('approved','implementing');
CREATE TABLE change_roots (
    change_id bigint NOT NULL REFERENCES change_requests(id),
    asset_id bigint NOT NULL REFERENCES assets(id),
    PRIMARY KEY(change_id,asset_id)
);
CREATE TABLE change_impacts (
    change_id bigint NOT NULL REFERENCES change_requests(id),
    asset_id bigint NOT NULL REFERENCES assets(id),
    PRIMARY KEY(change_id,asset_id)
);
CREATE INDEX change_impacts_asset_idx ON change_impacts(asset_id,change_id);
CREATE TABLE change_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    change_id bigint NOT NULL REFERENCES change_requests(id),
    actor_id bigint NOT NULL REFERENCES users(id),
    action text NOT NULL,
    from_status text,
    to_status text NOT NULL,
    note text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX change_events_page_idx ON change_events(change_id,id DESC);
CREATE TRIGGER change_events_immutable BEFORE UPDATE OR DELETE ON change_events
    FOR EACH ROW EXECUTE FUNCTION reject_audit_change();
CREATE TRIGGER change_events_no_truncate BEFORE TRUNCATE ON change_events
    FOR EACH STATEMENT EXECUTE FUNCTION reject_audit_change();
