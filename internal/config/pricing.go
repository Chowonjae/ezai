package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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
		// 가장 긴 매칭을 선택하여 결정적 결과를 보장한다.
		bestLen := 0
		for name, p := range pm.pricing {
			if strings.HasPrefix(modelName, name) && len(name) > bestLen {
				pricing = p
				bestLen = len(name)
				ok = true
			}
		}
	}
	if !ok {
		// 와일드카드 매칭 (예: ollama/*) - 가장 긴 접두사 매칭 선택
		bestLen := 0
		for pattern, p := range pm.pricing {
			if strings.HasSuffix(pattern, "/*") {
				prefix := strings.TrimSuffix(pattern, "/*")
				if strings.HasPrefix(modelName, prefix) && len(prefix) > bestLen {
					pricing = p
					bestLen = len(prefix)
					ok = true
				}
			}
		}
		if !ok {
			log.Printf("[WARN] 모델 '%s'의 가격 정보 미등록, 비용 0으로 계산", modelName)
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

// AllPricing - 전체 가격 테이블 반환 (복사본)
// 내부 맵을 직접 노출하지 않아 외부에서 수정해도 원본이 보호된다.
func (pm *PricingManager) AllPricing() map[string]ModelPricing {
	cp := make(map[string]ModelPricing, len(pm.pricing))
	for k, v := range pm.pricing {
		cp[k] = v
	}
	return cp
}

// ModelNames - 가격 테이블의 모델명 목록 (정렬됨)
func (pm *PricingManager) ModelNames() []string {
	names := make([]string, 0, len(pm.pricing))
	for k := range pm.pricing {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
