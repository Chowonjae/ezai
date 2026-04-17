#!/bin/bash
# ezai 서버 시작 스크립트
# 사용법: ./batch/run.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
ENV_FILE="$PROJECT_DIR/.env"
BINARY="$PROJECT_DIR/bin/ezai"

# .env 파일 로드
if [ -f "$ENV_FILE" ]; then
    echo "[ezai] .env 파일 로드: $ENV_FILE"
    set -a
    source "$ENV_FILE"
    set +a
else
    echo "[ezai] 경고: .env 파일이 없습니다 ($ENV_FILE)"
    echo "[ezai] EZAI_DB_KEY 환경변수가 설정되어 있는지 확인하세요."
fi

# 필수 환경변수 확인
if [ -z "$EZAI_DB_KEY" ]; then
    echo "[ezai] 오류: EZAI_DB_KEY가 설정되지 않았습니다."
    echo ""
    echo "  .env 파일을 생성하세요:"
    echo "    echo 'EZAI_DB_KEY=$(openssl rand -hex 32)' > $ENV_FILE"
    echo ""
    exit 1
fi

# 빌드 (바이너리가 없거나 소스가 더 최신이면)
cd "$PROJECT_DIR"
if [ ! -f "$BINARY" ] || [ "$(find cmd internal -name '*.go' -newer "$BINARY" 2>/dev/null | head -1)" ]; then
    echo "[ezai] 빌드 중..."
    CGO_ENABLED=1 go build -o "$BINARY" ./cmd/ezai
    echo "[ezai] 빌드 완료: $BINARY"
fi

# 서버 시작
echo "[ezai] 서버 시작..."
exec "$BINARY"
