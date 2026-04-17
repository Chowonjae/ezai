package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ModelPricing - 모델별 가격 정보
type ModelPricing struct {
	InputPer1MTokens  float64 `yaml:"input_per_1m_tokens" json:"input_per_1m_tokens"`
	OutputPer1MTokens float64 `yaml:"output_per_1m_tokens" json:"output_per_1m_tokens"`
	Currency          string  `yaml:"currency" json:"currency"`
	UpdatedAt         string  `yaml:"updated_at" json:"updated_at"`
}

// PricingConfig - 전체 가격 테이블
type PricingConfig struct {
	Pricing map[string]ModelPricing `yaml:"pricing"`
}

// PricingManager - 가격 테이블 관리 및 비용 계산
type PricingManager struct {
	pricing map[string]ModelPricing
}

// NewPricingManager - 가격 테이블 로드
func NewPricingManager(configDir string) (*PricingManager, error) {
	path := filepath.Join(configDir, "pricing.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("가격 테이블 읽기 실패: %w", err)
	}

	var cfg PricingConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("가격 테이블 파싱 실패: %w", err)
	}

	return &PricingManager{pricing: cfg.Pricing}, nil
}

// Calculate - 토큰 수 기반 비용 계산 (USD)
func (pm *PricingManager) Calculate(modelName string, inputTokens, outputTokens int) float64 {
	pricing, ok := pm.pricing[modelName]
	if !ok {
		// 접두사 매칭 (예: gpt-4o-mini-2024-07-18 → gpt-4o-mini)
		for name, p := range pm.pricing {
			if strings.HasPrefix(modelName, name) {
				pricing = p
				ok = true
				break
			}
		}
	}
	if !ok {
		// 와일드카드 매칭 (예: ollama/*)
		for pattern, p := range pm.pricing {
			if strings.HasSuffix(pattern, "/*") {
				prefix := strings.TrimSuffix(pattern, "/*")
				if strings.HasPrefix(modelName, prefix) {
					pricing = p
					ok = true
					break
				}
			}
		}
		if !ok {
			return 0
		}
	}

	inputCost := float64(inputTokens) / 1_000_000 * pricing.InputPer1MTokens
	outputCost := float64(outputTokens) / 1_000_000 * pricing.OutputPer1MTokens
	return inputCost + outputCost
}

// GetPricing - 특정 모델의 가격 정보 조회
func (pm *PricingManager) GetPricing(modelName string) (ModelPricing, bool) {
	p, ok := pm.pricing[modelName]
	return p, ok
}

// AllPricing - 전체 가격 테이블 반환
func (pm *PricingManager) AllPricing() map[string]ModelPricing {
	return pm.pricing
}
