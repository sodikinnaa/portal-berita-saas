CREATE TABLE IF NOT EXISTS site_settings (
    key text PRIMARY KEY,
    value text NOT NULL DEFAULT ''
);

INSERT INTO site_settings (key, value) VALUES
('social_facebook_url', '#'),
('social_facebook_count', '24.8K'),
('social_twitter_url', '#'),
('social_twitter_count', '18.1K'),
('social_youtube_url', '#'),
('social_youtube_count', '103K'),
('social_instagram_url', '#'),
('social_instagram_count', '56.4K')
ON CONFLICT (key) DO NOTHING;
