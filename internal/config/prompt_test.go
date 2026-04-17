package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptBuild(t *testing.T) {
	dir := t.TempDir()

	// base.yaml
	os.WriteFile(filepath.Join(dir, "base.yaml"), []byte(`system_prompt: "공통 프롬프트"`), 0644)

	// models/gemini.yaml
	os.MkdirAll(filepath.Join(dir, "models"), 0755)
	os.WriteFile(filepath.Join(dir, "models", "gemini.yaml"), []byte(`system_prompt: "Gemini 프롬프트"`), 0644)

	// tasks/summarize.yaml
	os.MkdirAll(filepath.Join(dir, "tasks"), 0755)
	os.WriteFile(filepath.Join(dir, "tasks", "summarize.yaml"), []byte(`task_prompt: "{{length}}자로 요약: {{input}}"`), 0644)

	pm := NewPromptManager(dir)

	// base만
	result, err := pm.Build("", "", "", nil)
	if err != nil {
		t.Fatalf("빌드 실패: %v", err)
	}
	if !strings.Contains(result, "공통 프롬프트") {
		t.Error("base 프롬프트 누락")
	}

	// base + model
	result, _ = pm.Build("gemini", "", "", nil)
	if !strings.Contains(result, "공통 프롬프트") || !strings.Contains(result, "Gemini 프롬프트") {
		t.Error("model 프롬프트 조합 실패")
	}

	// 변수 치환
	vars := map[string]any{"length": 100, "input": "테스트 텍스트"}
	result, _ = pm.Build("gemini", "", "summarize", vars)
	if !strings.Contains(result, "100자로 요약") || !strings.Contains(result, "테스트 텍스트") {
		t.Errorf("변수 치환 실패: %s", result)
	}
}
