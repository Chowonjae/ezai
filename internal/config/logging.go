package config

import (
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoggingConfig - 로그 기록 설정
type LoggingConfig struct {
	Logging LoggingPolicy `yaml:"logging"`
}

// LoggingPolicy - 로그 기록 정책 상세
type LoggingPolicy struct {
	Record  LoggingRecord  `yaml:"record"`
	Privacy LoggingPrivacy `yaml:"privacy"`
}

// LoggingRecord - 기록 범위 설정
type LoggingRecord struct {
	InputPreview       bool `yaml:"input_preview"`
	InputPreviewLength int  `yaml:"input_preview_length"`
	OutputPreview      bool `yaml:"output_preview"`
	OutputPreviewLength int `yaml:"output_preview_length"`
	FullPrompt         bool `yaml:"full_prompt"`
	FullResponse       bool `yaml:"full_response"`
}

// LoggingPrivacy - 민감정보 처리 설정
type LoggingPrivacy struct {
	HashPrompts bool `yaml:"hash_prompts"`
	MaskAPIKeys bool `yaml:"mask_api_keys"`
	MaskPII     bool `yaml:"mask_pii"`
}

// LoadLoggingConfig - logging.yaml 로드 (선택적, 없으면 기본값 사용)
func LoadLoggingConfig(configDir string) *LoggingConfig {
	path := filepath.Join(configDir, "logging.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		// 파일이 없으면 기본 설정 반환
		return defaultLoggingConfig()
	}

	cfg := &LoggingConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		log.Printf("[WARN] logging.yaml 파싱 실패, 기본값 사용: %v", err)
		return defaultLoggingConfig()
	}

	applyLoggingDefaults(cfg)
	return cfg
}

// defaultLoggingConfig - 기본 로깅 설정
func defaultLoggingConfig() *LoggingConfig {
	return &LoggingConfig{
		Logging: LoggingPolicy{
			Record: LoggingRecord{
				InputPreview:        true,
				InputPreviewLength:  200,
				OutputPreview:       true,
				OutputPreviewLength: 200,
				FullPrompt:          false,
				FullResponse:        false,
			},
			Privacy: LoggingPrivacy{
				HashPrompts: true,
				MaskAPIKeys: true,
				MaskPII:     false,
			},
		},
	}
}

// applyLoggingDefaults - 기본값 적용
// 주의: 음수(-1)는 "미리보기 비활성화"로 처리, 0은 기본값 200 적용
func applyLoggingDefaults(cfg *LoggingConfig) {
	if cfg.Logging.Record.InputPreviewLength < 0 {
		cfg.Logging.Record.InputPreviewLength = 0 // 비활성화
	} else if cfg.Logging.Record.InputPreviewLength == 0 {
		cfg.Logging.Record.InputPreviewLength = 200
	}
	if cfg.Logging.Record.OutputPreviewLength < 0 {
		cfg.Logging.Record.OutputPreviewLength = 0 // 비활성화
	} else if cfg.Logging.Record.OutputPreviewLength == 0 {
		cfg.Logging.Record.OutputPreviewLength = 200
	}
}
