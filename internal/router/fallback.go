package router

import (
	"net/http"
	"strings"
)

// FallbackPolicy - fallback 정책 종류
const (
	PolicyOnError     = "on_error"      // 5xx, 네트워크 에러 시 다음 모델로
	PolicyOnTimeout   = "on_timeout"    // 타임아웃 시 다음 모델로
	PolicyOnRateLimit = "on_rate_limit" // 429 시 다음 모델로
	PolicyFastest     = "always_fastest" // 모든 모델에 동시 요청
)

// FallbackAttempt - fallback 시도 이력
type FallbackAttempt struct {
	Order     int    `json:"order"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Status    string `json:"status"`    // success, error, timeout, rate_limited, circuit_open
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
}

// shouldFallback - 에러 유형에 따라 fallback 여부를 판정
func shouldFallback(policy string, err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	switch policy {
	case PolicyOnError:
		// 5xx 또는 네트워크 에러 시 fallback
		return isServerError(errMsg) || isNetworkError(errMsg)
	case PolicyOnTimeout:
		// 타임아웃 에러 시 fallback
		return isTimeoutError(errMsg)
	case PolicyOnRateLimit:
		// 429 Rate Limit 에러 시 fallback
		return isRateLimitError(errMsg)
	case PolicyFastest:
		// always_fastest는 별도 로직 (동시 요청)
		return false
	default:
		// 기본: on_error와 동일
		return isServerError(errMsg) || isNetworkError(errMsg)
	}
}

// isServerError - 5xx 서버 에러 여부
func isServerError(errMsg string) bool {
	serverCodes := []string{"500", "502", "503", "504"}
	for _, code := range serverCodes {
		if strings.Contains(errMsg, code) {
			return true
		}
	}
	return strings.Contains(errMsg, http.StatusText(http.StatusInternalServerError)) ||
		strings.Contains(errMsg, "Service Unavailable")
}

// isNetworkError - 네트워크 에러 여부
func isNetworkError(errMsg string) bool {
	networkPatterns := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"network is unreachable",
		"dial tcp",
		"EOF",
	}
	lower := strings.ToLower(errMsg)
	for _, p := range networkPatterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// isTimeoutError - 타임아웃 에러 여부
func isTimeoutError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "context canceled")
}

// isRateLimitError - 429 Rate Limit 에러 여부
func isRateLimitError(errMsg string) bool {
	return strings.Contains(errMsg, "429") ||
		strings.Contains(strings.ToLower(errMsg), "rate limit") ||
		strings.Contains(strings.ToLower(errMsg), "too many requests")
}
