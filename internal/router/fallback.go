package router

import (
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
// HTTP 상태코드 텍스트로 매칭하여 숫자만으로 인한 오탐(토큰 수, 요청 ID 등)을 방지한다.
func isServerError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "internal server error") ||
		strings.Contains(lower, "bad gateway") ||
		strings.Contains(lower, "service unavailable") ||
		strings.Contains(lower, "gateway timeout") ||
		containsStatusCode(errMsg, "500") ||
		containsStatusCode(errMsg, "502") ||
		containsStatusCode(errMsg, "503") ||
		containsStatusCode(errMsg, "504")
}

// containsStatusCode - "status: CODE" 또는 ": CODE" 패턴으로 HTTP 상태코드를 매칭
// 단순 숫자 서브스트링 매칭의 오탐을 방지한다.
func containsStatusCode(errMsg, code string) bool {
	patterns := []string{
		"status: " + code,
		"status:" + code,
		"status code: " + code,
		"status_code: " + code,
		code + " ",
	}
	for _, p := range patterns {
		if strings.Contains(errMsg, p) {
			return true
		}
	}
	return false
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
// 주의: "context canceled"는 클라이언트 연결 끊김이므로 타임아웃이 아님 (fallback 불필요)
func isTimeoutError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "deadline exceeded")
}

// isRateLimitError - 429 Rate Limit 에러 여부
func isRateLimitError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return containsStatusCode(errMsg, "429") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many requests")
}
