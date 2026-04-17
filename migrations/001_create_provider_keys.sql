-- provider_keys: 프로바이더 API 키 저장 (암호화 DB)
CREATE TABLE IF NOT EXISTS provider_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,              -- 'gemini', 'claude', 'gpt', 'perplexity'
    key_name TEXT NOT NULL,              -- 식별용 이름
    encrypted_value BLOB NOT NULL,       -- AES-256 암호화된 키 값
    key_type TEXT DEFAULT 'api_key',     -- 'api_key', 'service_account_json'
    is_active INTEGER DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_keys_provider_name ON provider_keys(provider, key_name);
