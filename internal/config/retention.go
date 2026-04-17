package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RetentionConfig - 로그/비용 보존 정책 설정
type RetentionConfig struct {
	Retention RetentionPolicy `yaml:"retention"`
}

// RetentionPolicy - 보존 정책 상세
type RetentionPolicy struct {
	DetailLogs DetailLogsRetention `yaml:"detail_logs"`
	Aggregated AggregatedRetention `yaml:"aggregated"`
	Reset      ResetPolicy         `yaml:"reset"`
}

// DetailLogsRetention - 상세 로그 보존 기간
type DetailLogsRetention struct {
	HotStorageDays  int `yaml:"hot_storage_days"`
	ArchiveAfterDays int `yaml:"archive_after_days"`
	DeleteAfterDays  int `yaml:"delete_after_days"`
}

// AggregatedRetention - 집계 데이터 보존 기간
type AggregatedRetention struct {
	DailyKeepDays    int `yaml:"daily_keep_days"`
	MonthlyKeepYears int `yaml:"monthly_keep_years"`
	YearlyKeepYears  int `yaml:"yearly_keep_years"`
}

// ResetPolicy - 초기화 API 접근 제어
type ResetPolicy struct {
	RequireConfirmation bool     `yaml:"require_confirmation"`
	AllowedOperations   []string `yaml:"allowed_operations"`
}

// IsOperationAllowed - 지정 작업이 허용되었는지 확인
func (r *ResetPolicy) IsOperationAllowed(op string) bool {
	for _, allowed := range r.AllowedOperations {
		if allowed == op {
			return true
		}
	}
	return false
}

// LoadRetentionConfig - usage_retention.yaml 로드
func LoadRetentionConfig(configDir string) (*RetentionConfig, error) {
	path := filepath.Join(configDir, "usage_retention.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("보존 정책 설정 파일 읽기 실패: %w", err)
	}

	cfg := &RetentionConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("보존 정책 설정 파싱 실패: %w", err)
	}

	// 기본값 설정
	if cfg.Retention.DetailLogs.HotStorageDays == 0 {
		cfg.Retention.DetailLogs.HotStorageDays = 90
	}
	if cfg.Retention.DetailLogs.ArchiveAfterDays == 0 {
		cfg.Retention.DetailLogs.ArchiveAfterDays = 90
	}

	return cfg, nil
}
