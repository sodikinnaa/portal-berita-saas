INSERT INTO site_settings (key, value) VALUES
('site_description', 'NewsPaper adalah portal berita terpercaya yang menyajikan informasi terkini, mendalam, dan berimbang dari seluruh penjuru dunia. Kami berkomitmen pada jurnalisme berkualitas tinggi sejak 2005.')
ON CONFLICT (key) DO NOTHING;
