CREATE TABLE IF NOT EXISTS articles (
    id text PRIMARY KEY,
    author_id text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    title text NOT NULL,
    slug text NOT NULL UNIQUE,
    excerpt text NOT NULL DEFAULT '',
    content text NOT NULL DEFAULT '',
    category text NOT NULL DEFAULT '',
    hero_image_url text NOT NULL DEFAULT '',
    status text NOT NULL,
    review_note text NOT NULL DEFAULT '',
    reviewed_by text NOT NULL DEFAULT '',
    reviewed_at timestamptz,
    published_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_articles_status_created_at ON articles(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_author_updated_at ON articles(author_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_category ON articles(category);
