.PHONY: build run clean tidy

# 빌드
build:
	go build -o bin/ezai ./cmd/ezai

# 실행
run:
	go run ./cmd/ezai

# 의존성 정리
tidy:
	go mod tidy

# 빌드 결과물 삭제
clean:
	rm -rf bin/
