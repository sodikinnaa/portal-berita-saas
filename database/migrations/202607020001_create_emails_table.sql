CREATE TABLE IF NOT EXISTS emails (
    id VARCHAR(50) PRIMARY KEY,
    user_id VARCHAR(50) REFERENCES users(id) ON DELETE SET NULL, -- Pemilik Inbox
    direction VARCHAR(20) NOT NULL, -- 'inbound' or 'outbound'
    sender VARCHAR(255) NOT NULL,   -- Alamat pengirim
    sender_name VARCHAR(255),       -- Nama pengirim
    recipient VARCHAR(255) NOT NULL, -- Alamat penerima
    subject TEXT,
    body_html TEXT,
    body_text TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'unread', -- 'unread', 'read', 'sent', 'failed'
    error_message TEXT,             -- Menyimpan alasan jika pengiriman gagal
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB                  -- Menyimpan raw event log / data parsing tambahan
);

CREATE INDEX idx_emails_user_id ON emails(user_id);
CREATE INDEX idx_emails_direction ON emails(direction);
CREATE INDEX idx_emails_recipient ON emails(recipient);
CREATE INDEX idx_emails_sender ON emails(sender);
CREATE INDEX idx_emails_status ON emails(status);
CREATE INDEX idx_emails_created_at ON emails(created_at DESC);
