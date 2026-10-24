CREATE TABLE asset_graph_revision (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0)
);
INSERT INTO asset_graph_revision DEFAULT VALUES;
CREATE TABLE assets (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code text NOT NULL UNIQUE CHECK(code ~ '^[A-Z0-9][A-Z0-9._-]{1,39}$'),
    name text NOT NULL CHECK(char_length(btrim(name)) BETWEEN 1 AND 160),
    kind text NOT NULL CHECK(kind IN ('server','service','application','network','device')),
    state text NOT NULL CHECK(state IN ('active','maintenance','retired')),
    owner_id bigint REFERENCES users(id),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX assets_kind_id_idx ON assets(kind,id DESC);
CREATE INDEX assets_owner_idx ON assets(owner_id);
-- provider -> dependent: an outage of the provider can affect the dependent.
CREATE TABLE asset_dependencies (
    provider_id bigint NOT NULL REFERENCES assets(id),
    dependent_id bigint NOT NULL REFERENCES assets(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(provider_id,dependent_id),
    CHECK(provider_id<>dependent_id)
);
CREATE INDEX asset_dependencies_reverse_idx ON asset_dependencies(dependent_id,provider_id);
CREATE TABLE asset_seed_state (name text PRIMARY KEY);
