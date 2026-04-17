package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPricingCalculate(t *testing.T) {
	// 테스트용 pricing.yaml 생성
	dir := t.TempDir()
	yaml := `pricing:
  gemini-2.5-flash:
    input_per_1m_tokens: 0.15
    output_per_1m_tokens: 0.60
  gpt-4o-mini:
    input_per_1m_tokens: 0.15
    output_per_1m_tokens: 0.60
  "ollama/*":
    input_per_1m_tokens: 0
    output_per_1m_tokens: 0
`
	os.WriteFile(filepath.Join(dir, "pricing.yaml"), []byte(yaml), 0644)

	pm, err := NewPricingManager(dir)
	if err != nil {
		t.Fatalf("PricingManager 생성 실패: %v", err)
	}

	// 정상 계산
	cost := pm.Calculate("gemini-2.5-flash", 1000, 500)
	expected := (1000.0/1_000_000)*0.15 + (500.0/1_000_000)*0.60
	if cost != expected {
		t.Errorf("비용 계산 불일치: got %f, want %f", cost, expected)
	}

	// 없는 모델 → 0
	cost = pm.Calculate("nonexistent-model", 1000, 500)
	if cost != 0 {
		t.Errorf("없는 모델 비용: got %f, want 0", cost)
	}

	// 와일드카드 매칭 (ollama/*)
	cost = pm.Calculate("ollama/llama3.1", 1000, 500)
	if cost != 0 {
		t.Errorf("Ollama 비용: got %f, want 0", cost)
	}
}
