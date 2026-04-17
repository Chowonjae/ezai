# ezai - Multi AI Gateway

다양한 AI 모델(Gemini, Claude, GPT, Perplexity, Ollama)을 하나의 통합 API로 관리하는 AI Gateway 서비스.
여러 서비스에서 AI를 호출할 때 모델 선택, 프롬프트 관리, 장애 대응(Fallback), 비용 추적 등을 중앙에서 제어한다.

## 왜 필요한가?

여러 AI 프로바이더를 동시에 사용하는 환경에서 각 서비스마다 개별적으로 SDK를 통합하면 다음 문제가 발생한다:

- 프로바이더별로 다른 SDK, 인증, 요청/응답 형식을 각 서비스에서 반복 구현
- 특정 프로바이더 장애 시 수동 전환 필요
- 비용 추적이 분산되어 전체 지출 파악 불가
- 프롬프트가 코드에 하드코딩되어 변경 시 배포 필요

ezai는 이를 중앙 게이트웨이로 해결한다.

```
[서비스 A]  [서비스 B]  [서비스 C]
     \          |          /
      \         |         /
       +----- [ezai] ----+      ← 통합 API, 프롬프트 관리, 라우팅, 로깅, 비용 추적
              / | \  \
             /  |  \  \
     Gemini Claude GPT Perplexity  Ollama
    (Vertex AI)                  (로컬 LLM)
```

## 주요 기능

| 기능 | 설명 |
|------|------|
| **통합 API** | 5개 프로바이더를 동일한 요청/응답 형식으로 호출 |
| **Fallback** | 프로바이더 장애 시 자동으로 다음 모델로 전환 (4가지 정책) |
| **Circuit Breaker** | 프로바이더별 장애 감지 및 자동 차단/복구 |
| **프롬프트 관리** | YAML 기반 계층적 프롬프트 조합 (base + project + model + task) |
| **비용 추적** | 모든 요청의 토큰 수, 비용을 자동 계산 및 집계 |
| **스트리밍** | SSE(Server-Sent Events) 기반 실시간 응답 |
| **배치 처리** | Redis 큐 기반 대량 요청 비동기 처리 |
| **캐싱** | Redis 기반 동일 요청 응답 캐싱 |
| **Rate Limiting** | 클라이언트별 요청 빈도 제한 |
| **클라이언트 인증** | 키 쌍(client_id + secret) 기반 외부 클라이언트 인증, 셀프 로테이션, 만료 관리 |
| **API 키 관리** | AES-256 암호화 저장, 로테이션, 감사 로그 |

## 지원 프로바이더

| 프로바이더 | SDK | 모델 예시 |
|-----------|-----|----------|
| Gemini | Vertex AI Go SDK (`google.golang.org/genai`) | gemini-2.5-pro, gemini-2.5-flash |
| Claude | Anthropic Go SDK | claude-opus-4-6, claude-sonnet-4-6 |
| GPT | OpenAI Go SDK | gpt-4o, gpt-4o-mini |
| Perplexity | OpenAI 호환 API | sonar-pro, sonar-reasoning-pro |
| Ollama | OpenAI 호환 API (로컬) | llama3, mistral 등 |

---

## 설치

### 사전 요구사항

- **Go** 1.22 이상
- **GCC** (SQLCipher 빌드에 필요, `CGO_ENABLED=1`)
- **Redis** (캐싱, 배치 처리, Rate Limiting 사용 시)

macOS:

```bash
brew install go gcc redis
```

Ubuntu/Debian:

```bash
sudo apt install golang gcc libsqlite3-dev redis-server
```

### 프로젝트 클론 및 빌드

```bash
git clone https://github.com/Chowonjae/ezai.git
cd ezai
go mod tidy
CGO_ENABLED=1 go build -o bin/ezai ./cmd/ezai
```

또는 Makefile 사용:

```bash
make build
```

### DB 암호화 키 생성

API 키를 암호화 저장하기 위한 마스터 키를 생성한다.

```bash
./bin/ezai keys init-db
```

`.env` 파일에 `EZAI_DB_KEY`가 자동 생성된다:

```bash
# .env
export EZAI_DB_KEY=<64자리 hex 문자열>
```

환경변수 로드:

```bash
source .env
```

### API 키 등록

각 프로바이더의 API 키를 암호화 DB에 등록한다.

```bash
# Claude
ezai keys add claude --value sk-ant-api03-xxxxx

# GPT
ezai keys add gpt --value sk-xxxxx

# Perplexity
ezai keys add perplexity --value pplx-xxxxx

# Gemini (서비스 계정 JSON 파일)
ezai keys add gemini --file /path/to/service-account.json

# 등록 확인
ezai keys list
```

출력 예시:

```
ID   Provider     Type                 KeyName                Active
--------------------------------------------------------------
1    gemini       service_account_json main                   ✓
2    claude       api_key              main                   ✓
3    gpt          api_key              main                   ✓
4    perplexity   api_key              main                   ✓
```

### 서버 시작

```bash
source .env
./bin/ezai
```

또는:

```bash
make run
```

정상 시작 시 로그:

```
{"level":"info","msg":"로그 DB 초기화 완료","path":"data/ezai.db"}
{"level":"info","msg":"키 DB 초기화 완료 (암호화)","path":"data/keys.db"}
{"level":"info","msg":"프로바이더 등록","provider":"gemini"}
{"level":"info","msg":"프로바이더 등록","provider":"claude"}
{"level":"info","msg":"프로바이더 등록","provider":"gpt"}
{"level":"info","msg":"라우터 초기화 완료 (fallback/circuit breaker 활성화)"}
{"level":"info","msg":"서버 시작","addr":"0.0.0.0:8080"}
```

---

## 프로젝트 구조

```
ezai/
├── cmd/ezai/
│   └── main.go                  ← 진입점, CLI 커맨드, 서버 부트스트랩
├── internal/
│   ├── provider/                ← AI 프로바이더 어댑터
│   │   ├── provider.go          ← Provider 인터페이스 정의
│   │   ├── registry.go          ← 프로바이더 레지스트리
│   │   ├── gemini.go            ← Gemini (Vertex AI)
│   │   ├── claude.go            ← Claude (Anthropic)
│   │   ├── gpt.go               ← GPT (OpenAI)
│   │   ├── perplexity.go        ← Perplexity
│   │   └── ollama.go            ← Ollama (로컬 LLM)
│   ├── router/                  ← 라우팅 + Fallback 엔진
│   │   ├── router.go            ← 요청 라우팅, fallback 실행
│   │   ├── fallback.go          ← Fallback 정책 (4가지)
│   │   └── circuitbreaker.go    ← 프로바이더별 Circuit Breaker
│   ├── handler/                 ← HTTP 핸들러
│   │   ├── chat.go              ← POST /chat (프롬프트 조합 + 라우팅)
│   │   ├── stream.go            ← POST /chat/stream (SSE)
│   │   ├── batch.go             ← POST /batch, GET /batch/{job_id}
│   │   ├── usage.go             ← GET /usage, GET /usage/pricing
│   │   ├── usage_admin.go       ← 비용 아카이브/리셋/삭제 API
│   │   ├── admin.go             ← 프로바이더 키 관리, 감사 로그, 로그 조회
│   │   ├── clientkey_admin.go   ← 클라이언트 키 발급/목록/폐기/재발급
│   │   ├── clientkey_rotate.go  ← 클라이언트 키 셀프 로테이션
│   │   ├── models.go            ← GET /models
│   │   └── health.go            ← GET /health
│   ├── model/                   ← 요청/응답 데이터 구조체
│   │   ├── request.go           ← ChatRequest, Message, ChatOptions
│   │   ├── response.go          ← ChatResponse, UsageInfo, StreamChunk
│   │   ├── provider.go          ← ModelInfo
│   │   └── job.go               ← BatchJob
│   ├── config/                  ← 설정 로드
│   │   ├── config.go            ← server.yaml, fallback_global.yaml 로드
│   │   ├── pricing.go           ← 가격 테이블 관리 + 비용 계산
│   │   ├── prompt.go            ← 프롬프트 계층 조합 엔진
│   │   └── retention.go         ← 보존 정책 로드
│   ├── store/                   ← DB 저장소
│   │   ├── sqlite.go            ← SQLite 연결 + 마이그레이션 실행
│   │   ├── encrypted.go         ← SQLCipher 암호화 DB
│   │   ├── keystore.go          ← API 키 CRUD (암호화 저장/복호화)
│   │   ├── audit.go             ← 키 변경 감사 로그
│   │   ├── clientkey.go         ← 클라이언트 키 CRUD (해시 저장)
│   │   ├── requestlog.go        ← 비동기 요청 로그 기록 + 조회
│   │   ├── usage.go             ← 사용량/비용 집계 조회
│   │   └── usage_admin.go       ← 아카이브/리셋/삭제 로직
│   ├── middleware/              ← HTTP 미들웨어
│   │   ├── auth.go              ← CIDR 기반 인증 + 클라이언트 키 쌍 검증
│   │   ├── trustednet.go        ← admin 전용 신뢰 네트워크 제한
│   │   ├── ratelimit.go         ← Redis 기반 Rate Limiting
│   │   ├── requestid.go         ← Trace ID 생성
│   │   ├── clientid.go          ← Client ID 추출
│   │   ├── logger.go            ← 요청/응답 로깅
│   │   └── recovery.go          ← Panic 복구
│   ├── cache/
│   │   └── cache.go             ← Redis 응답 캐싱
│   ├── queue/                   ← Redis 큐 기반 배치 처리
│   │   ├── producer.go          ← 작업 큐 등록
│   │   ├── consumer.go          ← Worker Pool 소비
│   │   └── job.go               ← 작업 상태 관리
│   ├── concurrency/
│   │   └── semaphore.go         ← 프로바이더별 동시성 제어
│   ├── crypto/
│   │   ├── encrypt.go           ← AES-256-GCM 암호화/복호화
│   │   └── secret.go            ← 클라이언트 시크릿 생성 + SHA-256 해시
│   ├── service/
│   │   └── clientkey_validator.go ← 클라이언트 키 검증 서비스 (캐시 포함)
│   └── server/
│       ├── server.go            ← HTTP 서버 생성 + 의존성 주입
│       └── routes.go            ← 라우트 등록
├── config/                      ← YAML 설정 파일
│   ├── server.yaml              ← 서버, Redis, DB, 인증, 프로바이더 설정
│   ├── fallback_global.yaml     ← Circuit Breaker + 프로바이더별 동시성
│   ├── pricing.yaml             ← 모델별 토큰 단가
│   ├── usage_retention.yaml     ← 로그/비용 보존 정책
│   └── projects/
│       └── default.yaml         ← 기본 프로젝트 fallback 설정
├── prompts/                     ← YAML 프롬프트 템플릿
│   ├── base.yaml                ← 공통 시스템 프롬프트
│   ├── models/
│   │   ├── gemini.yaml          ← Gemini 특화 프롬프트
│   │   ├── claude.yaml          ← Claude 특화 프롬프트
│   │   └── gpt.yaml             ← GPT 특화 프롬프트
│   ├── projects/
│   │   ├── ecommerce.yaml       ← 이커머스 프로젝트 프롬프트
│   │   └── chatbot.yaml         ← 챗봇 프로젝트 프롬프트
│   └── tasks/
│       ├── summarize.yaml       ← 요약 태스크
│       ├── summarize.gemini.yaml ← 요약 (Gemini 특화 오버라이드)
│       ├── translate.yaml       ← 번역 태스크
│       └── classify.yaml        ← 분류 태스크
├── migrations/                  ← SQL 마이그레이션
│   ├── 001_create_provider_keys.sql
│   ├── 002_create_key_audit_log.sql
│   ├── 003_create_request_logs.sql
│   ├── 004_create_archived_logs.sql
│   └── 005_create_client_keys.sql
├── data/                        ← DB 파일 (gitignore)
│   ├── ezai.db                  ← 요청 로그 (SQLite)
│   └── keys.db                  ← API 키 (SQLCipher 암호화)
├── go.mod
├── go.sum
├── Makefile
├── .env.example
└── .gitignore
```

### 요청 처리 흐름

```
클라이언트 요청
    │
    ▼
[미들웨어 체인]
  Recovery → RequestID → Logger → Auth(키 쌍 검증) → RateLimit
    │
    ▼
[ChatHandler]
  1. 요청 파싱 (JSON → ChatRequest)
  2. 프롬프트 조합 (project/task 지정 시)
     base.yaml + projects/{project}.yaml + models/{provider}.yaml + tasks/{task}.yaml
  3. 캐시 조회 (Redis, 히트 시 즉시 반환)
  4. Router 실행
     │
     ├─ Circuit Breaker 상태 확인
     ├─ Semaphore 획득 (동시성 제어)
     ├─ Provider SDK 호출
     ├─ 실패 시 → Fallback 정책에 따라 다음 모델로
     └─ 성공 시 → 응답 반환
  5. 비용 계산 (pricing.yaml 기준)
  6. 로그 기록 (비동기, request_logs 테이블)
  7. 캐시 저장
    │
    ▼
클라이언트 응답
```

---

## 설정

### 서버 설정 (`config/server.yaml`)

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout_sec: 30
  write_timeout_sec: 120    # 스트리밍 응답을 위해 길게 설정
  shutdown_timeout_sec: 30

redis:
  addr: "localhost:6379"
  password: ""
  db: 0
  pool_size: 20

database:
  logs_path: "data/ezai.db"    # 요청 로그 (일반 SQLite)
  keys_path: "data/keys.db"    # API 키 (SQLCipher 암호화)

auth:
  trusted_cidrs:               # 이 대역은 인증 없이 접근 가능
    - "127.0.0.0/8"
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "192.168.0.0/16"

providers:
  gemini:
    enabled: true
    location: "us-central1"
  claude:
    enabled: true
  gpt:
    enabled: true
  perplexity:
    enabled: true
    base_url: "https://api.perplexity.ai"
  ollama:
    enabled: true
    base_url: "http://localhost:11434/v1"
```

### Fallback / Circuit Breaker (`config/fallback_global.yaml`)

```yaml
circuit_breaker:
  failure_threshold: 5       # 연속 5회 실패 시 프로바이더 차단
  recovery_timeout_sec: 60   # 60초 후 재시도
  half_open_requests: 3      # 복구 테스트 요청 수

providers:
  gemini:
    max_concurrent: 30       # 동시 요청 제한
    timeout_ms: 30000
  claude:
    max_concurrent: 20
    timeout_ms: 30000
  gpt:
    max_concurrent: 30
    timeout_ms: 30000
  perplexity:
    max_concurrent: 10
    timeout_ms: 30000
  ollama:
    max_concurrent: 5
    timeout_ms: 60000
```

### 가격 테이블 (`config/pricing.yaml`)

모델별 토큰 단가를 관리한다. 가격 변동 시 이 파일만 수정하면 비용 계산에 반영된다.

| 모델 | Input ($/1M tokens) | Output ($/1M tokens) |
|------|---------------------|----------------------|
| gemini-2.5-pro | 1.25 | 10.00 |
| gemini-2.5-flash | 0.15 | 0.60 |
| claude-opus-4-6 | 15.00 | 75.00 |
| claude-sonnet-4-6 | 3.00 | 15.00 |
| claude-haiku-4-5 | 0.80 | 4.00 |
| gpt-4o | 2.50 | 10.00 |
| gpt-4o-mini | 0.15 | 0.60 |
| sonar-pro | 3.00 | 15.00 |
| ollama/* | 0 | 0 |

---

## 클라이언트 인증

ezai는 **키 쌍(client_id + secret)** 기반으로 외부 클라이언트를 인증한다.

### 인증 구조

```
┌─────────────────────────────────────────────────────────┐
│                     ezai Gateway                        │
│                                                         │
│  신뢰 네트워크 (127.0.0.0/8, 10.0.0.0/8 등)            │
│  → 인증 없이 모든 API 접근 가능                          │
│  → admin API도 접근 가능                                 │
│                                                         │
│  외부 네트워크                                           │
│  → X-Client-ID + X-Client-Secret 헤더 필수              │
│  → admin API 접근 불가 (403 Forbidden)                   │
│  → 셀프 로테이션 (/v1/keys/rotate)만 가능               │
└─────────────────────────────────────────────────────────┘
```

- **client_id**: 서비스 식별자 (비밀 아님, 로그에 자유롭게 기록)
- **secret**: `ezs_` 접두어 + 32바이트 랜덤 hex (총 68자). DB에는 SHA-256 해시만 저장되며, 발급 시 1회만 노출된다.
- **만료(TTL)**: 키 발급 시 반드시 설정. 만료된 키로는 인증 불가.

### 키 발급 (관리자)

신뢰 네트워크(내부망)에서 관리자가 서비스별로 키를 발급한다.

```bash
# 모바일 앱용 키 발급 (720시간 = 30일)
curl -X POST http://localhost:8080/admin/client-keys \
  -H "Content-Type: application/json" \
  -d '{
    "client_id": "mobile-app-prod",
    "service_name": "mobile",
    "description": "모바일 앱 프로덕션",
    "ttl_hours": 720
  }'
```

응답:

```json
{
  "client_key": {
    "id": 1,
    "client_id": "mobile-app-prod",
    "secret_prefix": "ezs_a3f1b2c4",
    "service_name": "mobile",
    "description": "모바일 앱 프로덕션",
    "is_active": true,
    "expires_at": "2026-05-17T14:30:00Z",
    "created_at": "2026-04-17T14:30:00Z",
    "updated_at": "2026-04-17T14:30:00Z"
  },
  "secret": "ezs_a3f1b2c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1",
  "warning": "이 시크릿은 다시 확인할 수 없습니다. 안전한 곳에 보관하세요."
}
```

> **중요**: `secret` 값은 이 응답에서만 확인할 수 있다. 분실 시 재발급이 필요하다.

### 외부 클라이언트 사용

발급받은 키 쌍을 헤더에 넣어 요청한다.

```bash
curl -X POST https://ezai.example.com/chat \
  -H "Content-Type: application/json" \
  -H "X-Client-ID: mobile-app-prod" \
  -H "X-Client-Secret: ezs_a3f1b2c4d5e6f7..." \
  -d '{
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "messages": [{"role": "user", "content": "안녕하세요"}]
  }'
```

인증 성공 시 모든 응답에 만료 정보 헤더가 포함된다:

```
X-Key-Expires-At: 2026-05-17T14:30:00Z
X-Key-Expires-In: 2592000
```

`X-Key-Expires-In`은 남은 시간(초)이다. 클라이언트는 이 값을 확인하여 만료 전에 로테이션할 수 있다.

### 셀프 로테이션

클라이언트가 유효한 키로 직접 시크릿을 교체한다. client_id는 유지되고 secret만 바뀐다.

```bash
curl -X POST https://ezai.example.com/v1/keys/rotate \
  -H "X-Client-ID: mobile-app-prod" \
  -H "X-Client-Secret: ezs_현재시크릿..."
```

응답:

```json
{
  "client_id": "mobile-app-prod",
  "secret": "ezs_새로운시크릿...",
  "expires_at": "2026-05-17T14:30:00Z",
  "warning": "이 시크릿은 다시 확인할 수 없습니다. 안전한 곳에 보관하세요."
}
```

> **참고**: 로테이션 시 만료일(`expires_at`)은 변경되지 않는다. 만료일 연장이 필요하면 관리자가 재발급해야 한다.

클라이언트 측 로테이션 예시 (Python):

```python
import requests

resp = requests.post("https://ezai.example.com/chat", headers={
    "X-Client-ID": CLIENT_ID,
    "X-Client-Secret": CLIENT_SECRET,
}, json={...})

# 만료 임박 시 자동 로테이션
remaining = int(resp.headers.get("X-Key-Expires-In", 99999))
if remaining < 86400:  # 1일 미만
    rotate_resp = requests.post("https://ezai.example.com/v1/keys/rotate", headers={
        "X-Client-ID": CLIENT_ID,
        "X-Client-Secret": CLIENT_SECRET,
    })
    CLIENT_SECRET = rotate_resp.json()["secret"]
```

### 키 관리 (관리자)

```bash
# 전체 키 목록 조회
curl http://localhost:8080/admin/client-keys

# 서비스별 필터링
curl "http://localhost:8080/admin/client-keys?service=mobile"

# 키 폐기 (즉시 차단)
curl -X DELETE http://localhost:8080/admin/client-keys/mobile-app-prod

# 만료/폐기된 키 재발급 (새 시크릿 + 새 만료일)
curl -X POST http://localhost:8080/admin/client-keys/mobile-app-prod/reissue \
  -H "Content-Type: application/json" \
  -d '{"ttl_hours": 720}'
```

### 키 생명주기

```
                관리자 발급
                    │
                    ▼
              ┌──────────┐
              │  활성(Active)  │◄──── 셀프 로테이션 (secret만 교체)
              └──────────┘
              │           │
         만료됨         관리자 폐기
              │           │
              ▼           ▼
         ┌─────────┐  ┌─────────┐
         │ 만료     │  │ 비활성   │
         └─────────┘  └─────────┘
              │           │
              └─────┬─────┘
                    │
               관리자 재발급
                    │
                    ▼
              ┌──────────┐
              │  활성(Active)  │
              └──────────┘
```

- **활성 → 만료**: TTL 경과 시 자동. 클라이언트는 `X-Key-Expires-In` 헤더로 잔여 시간을 확인하여 만료 전에 로테이션 가능.
- **활성 → 비활성**: 관리자가 `DELETE /admin/client-keys/:client_id`로 즉시 차단. 키 유출 시 사용.
- **만료/비활성 → 활성**: 관리자가 `POST /admin/client-keys/:client_id/reissue`로 새 시크릿과 만료일을 재설정.
- **셀프 로테이션**: 활성 상태에서만 가능. 만료일은 유지되고 secret만 교체.

### 서비스별 키 분리

각 서비스는 독립된 키를 가진다. 한 서비스의 키 변경이 다른 서비스에 영향을 주지 않는다.

```bash
# 서비스별 독립 키 발급
curl -X POST http://localhost:8080/admin/client-keys \
  -d '{"client_id": "mobile-app-prod", "service_name": "mobile", "ttl_hours": 720}'

curl -X POST http://localhost:8080/admin/client-keys \
  -d '{"client_id": "web-backend-prod", "service_name": "web", "ttl_hours": 720}'

curl -X POST http://localhost:8080/admin/client-keys \
  -d '{"client_id": "analytics-worker", "service_name": "analytics", "ttl_hours": 720}'
```

키 목록 조회 예시:

```json
{
  "client_keys": [
    {"client_id": "analytics-worker",  "secret_prefix": "ezs_c9e4...", "service_name": "analytics", "expires_at": "2026-05-17T..."},
    {"client_id": "mobile-app-prod",   "secret_prefix": "ezs_a3f1...", "service_name": "mobile",    "expires_at": "2026-05-17T..."},
    {"client_id": "web-backend-prod",  "secret_prefix": "ezs_b7d2...", "service_name": "web",       "expires_at": "2026-05-17T..."}
  ]
}
```

---

## 사용 방법

### 기본 채팅 요청

프로바이더와 모델을 지정하여 직접 요청한다.

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "messages": [
      {"role": "user", "content": "Go 언어의 장점을 3가지 알려줘"}
    ]
  }'
```

응답:

```json
{
  "id": "tr_20260417_143000_a1b2c3",
  "provider": "gemini",
  "model": "gemini-2.5-flash",
  "content": "Go 언어의 장점 3가지:\n1. ...",
  "usage": {
    "input_tokens": 15,
    "output_tokens": 120,
    "total_tokens": 135,
    "estimated_cost_usd": 0.000074
  },
  "metadata": {
    "latency_ms": 1850,
    "fallback_used": false,
    "fallback_reason": null,
    "search_sources": []
  }
}
```

### Fallback 사용

프로바이더 장애 시 자동으로 다음 모델로 전환한다.

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "messages": [
      {"role": "user", "content": "서울 날씨 알려줘"}
    ],
    "fallback": [
      {"provider": "claude", "model": "claude-sonnet-4-6"},
      {"provider": "gpt", "model": "gpt-4o"}
    ],
    "fallback_policy": "on_error"
  }'
```

Fallback 정책:

| 정책 | 동작 |
|------|------|
| `on_error` | API 에러(5xx, 네트워크 장애) 시 다음 모델로 |
| `on_timeout` | 지정 시간 초과 시 다음 모델로 |
| `on_rate_limit` | 429(Rate Limit) 시 다음 모델로 |
| `always_fastest` | 모든 모델에 동시 요청, 가장 빠른 응답 사용 |

### 프롬프트 템플릿 사용

미리 정의된 태스크 템플릿으로 프롬프트를 서버에서 조합한다.

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "project": "ecommerce",
    "task": "summarize",
    "prompt_variables": {
      "length": 200,
      "input": "오늘 애플이 신형 맥북 프로를 발표했다. M5 Pro 칩을 탑재하며..."
    },
    "messages": [
      {"role": "user", "content": "요약해주세요"}
    ]
  }'
```

서버가 다음 순서로 프롬프트를 조합한다:

```
① prompts/base.yaml                     (공통 규칙)
② prompts/projects/ecommerce.yaml       (이커머스 도메인 지식)
③ prompts/models/gemini.yaml            (Gemini 특화 지시)
④ prompts/tasks/summarize.gemini.yaml   (요약 태스크, Gemini 오버라이드)
⑤ {{length}} → 200, {{input}} → 실제 텍스트  (변수 치환)
```

조합된 프롬프트가 `system` 메시지로 messages 앞에 삽입된다.

### 스트리밍 응답

SSE(Server-Sent Events)로 실시간 응답을 받는다.

```bash
curl -N -X POST http://localhost:8080/chat/stream \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "claude",
    "model": "claude-sonnet-4-6",
    "messages": [
      {"role": "user", "content": "Go 언어로 HTTP 서버 만드는 법을 알려줘"}
    ]
  }'
```

응답 (SSE 형식):

```
data: {"content":"Go","done":false}
data: {"content":" 언어로","done":false}
data: {"content":" HTTP 서버를","done":false}
...
data: {"content":"","done":true,"usage":{"input_tokens":20,"output_tokens":350}}
```

### Google Search Grounding (Gemini)

Gemini 모델에 인터넷 검색 결과를 참조하게 한다.

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "messages": [
      {"role": "user", "content": "2026년 4월 서울 날씨는?"}
    ],
    "options": {
      "search_grounding": true
    }
  }'
```

응답의 `search_sources`에 참조한 웹 페이지 출처가 포함된다.

### 배치 요청

대량 요청을 Redis 큐에 등록하고 비동기로 처리한다.

작업 등록:

```bash
curl -X POST http://localhost:8080/batch \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "messages": [
      {"role": "user", "content": "이 리뷰를 분석해줘: ..."}
    ]
  }'
```

응답 (202 Accepted):

```json
{
  "job_id": "job_a1b2c3d4",
  "status": "pending"
}
```

결과 조회:

```bash
curl http://localhost:8080/batch/job_a1b2c3d4
```

```json
{
  "job_id": "job_a1b2c3d4",
  "status": "completed",
  "response": {
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "content": "분석 결과..."
  }
}
```

### 사용량/비용 조회

```bash
# 오늘 사용량
curl "http://localhost:8080/usage?period=daily&date=2026-04-17"

# 이번 달, 프로바이더별 집계
curl "http://localhost:8080/usage?period=monthly&month=2026-04&group_by=provider"

# 특정 프로젝트, 모델별 집계
curl "http://localhost:8080/usage?period=custom&from=2026-04-01&to=2026-04-17&project=ecommerce&group_by=model"
```

응답 예시:

```json
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
      "group_key": "gemini",
      "requests": 25000,
      "input_tokens": 7000000,
      "output_tokens": 4500000,
      "cost_usd": 38.20
    },
    {
      "group_key": "claude",
      "requests": 15000,
      "input_tokens": 4000000,
      "output_tokens": 2800000,
      "cost_usd": 84.00
    }
  ]
}
```

### 모델 목록 조회

```bash
# 전체 모델
curl http://localhost:8080/models

# 특정 프로바이더
curl "http://localhost:8080/models?provider=gemini"
```

### 가격 테이블 조회

```bash
curl http://localhost:8080/usage/pricing
```

---

## Admin API

> **접근 제한**: 모든 admin API(`/admin/*`)는 신뢰 네트워크(trusted CIDR)에서만 접근 가능하다. 외부 네트워크에서는 클라이언트 키가 있더라도 403 Forbidden이 반환된다.

### 클라이언트 키 관리

클라이언트 인증 키의 발급, 조회, 폐기, 재발급을 관리한다. 자세한 사용법은 [클라이언트 인증](#클라이언트-인증) 섹션을 참고한다.

```bash
# 키 발급
curl -X POST http://localhost:8080/admin/client-keys \
  -H "Content-Type: application/json" \
  -d '{"client_id": "my-service", "service_name": "backend", "ttl_hours": 720}'

# 키 목록 조회
curl http://localhost:8080/admin/client-keys

# 서비스별 필터링
curl "http://localhost:8080/admin/client-keys?service=backend"

# 키 폐기
curl -X DELETE http://localhost:8080/admin/client-keys/my-service

# 만료/폐기 키 재발급
curl -X POST http://localhost:8080/admin/client-keys/my-service/reissue \
  -H "Content-Type: application/json" \
  -d '{"ttl_hours": 720}'
```

### 프로바이더 API 키 관리

```bash
# 키 목록 (값은 노출되지 않음)
curl http://localhost:8080/admin/keys

# 키 등록
curl -X POST http://localhost:8080/admin/keys \
  -H "Content-Type: application/json" \
  -d '{"provider": "claude", "key_name": "backup", "key_value": "sk-ant-..."}'

# 키 로테이션 (기존 키 비활성화 + 새 키 등록)
curl -X POST http://localhost:8080/admin/keys/1/rotate \
  -H "Content-Type: application/json" \
  -d '{"key_value": "sk-ant-new-..."}'

# 키 비활성화
curl -X DELETE http://localhost:8080/admin/keys/1

# 감사 로그
curl http://localhost:8080/admin/keys/audit
```

### 요청 로그 조회

```bash
# 특정 요청 추적
curl "http://localhost:8080/admin/logs?trace_id=tr_xxxxx"

# 에러 요청만 조회
curl "http://localhost:8080/admin/logs?status=error&limit=50"

# fallback 발생 건만 조회
curl "http://localhost:8080/admin/logs?fallback_used=true"

# 특정 프로바이더, 날짜 필터
curl "http://localhost:8080/admin/logs?provider=gemini&date=2026-04-17"
```

### 비용 데이터 관리

```bash
# 현재 보존 정책 확인
curl http://localhost:8080/admin/usage/retention

# 아카이브 (일별 요약으로 집계 후 원본 삭제)
curl -X POST http://localhost:8080/admin/usage/archive \
  -H "Content-Type: application/json" \
  -d '{
    "before": "2026-01-01",
    "confirmation": "CONFIRM-ARCHIVE-2026-01-01",
    "reason": "2025년 데이터 아카이브"
  }'

# 소프트 리셋 (비용 필드만 0으로 초기화, 로그 보존)
curl -X POST http://localhost:8080/admin/usage/reset \
  -H "Content-Type: application/json" \
  -d '{
    "before": "2026-01-01",
    "confirmation": "CONFIRM-RESET-2026-01-01",
    "reason": "월별 예산 리셋"
  }'
```

하드 삭제는 기본 비활성화 상태이며, `config/usage_retention.yaml`의 `allowed_operations`에 `hard_delete`를 추가해야 사용 가능하다.

---

## CLI 커맨드

```bash
ezai                                          # 서버 시작
ezai serve                                    # 서버 시작 (동일)
ezai keys init-db                             # DB 암호화 키 생성
ezai keys add <provider> --value <key>        # API 키 등록
ezai keys add <provider> --file <path>        # 파일에서 키 등록 (Gemini 서비스 계정)
ezai keys list                                # 키 목록 조회
ezai keys delete <id>                         # 키 비활성화
ezai keys rotate <id> --value <new_key>       # 키 로테이션
```

---

## 환경변수

| 변수 | 필수 | 설명 |
|------|------|------|
| `EZAI_DB_KEY` | O | DB 암호화 키 (hex 64자, `ezai keys init-db`로 생성) |
| `EZAI_CONFIG_DIR` | | 설정 디렉토리 경로 (기본: `config`) |
| `EZAI_PROMPTS_DIR` | | 프롬프트 디렉토리 경로 (기본: `prompts`) |
| `GOOGLE_APPLICATION_CREDENTIALS` | | Gemini 서비스 계정 JSON 파일 경로 (keys.db 미사용 시) |
| `ANTHROPIC_API_KEY` | | Claude API 키 (keys.db 미사용 시) |
| `OPENAI_API_KEY` | | GPT API 키 (keys.db 미사용 시) |
| `PERPLEXITY_API_KEY` | | Perplexity API 키 (keys.db 미사용 시) |
| `EZAI_GEMINI_PROJECT` | | Gemini 프로젝트 ID (서비스 계정에서 자동 추출 가능) |

API 키는 keys.db(암호화)에 등록하는 것을 권장한다. 환경변수는 keys.db에 키가 없을 때 fallback으로 사용된다.

---

## 데이터 저장소

| 데이터 | 저장소 | 경로/위치 |
|--------|--------|----------|
| API 키, 시크릿 | SQLite + SQLCipher (암호화) | `data/keys.db` |
| 프롬프트 | YAML + Git | `prompts/` |
| 설정 (라우팅, 가격 등) | YAML + Git | `config/` |
| 요청 로그, 비용 | SQLite | `data/ezai.db` |

API 키는 절대 YAML이나 코드에 저장하지 않는다. `data/` 디렉토리는 `.gitignore`에 포함되어 있다.
