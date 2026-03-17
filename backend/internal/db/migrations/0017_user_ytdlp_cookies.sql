CREATE TABLE IF NOT EXISTS user_ytdlp_cookies (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    label TEXT NOT NULL,
    domain_rule TEXT NOT NULL,
    format TEXT NOT NULL CHECK (format IN ('header', 'cookies_txt')),
    cipher_text TEXT NOT NULL,
    nonce TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_user_ytdlp_cookies_user_updated
ON user_ytdlp_cookies(user_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_ytdlp_cookies_user_domain
ON user_ytdlp_cookies(user_id, domain_rule);
