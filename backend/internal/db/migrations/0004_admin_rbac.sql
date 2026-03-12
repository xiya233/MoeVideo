ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin'));

CREATE INDEX IF NOT EXISTS idx_users_role_status
ON users(role, status);

CREATE TABLE IF NOT EXISTS admin_audit_logs (
    id TEXT PRIMARY KEY,
    admin_user_id TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    ip TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (admin_user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_admin_audit_logs_created_at
ON admin_audit_logs(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_admin_audit_logs_admin_user
ON admin_audit_logs(admin_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_admin_audit_logs_resource
ON admin_audit_logs(resource_type, resource_id, created_at DESC);
