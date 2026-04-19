-- request_logs: API 호출 로그 (일반 DB)
CREATE TABLE IF NOT EXISTS request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id TEXT NOT NULL UNIQUE,
    timestamp TEXT NOT NULL DEFAULT (datetime('now')),

    -- 누가 (호출자)
    client_id TEXT,
    client_ip TEXT,
    api_key_id INTEGER,

    -- 어디서 (프로젝트/태스크)
    project TEXT,
    task TEXT,

    -- 뭘 (요청 내용)
    requested_provider TEXT,
    requested_model TEXT,
    prompt_hash TEXT,
    input_preview TEXT,
    options_json TEXT,

    -- 어떻게 (라우팅 결과)
    actual_provider TEXT,
    actual_model TEXT,
    fallback_used INTEGER DEFAULT 0,
    fallback_chain_json TEXT,
    routing_reason TEXT,

    -- 결과
    status TEXT NOT NULL,                -- 'success', 'error', 'timeout', 'rate_limited'
    error_code TEXT,
    error_message TEXT,
    input_tokens INTEGER,
    output_tokens INTEGER,
    cost_usd REAL,
    latency_ms INTEGER,
    provider_latency_ms INTEGER,
    search_grounding INTEGER DEFAULT 0,

    -- 메타
    output_preview TEXT,
    metadata_json TEXT
);

-- 빠른 조회를 위한 인덱스
CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON request_logs(timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_client ON request_logs(client_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_provider ON request_logs(actual_provider, timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_project ON request_logs(project, timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_status ON request_logs(status, timestamp);
-- trace_id는 UNIQUE 제약이 이미 인덱스를 생성하므로 별도 인덱스 불필요
