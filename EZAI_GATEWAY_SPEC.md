# ezai - AI Gateway 설계 문서

## 1. 프로젝트 개요

다양한 AI 모델(Gemini, Claude, GPT, Perplexity, 로컬 LLM 등)을 하나의 통합 API로 관리하는 AI Gateway 서비스.
여러 서비스에서 AI를 호출할 때 모델 선택, 프롬프트 관리, 장애 대응, 비용 추적 등을 중앙에서 제어한다.

### 아키텍처

```
[클라이언트 / 서비스들]
        │
        ▼
      [ezai]  ← 통합 API, 프롬프트 관리, 라우팅, 로깅, 비용 추적
        │
        ├── Vertex AI (Gemini)
        ├── OpenAI (GPT)
        ├── Anthropic (Claude)
        ├── Perplexity
        └── Ollama (로컬 LLM)
```

---

## 2. 핵심 기능 요구사항

### 2.1 모델 라우팅

- 클라이언트가 요청 시 사용할 모델(provider + model)을 지정
- 모델별 파라미터(max_tokens, temperature 등)를 통합 인터페이스로 변환

### 2.2 Fallback (장애 대응) - 사용자 정의 가능

장애 시 어떤 LLM으로 전환할지를 **사용자(클라이언트)가 직접 설정**할 수 있어야 한다.

#### Fallback 설정 방식

**요청 단위 설정** - 각 API 요청에 fallback 체인 포함:

```json
POST /chat
{
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "messages": [...],
    "fallback": [
        {"provider": "claude", "model": "claude-sonnet-4-6"},
        {"provider": "gpt", "model": "gpt-4o"}
    ],
    "fallback_policy": "on_error"
}
```

**프로젝트 단위 설정** - 프로젝트별 기본 fallback 정책:

```yaml
# config/projects/ecommerce.yaml
fallback:
  default_chain:
    - provider: gemini
      model: gemini-2.5-flash
    - provider: claude
      model: claude-sonnet-4-6
    - provider: gpt
      model: gpt-4o
  policy: on_error          # on_error | on_timeout | on_rate_limit | always_fastest
  timeout_ms: 10000         # 이 시간 초과 시 다음 모델로
  max_retries: 1            # 같은 모델 재시도 횟수
```

**글로벌 설정** - 시스템 전체 기본값:

```yaml
# config/fallback_global.yaml
circuit_breaker:
  failure_threshold: 5       # 연속 5회 실패 시 차단
  recovery_timeout_sec: 60   # 60초 후 재시도
  half_open_requests: 3      # 복구 테스트 요청 수

providers:
  gemini:
    max_concurrent: 30
    timeout_ms: 30000
  claude:
    max_concurrent: 20
    timeout_ms: 30000
  gpt:
    max_concurrent: 30
    timeout_ms: 30000
  ollama:
    max_concurrent: 5
    timeout_ms: 60000
  perplexity:
    max_concurrent: 10
    timeout_ms: 30000
```

#### Fallback 정책 종류

| 정책 | 동작 |
|------|------|
| `on_error` | API 에러(5xx, 네트워크 장애) 시 다음 모델로 |
| `on_timeout` | 지정 시간 초과 시 다음 모델로 |
| `on_rate_limit` | 429(Rate Limit) 시 다음 모델로 |
| `always_fastest` | 모든 모델에 동시 요청, 가장 빠른 응답 사용 (비용 높음) |

### 2.3 프롬프트 관리

프롬프트를 코드에서 분리하고, 계층 구조로 조합한다.

#### 디렉토리 구조

```
prompts/
├── base.yaml                    ← 공통 (톤, 언어, 출력 형식)
├── projects/
│   ├── ecommerce.yaml           ← 프로젝트별 도메인 지식
│   └── chatbot.yaml
├── models/
│   ├── gemini.yaml              ← 모델별 특화 설정
│   ├── claude.yaml
│   └── gpt.yaml
└── tasks/
    ├── summarize.yaml           ← 기본 태스크 프롬프트
    ├── summarize.gemini.yaml    ← 모델 특화 오버라이드
    ├── translate.yaml
    └── classify.yaml
```

#### 조합 우선순위

```
최종 프롬프트 = base + project + model(선택) + task
모델 특화 오버라이드: tasks/{task}.{model}.yaml > tasks/{task}.yaml
```

#### 저장소 전략

- **초기**: YAML 파일 + Git (버전 관리, PR 리뷰)
- **확장 시**: DB(PostgreSQL) 기반 + 버전 태그 + A/B 테스트

### 2.4 대량 트래픽 처리

#### 필수 설계

1. **동시 처리**: Go goroutine 기반 경량 동시성
2. **동시성 제어**: 프로바이더별 채널/세마포어 (`chan struct{}`)
3. **큐 기반 배치**: 실시간 불필요한 요청은 Redis 큐로 분리
4. **스트리밍**: SSE(Server-Sent Events)로 긴 응답 처리

#### API 엔드포인트

```
Go HTTP Server
├── POST /chat              ← 실시간 요청
├── POST /chat/stream       ← 스트리밍 응답 (SSE)
├── POST /batch             ← 대량 요청 → 큐, 202 반환
├── GET  /batch/{job_id}    ← 배치 결과 조회
├── GET  /health            ← 헬스체크
├── GET  /models            ← 사용 가능한 모델 목록
└── GET  /usage             ← 비용/사용량 조회
```

#### 목표 규모: 중규모 (동시 수백 건)

구성: Go HTTP Server + Redis 큐 + Worker goroutine

#### 서버 리소스 분석

Gateway는 요청을 중계할 뿐, 실제 추론 연산은 AI 프로바이더 서버에서 수행한다.
goroutine 기반이므로 AI 응답 대기 중 CPU를 거의 사용하지 않는다.

```
Gateway가 하는 일              → 리소스 거의 안 씀
├── JSON 파싱/변환              (마이크로초)
├── goroutine HTTP 전달          (대기 중 CPU 0%)
├── 토큰 카운트/비용 계산        (마이크로초)
└── 로그 기록                   (마이크로초)

AI 프로바이더가 하는 일         → 무거운 연산은 전부 여기서
└── 모델 추론 (2~30초)
```

### 2.5 비용 추적 및 관리

비용 데이터는 별도 테이블 없이 `request_logs` 테이블(2.7절)에서 집계한다.
모든 요청에 토큰 수, 비용, 레이턴시가 기록되므로 이를 기반으로 조회/집계한다.

#### 가격 테이블

프로바이더별 토큰 단가를 설정 파일로 관리한다. 가격 변동 시 이 파일만 업데이트.

```yaml
# config/pricing.yaml
pricing:
  gemini-2.5-flash:
    input_per_1m_tokens: 0.15
    output_per_1m_tokens: 0.60
    currency: USD
    updated_at: "2026-04-16"
  gemini-2.5-pro:
    input_per_1m_tokens: 1.25
    output_per_1m_tokens: 10.00
  claude-sonnet-4-6:
    input_per_1m_tokens: 3.00
    output_per_1m_tokens: 15.00
  gpt-4o:
    input_per_1m_tokens: 2.50
    output_per_1m_tokens: 10.00
  ollama/*:
    input_per_1m_tokens: 0
    output_per_1m_tokens: 0
```

#### 조회 API

```
GET /usage?period=daily&date=2026-04-16
GET /usage?period=monthly&month=2026-04
GET /usage?period=yearly&year=2026
GET /usage?period=custom&from=2026-01-01&to=2026-04-16
```

필터 파라미터:

| 파라미터 | 설명 | 예시 |
|----------|------|------|
| `period` | 조회 기간 단위 | `daily`, `monthly`, `yearly`, `custom` |
| `provider` | 프로바이더 필터 | `gemini`, `claude` |
| `model` | 모델 필터 | `gemini-2.5-flash` |
| `project` | 프로젝트 필터 | `ecommerce` |
| `client_id` | 호출 서비스 필터 | `service-a` |
| `group_by` | 그룹핑 기준 | `provider`, `model`, `project`, `client_id` |

#### 응답 예시

```json
GET /usage?period=monthly&month=2026-04&group_by=provider

{
    "period": "2026-04",
    "total": {
        "requests": 45230,
        "input_tokens": 12500000,
        "output_tokens": 8300000,
        "cost_usd": 142.50
    },
    "breakdown": [
        {
            "provider": "gemini",
            "requests": 25000,
            "input_tokens": 7000000,
            "output_tokens": 4500000,
            "cost_usd": 38.20
        },
        {
            "provider": "claude",
            "requests": 15000,
            "input_tokens": 4000000,
            "output_tokens": 2800000,
            "cost_usd": 84.00
        },
        {
            "provider": "ollama",
            "requests": 5230,
            "input_tokens": 1500000,
            "output_tokens": 1000000,
            "cost_usd": 0
        }
    ]
}
```

#### 비용 초기화 정책

비용 데이터는 함부로 삭제하면 안 되므로, 초기화가 아닌 **아카이브** 방식을 기본으로 한다.

| 방식 | 설명 | 용도 |
|------|------|------|
| **아카이브** (권장) | 원본 데이터를 별도 테이블/스토리지로 이동 후 집계 요약만 보관 | 운영 DB 경량화 + 이력 보존 |
| **소프트 리셋** | 집계 카운터만 0으로 리셋, 상세 로그는 유지 | 월별 예산 리셋 |
| **하드 삭제** | 지정 기간 이전 데이터 완전 삭제 | 개발/테스트 환경 정리, GDPR 등 규정 준수 |

```yaml
# config/usage_retention.yaml
retention:
  # 상세 로그 (요청별 기록)
  detail_logs:
    hot_storage_days: 90        # 최근 90일은 DB에 보관 (빠른 조회)
    archive_after_days: 90      # 90일 이후 아카이브 스토리지로 이동
    delete_after_days: 365      # 365일 이후 완전 삭제 (0이면 영구 보관)

  # 집계 데이터 (일별/월별 요약)
  aggregated:
    daily_keep_days: 365        # 일별 요약은 1년 보관
    monthly_keep_years: 5       # 월별 요약은 5년 보관
    yearly_keep_years: 0        # 연별 요약은 영구 보관

  # 초기화 API 접근 제어
  reset:
    require_admin_role: true
    require_confirmation: true   # "CONFIRM-DELETE-2026-04" 같은 확인 문자열 요구
    allowed_operations:
      - soft_reset               # 집계 카운터 리셋
      - archive                  # 아카이브로 이동
      # - hard_delete            # 기본 비활성화, 필요 시 명시적 활성화
```

초기화 API:

```
POST /admin/usage/archive     ← 지정 기간 데이터를 아카이브로 이동
POST /admin/usage/reset       ← 집계 카운터 소프트 리셋
DELETE /admin/usage            ← 하드 삭제 (기본 비활성화)
```

```json
POST /admin/usage/archive
{
    "before": "2026-01-01",
    "confirmation": "CONFIRM-ARCHIVE-2026-01",
    "reason": "2025년 데이터 아카이브"
}
```

### 2.6 데이터 저장소 분리 정책

비밀 정보는 DB, 설정 정보는 YAML 파일로 분리한다.

#### 원칙

- **비밀 정보 (API 키 등)** → 암호화된 내장 DB (SQLite + SQLCipher)
- **설정 정보 (프롬프트, 라우팅 등)** → YAML + Git
- **운영 데이터 (사용량 로그, 비용)** → SQLite → 규모 커지면 PostgreSQL

#### 상세 분류

| 데이터 | 저장소 | 이유 |
|--------|--------|------|
| API 키, 시크릿 | **SQLite (암호화)** | 보안, 감사 추적, Git 노출 방지 |
| 프롬프트 | YAML + Git | 버전 관리, PR 리뷰 |
| Fallback/라우팅 설정 | YAML + Git | 설정 변경 추적 |
| 가격 테이블 | YAML + Git | 변경 이력, 가독성 |
| 사용량/비용 로그 | **SQLite → 추후 PostgreSQL** | 집계 쿼리, 장기 보관 |

#### API 키 관리 (SQLite + 암호화)

내장 DB를 사용하는 이유:
- 외부 DB 서버 불필요 → 인프라 단순화
- 네트워크 노출 없음 → 공격 표면 축소
- 프로세스 내부에서만 접근 → 외부 접속 차단이 기본값
- `.db` 파일은 바이너리라 Git에 실수로 커밋해도 평문 노출 안 됨

보안 요구사항:
- **저장 시 암호화**: SQLCipher 또는 AES-256으로 키 값 암호화
- **복호화 키**: 환경변수 또는 시크릿 매니저에서 주입 (코드/파일에 포함 금지)
- **파일 권한**: DB 파일 `chmod 600` (소유자만 읽기/쓰기)
- **로그 마스킹**: API 키가 로그에 찍히지 않도록 마스킹 처리
- **변경 감사**: 키 등록/수정/삭제 시 감사 로그 테이블에 기록

```sql
-- API 키 테이블
CREATE TABLE provider_keys (
    id INTEGER PRIMARY KEY,
    provider TEXT NOT NULL,          -- 'gemini', 'claude', 'gpt', 'perplexity'
    key_name TEXT NOT NULL,          -- 식별용 이름
    encrypted_value BLOB NOT NULL,   -- 암호화된 키 값
    key_type TEXT DEFAULT 'api_key', -- 'api_key', 'service_account_json'
    is_active INTEGER DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- 키 변경 감사 로그
CREATE TABLE key_audit_log (
    id INTEGER PRIMARY KEY,
    provider_key_id INTEGER,
    action TEXT NOT NULL,            -- 'created', 'updated', 'rotated', 'deactivated', 'deleted'
    performed_by TEXT,
    performed_at TEXT NOT NULL,
    detail TEXT                      -- 변경 사유
);
```

관리 API:

```
GET    /admin/keys                ← 키 목록 (값은 마스킹)
POST   /admin/keys                ← 키 등록
PUT    /admin/keys/{id}           ← 키 수정
POST   /admin/keys/{id}/rotate    ← 키 로테이션 (새 키 등록 + 기존 키 비활성화)
DELETE /admin/keys/{id}           ← 키 비활성화
GET    /admin/keys/audit          ← 감사 로그 조회
```

스케일아웃 시 전환 경로:
- 단일 인스턴스 → SQLite (현재)
- 다중 인스턴스 → HashiCorp Vault 또는 AWS Secrets Manager로 전환

### 2.7 로그 정책

모든 API 호출의 전체 흐름을 세세하게 추적할 수 있어야 한다.

#### 요청 추적 ID

모든 요청에 고유 `trace_id`를 부여하고, 요청의 전체 생명주기를 이 ID로 추적한다.

```
trace_id: "tr_20260416_143000_a1b2c3"

[수신] 클라이언트 요청 → [라우팅] 모델 결정 → [전송] AI API 호출 → [응답] 결과 반환
  │                        │                     │                    │
  └── 로그 ①               └── 로그 ②            └── 로그 ③           └── 로그 ④
```

#### 로그 레벨

| 레벨 | 기록 내용 | 용도 |
|------|----------|------|
| **REQUEST** | 수신된 원본 요청 전체 | 누가, 어디서, 뭘 요청했는지 |
| **ROUTING** | 모델 선택 과정, fallback 발생 여부 | 왜 이 모델이 선택됐는지 |
| **PROVIDER** | AI API 호출/응답 상세 | 프로바이더와 무슨 데이터를 주고받았는지 |
| **RESPONSE** | 클라이언트에게 반환한 최종 응답 | 최종 결과 확인 |
| **ERROR** | 에러 발생 시 상세 정보 | 장애 원인 분석 |

#### 로그 스키마

```sql
CREATE TABLE request_logs (
    id INTEGER PRIMARY KEY,
    trace_id TEXT NOT NULL UNIQUE,
    timestamp TEXT NOT NULL,

    -- 누가 (호출자)
    client_id TEXT,                  -- 호출 서비스 ID
    client_ip TEXT,                  -- 호출자 IP
    api_key_id INTEGER,              -- 사용된 API 키 (FK, 값은 저장 안 함)

    -- 어디서 (프로젝트/태스크)
    project TEXT,                    -- 프로젝트명
    task TEXT,                       -- 태스크명

    -- 뭘 (요청 내용)
    requested_provider TEXT,         -- 요청된 프로바이더
    requested_model TEXT,            -- 요청된 모델
    prompt_hash TEXT,                -- 프롬프트 해시 (내용 대신 해시로 저장, 개인정보 보호)
    input_preview TEXT,              -- 입력 앞 200자 (선택적, 설정으로 ON/OFF)
    options_json TEXT,               -- temperature, max_tokens 등

    -- 어떻게 (라우팅 결과)
    actual_provider TEXT,            -- 실제 호출된 프로바이더
    actual_model TEXT,               -- 실제 호출된 모델
    fallback_used INTEGER DEFAULT 0, -- fallback 발생 여부
    fallback_chain_json TEXT,        -- fallback 시도 이력 [{provider, model, error, latency_ms}, ...]
    routing_reason TEXT,             -- 라우팅 결정 사유

    -- 결과
    status TEXT NOT NULL,            -- 'success', 'error', 'timeout', 'rate_limited'
    error_code TEXT,
    error_message TEXT,
    input_tokens INTEGER,
    output_tokens INTEGER,
    cost_usd REAL,
    latency_ms INTEGER,              -- 전체 응답 시간 (클라이언트 기준)
    provider_latency_ms INTEGER,     -- AI API 응답 시간만
    search_grounding INTEGER DEFAULT 0,

    -- 메타
    output_preview TEXT,             -- 출력 앞 200자 (선택적)
    metadata_json TEXT               -- 기타 커스텀 메타데이터
);

-- 빠른 조회를 위한 인덱스
CREATE INDEX idx_logs_timestamp ON request_logs(timestamp);
CREATE INDEX idx_logs_client ON request_logs(client_id, timestamp);
CREATE INDEX idx_logs_provider ON request_logs(actual_provider, timestamp);
CREATE INDEX idx_logs_project ON request_logs(project, timestamp);
CREATE INDEX idx_logs_status ON request_logs(status, timestamp);
CREATE INDEX idx_logs_trace ON request_logs(trace_id);
```

#### fallback 추적 예시

fallback이 발생한 경우 `fallback_chain_json`에 전체 시도 이력을 기록한다:

```json
{
    "trace_id": "tr_20260416_143000_a1b2c3",
    "requested_provider": "gemini",
    "requested_model": "gemini-2.5-flash",
    "actual_provider": "claude",
    "actual_model": "claude-sonnet-4-6",
    "fallback_used": true,
    "fallback_chain_json": [
        {
            "order": 1,
            "provider": "gemini",
            "model": "gemini-2.5-flash",
            "status": "error",
            "error": "503 Service Unavailable",
            "latency_ms": 5200
        },
        {
            "order": 2,
            "provider": "claude",
            "model": "claude-sonnet-4-6",
            "status": "success",
            "latency_ms": 2100
        }
    ],
    "routing_reason": "fallback:on_error - primary provider returned 503"
}
```

#### 로그 조회 API

```
GET /admin/logs?trace_id=tr_xxxxx                    ← 특정 요청 추적
GET /admin/logs?client_id=service-a&limit=100        ← 특정 서비스의 최근 요청
GET /admin/logs?provider=gemini&status=error          ← 특정 프로바이더 에러 내역
GET /admin/logs?project=ecommerce&date=2026-04-16    ← 프로젝트별 일별 조회
GET /admin/logs?fallback_used=true                    ← fallback 발생 건만 조회
GET /admin/logs/stats?group_by=actual_model&period=daily  ← 모델별 일별 통계
```

필터 파라미터:

| 파라미터 | 설명 |
|----------|------|
| `trace_id` | 특정 요청 추적 |
| `client_id` | 호출 서비스 필터 |
| `client_ip` | 호출자 IP 필터 |
| `project` | 프로젝트 필터 |
| `requested_model` | 요청된 모델 |
| `actual_model` | 실제 호출된 모델 |
| `provider` | 프로바이더 필터 |
| `status` | success / error / timeout / rate_limited |
| `fallback_used` | fallback 발생 건만 |
| `date` / `from` / `to` | 기간 필터 |
| `min_latency_ms` | 느린 요청 추적 (예: 10000 이상) |

#### 로그 보존 설정

```yaml
# config/logging.yaml
logging:
  # 기록 범위
  record:
    input_preview: true          # 입력 앞 200자 기록
    input_preview_length: 200
    output_preview: true         # 출력 앞 200자 기록
    output_preview_length: 200
    full_prompt: false           # 전체 프롬프트 기록 (개인정보 주의, 기본 OFF)
    full_response: false         # 전체 응답 기록 (용량 주의, 기본 OFF)

  # 민감정보 처리
  privacy:
    hash_prompts: true           # 프롬프트를 해시로만 저장
    mask_api_keys: true          # 로그에 API 키 마스킹
    mask_pii: false              # PII 자동 마스킹 (확장 시)

  # 보존 기간 (usage_retention.yaml의 정책과 동일하게 적용)
  retention:
    detail_logs_days: 90
    archive_after_days: 90
    delete_after_days: 365
```

### 2.8 기타 기능

| 기능 | 설명 |
|------|------|
| 캐싱 | 동일/유사 요청 Redis 캐싱 |
| Rate Limiting | 클라이언트별 + 프로바이더별 한도 관리 |
| Google Search Grounding | Gemini 모델에 인터넷 검색 기능 연동 (Vertex AI Go SDK 사용) |

---

## 3. 기술 스택

| 구성 요소 | 기술 |
|-----------|------|
| 언어 | Go |
| HTTP 프레임워크 | net/http (표준 라이브러리) 또는 Gin/Echo |
| 큐 | Redis (또는 RabbitMQ) |
| 내장 DB | SQLite + SQLCipher (API 키, 사용량 로그) |
| 외부 DB (확장 시) | PostgreSQL (대규모 로그, 비용 집계) |
| 캐시 | Redis |
| 설정 관리 | YAML + Git (프롬프트, 라우팅, 가격 테이블) |
| 시크릿 관리 (확장 시) | HashiCorp Vault / AWS Secrets Manager |

### 프로바이더별 SDK/호출 방식

각 프로바이더는 공식 SDK를 사용한다. Ollama는 OpenAI Go SDK로 호출한다.

| 프로바이더 | 호출 방식 | SDK / 엔드포인트 |
|-----------|----------|-----------------|
| Gemini | **Vertex AI Go SDK** | `cloud.google.com/go/aiplatform` + `google.golang.org/genai` |
| Claude | **Anthropic Go SDK** | `github.com/anthropics/anthropic-sdk-go` |
| GPT | **OpenAI Go SDK** | `github.com/openai/openai-go` |
| Perplexity | **Perplexity 공식 SDK** | `github.com/ppl-ai/pplx-go` (OpenAI 호환이지만 공식 SDK 사용) |
| Ollama (로컬 LLM) | **OpenAI Go SDK** | `github.com/openai/openai-go` (base_url: `http://localhost:11434/v1`) |

---

## 4. 통합 요청/응답 스키마

### 요청

```json
{
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "messages": [
        {"role": "system", "content": "..."},
        {"role": "user", "content": "..."}
    ],
    "options": {
        "temperature": 0.7,
        "max_tokens": 4096,
        "stream": false,
        "search_grounding": false
    },
    "fallback": [
        {"provider": "claude", "model": "claude-sonnet-4-6"},
        {"provider": "gpt", "model": "gpt-4o"}
    ],
    "fallback_policy": "on_error",
    "project": "ecommerce",
    "task": "summarize",
    "prompt_variables": {
        "length": 200,
        "input": "..."
    },
    "metadata": {
        "client_id": "service-a",
        "request_id": "uuid"
    }
}
```

### 응답

```json
{
    "id": "req_xxxxx",
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "content": "응답 텍스트...",
    "usage": {
        "input_tokens": 150,
        "output_tokens": 300,
        "total_tokens": 450,
        "estimated_cost_usd": 0.0012
    },
    "metadata": {
        "latency_ms": 2340,
        "fallback_used": false,
        "fallback_reason": null,
        "search_sources": []
    }
}
```

---

## 5. 기존 작업물

### Vertex AI 테스트 CLI (`/Users/violet/llm/vertex_ai_api/`)

- `test_gemini.py`: Gemini 모델 테스트 CLI
  - 서비스 계정 JSON 키 인증
  - 모델 선택 (프리셋 목록 + 직접 입력)
  - `--discover`: 프로젝트에서 사용 가능한 모델 조회 (실제 API 호출로 확인)
  - `--search`: Google Search Grounding 지원 (google.genai SDK)
  - 대화형 모드 + 단일 프롬프트 모드
  - 기본 리전: us-central1 (미국 아이오와)
- `.venv/`: Python 가상환경 (google-cloud-aiplatform 설치됨)

### 사용 가능한 Gemini 모델 (확인 필요)

프리셋 목록에는 있지만, 프로젝트/리전에 따라 접근 불가할 수 있음.
`--discover` 옵션으로 실제 가용 모델을 먼저 확인해야 함.

```
gemini-3.1-pro-preview, gemini-3.1-flash-lite-preview    (Preview)
gemini-3-pro-preview, gemini-3-flash-preview              (Preview)
gemini-2.5-pro, gemini-2.5-flash, gemini-2.5-flash-lite  (GA)
gemini-2.0-flash, gemini-2.0-flash-lite                  (GA)
```

---

## 6. 참고 오픈소스

| 프로젝트 | 특징 |
|----------|------|
| LiteLLM | OpenAI 호환 API로 100+ 모델 통합. Python. 가장 실용적 |
| Portkey | AI Gateway 특화. 라우팅, fallback, 캐싱 내장 |
| OpenRouter | SaaS형. API 하나로 여러 모델 호출 |

직접 구축할 수도 있고, 위 도구 위에 커스텀 로직을 얹는 방식도 가능.

---

## 7. 다음 단계

1. [ ] Gateway 프로젝트 초기 구조 생성 (Go)
2. [ ] 프로바이더별 어댑터 구현 (Gemini, Claude, GPT, Perplexity, Ollama)
3. [ ] 통합 요청/응답 스키마 구현
4. [ ] Fallback 체인 + Circuit Breaker 구현
5. [ ] 프롬프트 관리 시스템 (YAML 기반)
6. [ ] goroutine 동시성 제어 (채널/세마포어)
7. [ ] Redis 큐 기반 배치 처리
8. [ ] 비용 추적 + 로깅
9. [ ] Google Search Grounding 통합 (Vertex AI Go SDK)
