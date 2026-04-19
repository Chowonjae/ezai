-- 아카이브된 로그 요약 테이블
-- 상세 로그를 삭제하기 전, 일별 집계를 이 테이블에 보관한다.
CREATE TABLE IF NOT EXISTS archived_daily_summary (
    id INTEGER PRIMARY KEY,
    date TEXT NOT NULL,
    actual_provider TEXT,
    actual_model TEXT,
    project TEXT,
    client_id TEXT,
    requests INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd REAL NOT NULL DEFAULT 0,
    avg_latency_ms INTEGER NOT NULL DEFAULT 0,
    error_count INTEGER NOT NULL DEFAULT 0,
    archived_at TEXT NOT NULL,
    -- 동일 그룹의 중복 아카이브 방지
    UNIQUE(date, actual_provider, actual_model, project, client_id)
);

CREATE INDEX IF NOT EXISTS idx_archived_date ON archived_daily_summary(date);
CREATE INDEX IF NOT EXISTS idx_archived_provider ON archived_daily_summary(actual_provider, date);
CREATE INDEX IF NOT EXISTS idx_archived_project ON archived_daily_summary(project, date);
