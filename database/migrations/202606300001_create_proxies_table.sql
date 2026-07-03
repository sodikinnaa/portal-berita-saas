CREATE TABLE IF NOT EXISTS proxies (
    id text PRIMARY KEY,
    ip text NOT NULL,
    port int NOT NULL,
    username text,
    password text,
    protocol text NOT NULL, -- e.g. 'http', 'socks5'
    status text NOT NULL DEFAULT 'checking', -- 'active', 'dead', 'checking'
    last_checked timestamptz,
    latency_ms int,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
