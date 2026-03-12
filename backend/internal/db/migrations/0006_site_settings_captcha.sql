CREATE TABLE IF NOT EXISTS site_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    site_title TEXT NOT NULL DEFAULT 'MoeVideo',
    site_description TEXT NOT NULL DEFAULT '',
    site_logo_media_id TEXT,
    register_enabled INTEGER NOT NULL DEFAULT 1 CHECK (register_enabled IN (0, 1)),
    updated_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (site_logo_media_id) REFERENCES media_objects(id),
    FOREIGN KEY (updated_by) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS auth_captcha_challenges (
    id TEXT PRIMARY KEY,
    scene TEXT NOT NULL CHECK (scene IN ('login', 'register')),
    answer_hash TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    created_at TEXT NOT NULL,
    created_ip TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_auth_captcha_expires_at
ON auth_captcha_challenges(expires_at);

INSERT OR IGNORE INTO site_settings (
    id,
    site_title,
    site_description,
    site_logo_media_id,
    register_enabled,
    updated_by,
    created_at,
    updated_at
) VALUES (
    1,
    'MoeVideo',
    'MoeVideo VOD - Stitch design implementation',
    NULL,
    1,
    NULL,
    datetime('now'),
    datetime('now')
);
