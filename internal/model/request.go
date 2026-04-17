package model

// ChatRequest - 통합 요청 구조체
// 모든 프로바이더에 대한 요청을 이 구조체로 통일한다.
type ChatRequest struct {
	Provider       string           `json:"provider" binding:"required"`          // 프로바이더명 (gemini, claude, gpt, perplexity, ollama)
	Model          string           `json:"model" binding:"required"`             // 모델명
	Messages       []Message        `json:"messages" binding:"required,min=1"`    // 메시지 목록
	Options        ChatOptions      `json:"options,omitempty"`                    // 모델 파라미터
	Fallback       []FallbackTarget `json:"fallback,omitempty"`                   // fallback 체인
	FallbackPolicy string           `json:"fallback_policy,omitempty"`            // on_error|on_timeout|on_rate_limit|always_fastest
	Project        string           `json:"project,omitempty"`                    // 프로젝트명 (프롬프트 조합용)
	Task           string           `json:"task,omitempty"`                       // 태스크명 (프롬프트 조합용)
	PromptVars     map[string]any   `json:"prompt_variables,omitempty"`           // 프롬프트 변수 치환
	Metadata       RequestMetadata  `json:"metadata,omitempty"`                   // 클라이언트 메타데이터
}

// Message - 대화 메시지
type Message struct {
	Role    string `json:"role" binding:"required"`    // system|user|assistant
	Content string `json:"content" binding:"required"` // 메시지 내용
}

// ChatOptions - 모델 파라미터
type ChatOptions struct {
	Temperature     *float64 `json:"temperature,omitempty"`      // 생성 다양성 (0.0~2.0)
	MaxTokens       *int     `json:"max_tokens,omitempty"`       // 최대 출력 토큰 수
	TopP            *float64 `json:"top_p,omitempty"`            // 누적 확률 샘플링
	Stream          bool     `json:"stream,omitempty"`           // 스트리밍 여부
	SearchGrounding bool     `json:"search_grounding,omitempty"` // Google Search Grounding (Gemini 전용)
}

// FallbackTarget - fallback 대상
type FallbackTarget struct {
	Provider string `json:"provider"` // 프로바이더명
	Model    string `json:"model"`    // 모델명
}

// RequestMetadata - 요청 메타데이터
type RequestMetadata struct {
	ClientID  string `json:"client_id"`            // 호출 서비스 ID
	RequestID string `json:"request_id,omitempty"` // 클라이언트 지정 요청 ID
}
