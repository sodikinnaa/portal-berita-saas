INSERT INTO users (id, name, email, password_hash, role, status, created_at, updated_at)
VALUES
    ('user-admin', 'Admin Portal', 'admin@portal.test', '$2a$10$hISKvHtWGsWgLgvTgMP9tuQC8mIC1ZvNs74PSOdazrFIVyO9Ryi8C', 'admin', 'active', now(), now()),
    ('user-writer', 'Maria Sari', 'writer@portal.test', '$2a$10$ghCf.PuO9263gQkTmNaGZOqdpLqv7hZ.cKpdySk3fhQZVNBxAP6.u', 'writer', 'active', now(), now())
ON CONFLICT (id) DO NOTHING;
