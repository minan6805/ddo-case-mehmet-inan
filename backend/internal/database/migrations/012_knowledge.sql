CREATE TABLE knowledge_articles (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title text NOT NULL CHECK(char_length(btrim(title)) BETWEEN 1 AND 200),
    body text NOT NULL CHECK(char_length(btrim(body)) BETWEEN 1 AND 10000),
    category_code text NOT NULL REFERENCES ticket_categories(code),
    status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','archived')),
    author_id bigint NOT NULL REFERENCES users(id),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX knowledge_articles_status_category_id_idx ON knowledge_articles(status,category_code,id DESC);
CREATE TABLE knowledge_seed_state (name text PRIMARY KEY);
