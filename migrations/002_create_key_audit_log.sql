-- key_audit_log: API 키 변경 감사 로그 (암호화 DB)
CREATE TABLE IF NOT EXISTS key_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_key_id INTEGER,
    action TEXT NOT NULL,                -- 'created', 'updated', 'rotated', 'deactivated', 'deleted'
    performed_by TEXT,
    performed_at TEXT NOT NULL DEFAULT (datetime('now')),
    detail TEXT                          -- 변경 사유
);
