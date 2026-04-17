package model

// ModelInfo - 사용 가능한 모델 정보
type ModelInfo struct {
	Provider    string `json:"provider"`     // 프로바이더명
	Model       string `json:"model"`        // 모델 ID
	DisplayName string `json:"display_name"` // 표시 이름
	Available   bool   `json:"available"`    // 현재 사용 가능 여부
}
