INSERT INTO categories (id, name, slug, created_at, updated_at)
VALUES
    ('cat-teknologi', 'Teknologi', 'teknologi', now(), now()),
    ('cat-politik', 'Politik', 'politik', now(), now()),
    ('cat-olahraga', 'Olahraga', 'olahraga', now(), now()),
    ('cat-bisnis', 'Bisnis', 'bisnis', now(), now()),
    ('cat-hiburan', 'Hiburan', 'hiburan', now(), now())
ON CONFLICT (id) DO NOTHING;
