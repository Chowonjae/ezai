-- client_keys: 클라이언트 인증 키 쌍 (암호화 DB)
CREATE TABLE IF NOT EXISTS client_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id TEXT NOT NULL UNIQUE,           -- 서비스 식별자 (예: "mobile-app-prod")
    secret_hash TEXT NOT NULL,                -- SHA-256(secret), hex 64자
    secret_prefix TEXT NOT NULL,              -- 시크릿 앞 12자 (목록에서 식별용: "ezs_a3f1...")
    service_name TEXT NOT NULL,               -- 소속 서비스명
    description TEXT DEFAULT '',              -- 설명
    is_active INTEGER DEFAULT 1,
    expires_at TEXT NOT NULL,                 -- 만료 시간 (RFC3339 UTC)
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_client_keys_client_id ON client_keys(client_id);
CREATE INDEX IF NOT EXISTS idx_client_keys_active ON client_keys(is_active, expires_at);
CREATE INDEX IF NOT EXISTS idx_client_keys_service ON client_keys(service_name);
