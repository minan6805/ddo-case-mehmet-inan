-- Reports select a bounded cohort by ticket creation time, optionally category.
CREATE INDEX tickets_created_category_idx ON tickets(created_at,category_code);
