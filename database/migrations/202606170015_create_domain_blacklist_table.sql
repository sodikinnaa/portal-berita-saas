CREATE TABLE IF NOT EXISTS domain_blacklist (
    domain text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);
