ALTER TABLE users
    ADD COLUMN IF NOT EXISTS bio text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS phone text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS avatar_url text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

CREATE TABLE IF NOT EXISTS api_keys (
    id text PRIMARY KEY,
    admin_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    key_prefix text NOT NULL UNIQUE,
    key_hash text NOT NULL,
    key_secret text NOT NULL DEFAULT '',
    scopes text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active',
    last_used_at timestamptz,
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    revoked_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_api_keys_admin_created_at ON api_keys(admin_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_keys_status ON api_keys(status);

ALTER TABLE articles
    ADD COLUMN IF NOT EXISTS created_by_api_key_id text REFERENCES api_keys(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS api_actor_admin_id text REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_articles_api_actor_admin_id ON articles(api_actor_admin_id);

ALTER TABLE media
    ADD COLUMN IF NOT EXISTS created_by_api_key_id text REFERENCES api_keys(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'dashboard_upload';

CREATE INDEX IF NOT EXISTS idx_media_api_key_created_at ON media(created_by_api_key_id, created_at DESC);
