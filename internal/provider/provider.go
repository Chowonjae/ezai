package provider

import (
	"context"

	"github.com/Chowonjae/ezai/internal/model"
)

// Provider - AI 프로바이더 공통 인터페이스
// 모든 프로바이더(Gemini, Claude, GPT, Perplexity, Ollama)는 이 인터페이스를 구현한다.
type Provider interface {
	// Name - 프로바이더 식별자 반환 ("gemini", "claude", "gpt", "perplexity", "ollama")
	Name() string

	// Chat - 동기 요청/응답
	Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error)

	// ChatStream - 스트리밍 응답 (채널로 청크 전달)
	ChatStream(ctx context.Context, req *model.ChatRequest) (<-chan model.StreamChunk, error)

	// Models - 해당 프로바이더에서 사용 가능한 모델 목록
	Models() []model.ModelInfo

	// HealthCheck - 프로바이더 상태 확인
	HealthCheck(ctx context.Context) error
}
