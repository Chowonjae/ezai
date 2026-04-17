package model

// ChatResponse - 통합 응답 구조체
type ChatResponse struct {
	ID       string           `json:"id"`       // 요청 고유 ID
	Provider string           `json:"provider"` // 실제 응답한 프로바이더
	Model    string           `json:"model"`    // 실제 사용된 모델
	Content  string           `json:"content"`  // 응답 텍스트
	Usage    UsageInfo        `json:"usage"`    // 토큰 사용량 및 비용
	Metadata ResponseMetadata `json:"metadata"` // 응답 메타데이터
}

// UsageInfo - 토큰 사용량 및 비용 정보
type UsageInfo struct {
	InputTokens   int     `json:"input_tokens"`        // 입력 토큰 수
	OutputTokens  int     `json:"output_tokens"`       // 출력 토큰 수
	TotalTokens   int     `json:"total_tokens"`        // 총 토큰 수
	EstimatedCost float64 `json:"estimated_cost_usd"`  // 예상 비용 (USD)
}

// ResponseMetadata - 응답 메타데이터
type ResponseMetadata struct {
	LatencyMs      int64          `json:"latency_ms"`                // 전체 응답 시간 (ms)
	FallbackUsed   bool           `json:"fallback_used"`             // fallback 사용 여부
	FallbackReason *string        `json:"fallback_reason,omitempty"` // fallback 사유
	SearchSources  []SearchSource `json:"search_sources,omitempty"`  // 검색 소스 (Gemini Search Grounding)
}

// SearchSource - Google Search Grounding 검색 소스
type SearchSource struct {
	Title string `json:"title"` // 소스 제목
	URI   string `json:"uri"`   // 소스 URL
}

// StreamChunk - SSE 스트리밍 청크
type StreamChunk struct {
	Content string     `json:"content,omitempty"` // 텍스트 조각
	Done    bool       `json:"done"`              // 스트림 종료 여부
	Error   *string    `json:"error,omitempty"`   // 에러 발생 시
	Usage   *UsageInfo `json:"usage,omitempty"`   // 최종 청크에만 포함
}
