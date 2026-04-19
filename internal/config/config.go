package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config - 전체 설정 구조체
type Config struct {
	Server    ServerConfig              `yaml:"server"`
	Redis     RedisConfig               `yaml:"redis"`
	Database  DatabaseConfig            `yaml:"database"`
	Auth      AuthConfig                `yaml:"auth"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Fallback  *FallbackGlobalConfig     // fallback_global.yaml에서 별도 로드
}

// FallbackGlobalConfig - 글로벌 fallback + Circuit Breaker 설정
type FallbackGlobalConfig struct {
	CircuitBreaker CircuitBreakerConfig              `yaml:"circuit_breaker"`
	Providers      map[string]ProviderFallbackConfig `yaml:"providers"`
}

// CircuitBreakerConfig - Circuit Breaker 설정
type CircuitBreakerConfig struct {
	FailureThreshold   int `yaml:"failure_threshold"`
	RecoveryTimeoutSec int `yaml:"recovery_timeout_sec"`
	HalfOpenRequests   int `yaml:"half_open_requests"`
}

// ProviderFallbackConfig - 프로바이더별 동시성/타임아웃 설정
type ProviderFallbackConfig struct {
	MaxConcurrent int `yaml:"max_concurrent"`
	TimeoutMs     int `yaml:"timeout_ms"`
}

// ProjectFallbackConfig - 프로젝트별 fallback 설정
type ProjectFallbackConfig struct {
	Fallback struct {
		DefaultChain []FallbackChainEntry `yaml:"default_chain"`
		Policy       string               `yaml:"policy"`
		TimeoutMs    int                  `yaml:"timeout_ms"`
		MaxRetries   int                  `yaml:"max_retries"`
	} `yaml:"fallback"`
}

// FallbackChainEntry - fallback 체인 항목
type FallbackChainEntry struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// ServerConfig - HTTP 서버 설정
type ServerConfig struct {
	Host                  string `yaml:"host"`
	Port                  int    `yaml:"port"`
	ReadTimeoutSec        int    `yaml:"read_timeout_sec"`
	WriteTimeoutSec       int    `yaml:"write_timeout_sec"`
	StreamWriteTimeoutSec int    `yaml:"stream_write_timeout_sec"` // SSE 스트리밍 전용 (기본 600초)
	ShutdownTimeoutSec    int    `yaml:"shutdown_timeout_sec"`
}

// RedisConfig - Redis 연결 설정
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	PoolSize int    `yaml:"pool_size"`
}

// DatabaseConfig - 데이터베이스 파일 경로
type DatabaseConfig struct {
	LogsPath string `yaml:"logs_path"` // request_logs용 SQLite
	KeysPath string `yaml:"keys_path"` // provider_keys용 SQLCipher
}

// AuthConfig - 인증 설정
type AuthConfig struct {
	TrustedCIDRs []string `yaml:"trusted_cidrs"` // 신뢰 네트워크 CIDR 목록
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"` // 분당 요청 제한 (기본값 120)
}

// ProviderConfig - 프로바이더별 설정
type ProviderConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Location string `yaml:"location,omitempty"` // Gemini: Vertex AI 리전
	BaseURL  string `yaml:"base_url,omitempty"` // Perplexity, Ollama: 커스텀 엔드포인트
}

// Addr - 서버 바인드 주소 반환 (host:port)
func (s ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// Load - YAML 설정 파일 로드
func Load(configDir string) (*Config, error) {
	path := filepath.Join(configDir, "server.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일 읽기 실패: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("설정 파일 파싱 실패: %w", err)
	}

	// 기본값 설정
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.ReadTimeoutSec == 0 {
		cfg.Server.ReadTimeoutSec = 30
	}
	if cfg.Server.WriteTimeoutSec == 0 {
		cfg.Server.WriteTimeoutSec = 120
	}
	if cfg.Server.StreamWriteTimeoutSec == 0 {
		cfg.Server.StreamWriteTimeoutSec = 600 // SSE 스트리밍: 기본 10분
	}
	if cfg.Server.ShutdownTimeoutSec == 0 {
		cfg.Server.ShutdownTimeoutSec = 30
	}
	if cfg.Auth.RateLimitPerMinute == 0 {
		cfg.Auth.RateLimitPerMinute = 120
	}

	// fallback_global.yaml 로드 (선택적)
	fallbackPath := filepath.Join(configDir, "fallback_global.yaml")
	if fbData, err := os.ReadFile(fallbackPath); err == nil {
		fbCfg := &FallbackGlobalConfig{}
		if err := yaml.Unmarshal(fbData, fbCfg); err != nil {
			return nil, fmt.Errorf("fallback 설정 파싱 실패: %w", err)
		}
		cfg.Fallback = fbCfg
	}

	return cfg, nil
}

// LoadProjectFallback - 프로젝트별 fallback 설정 로드
func LoadProjectFallback(configDir, project string) (*ProjectFallbackConfig, error) {
	path := filepath.Join(configDir, "projects", project+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pCfg := &ProjectFallbackConfig{}
	if err := yaml.Unmarshal(data, pCfg); err != nil {
		return nil, fmt.Errorf("프로젝트 fallback 설정 파싱 실패: %w", err)
	}
	return pCfg, nil
}
