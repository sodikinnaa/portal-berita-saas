INSERT INTO site_settings (key, value) VALUES
('site_title', 'NewsPaper'),
('site_tagline', 'Berita Terkini & Terpercaya')
ON CONFLICT (key) DO NOTHING;
