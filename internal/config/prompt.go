package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// PromptConfig - 프롬프트 YAML 구조체
type PromptConfig struct {
	SystemPrompt string                 `yaml:"system_prompt"`
	TaskPrompt   string                 `yaml:"task_prompt"`
	Defaults     map[string]any         `yaml:"defaults"`
}

const maxPromptCacheSize = 256 // 프롬프트 캐시 최대 항목 수

// PromptManager - 프롬프트 계층 조합 관리
// 조합 우선순위: base + project + model(선택) + task
// 모델 특화 오버라이드: tasks/{task}.{model}.yaml > tasks/{task}.yaml
type PromptManager struct {
	promptsDir string
	mu         sync.RWMutex
	cache      map[string]*PromptConfig // 파일 경로 → 파싱 결과 캐시
}

// NewPromptManager - 프롬프트 매니저 생성
func NewPromptManager(promptsDir string) *PromptManager {
	return &PromptManager{
		promptsDir: promptsDir,
		cache:      make(map[string]*PromptConfig),
	}
}

// Build - 프롬프트 조합
// provider, project, task에 따라 계층적으로 프롬프트를 조합한다.
// prompt_variables로 변수 치환을 수행한다.
func (pm *PromptManager) Build(provider, project, task string, variables map[string]any) (string, error) {
	// 경로 순회 방지: project/task/provider에 ".."이나 절대경로가 포함되면 거부
	for _, name := range []string{project, task, provider} {
		if strings.Contains(name, "..") || filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
			return "", fmt.Errorf("잘못된 이름: %q (경로 구분자 사용 불가)", name)
		}
	}

	var parts []string

	// 1. base.yaml (공통)
	if base, err := pm.load("base.yaml"); err == nil && base.SystemPrompt != "" {
		parts = append(parts, base.SystemPrompt)
	}

	// 2. projects/{project}.yaml (프로젝트별)
	if project != "" {
		if proj, err := pm.load(filepath.Join("projects", project+".yaml")); err == nil && proj.SystemPrompt != "" {
			parts = append(parts, proj.SystemPrompt)
		}
	}

	// 3. models/{provider}.yaml (모델별)
	if provider != "" {
		if mdl, err := pm.load(filepath.Join("models", provider+".yaml")); err == nil && mdl.SystemPrompt != "" {
			parts = append(parts, mdl.SystemPrompt)
		}
	}

	// 4. tasks/{task}.yaml 또는 tasks/{task}.{provider}.yaml (태스크별)
	if task != "" {
		taskPrompt := ""
		// 모델 특화 오버라이드 우선
		if provider != "" {
			if override, err := pm.load(filepath.Join("tasks", task+"."+provider+".yaml")); err == nil {
				taskPrompt = override.TaskPrompt
				if taskPrompt == "" {
					taskPrompt = override.SystemPrompt
				}
			}
		}
		// 기본 태스크 프롬프트
		if taskPrompt == "" {
			if base, err := pm.load(filepath.Join("tasks", task+".yaml")); err == nil {
				taskPrompt = base.TaskPrompt
				if taskPrompt == "" {
					taskPrompt = base.SystemPrompt
				}
			}
		}
		if taskPrompt != "" {
			parts = append(parts, taskPrompt)
		}
	}

	result := strings.Join(parts, "\n")

	// 변수 치환: {{variable_name}} → 값
	if len(variables) > 0 {
		result = substituteVariables(result, variables)
	}

	return strings.TrimSpace(result), nil
}

// load - YAML 프롬프트 파일 로드 (캐시 적용, 동시성 안전)
func (pm *PromptManager) load(relativePath string) (*PromptConfig, error) {
	pm.mu.RLock()
	if cached, ok := pm.cache[relativePath]; ok {
		pm.mu.RUnlock()
		return cached, nil
	}
	pm.mu.RUnlock()

	fullPath := filepath.Join(pm.promptsDir, relativePath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}

	cfg := &PromptConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("프롬프트 파싱 실패 (%s): %w", relativePath, err)
	}

	pm.mu.Lock()
	// 캐시 크기 제한 초과 시 절반 삭제 (랜덤 eviction)
	if len(pm.cache) >= maxPromptCacheSize {
		half := len(pm.cache) / 2
		for k := range pm.cache {
			if half <= 0 {
				break
			}
			delete(pm.cache, k)
			half--
		}
	}
	pm.cache[relativePath] = cfg
	pm.mu.Unlock()
	return cfg, nil
}

// substituteVariables - {{variable}} 패턴을 값으로 치환
func substituteVariables(template string, variables map[string]any) string {
	result := template
	for key, value := range variables {
		placeholder := "{{" + key + "}}"
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", value))
	}
	return result
}
