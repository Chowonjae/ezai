package router

import (
	"sync"
	"time"
)

// CircuitState - Circuit Breaker 상태
type CircuitState int

const (
	StateClosed   CircuitState = iota // 정상: 요청 통과
	StateOpen                         // 차단: 요청 즉시 실패
	StateHalfOpen                     // 복구 테스트: 제한적 요청 허용
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker - 프로바이더별 Circuit Breaker
// 상태 전이: Closed → Open(연속 실패) → Half-Open(시간 경과) → Closed(성공)
type CircuitBreaker struct {
	mu               sync.RWMutex
	state            CircuitState
	failureCount     int           // 연속 실패 횟수
	successCount     int           // Half-Open 상태에서 성공 횟수
	failureThreshold int           // Closed→Open 전환 기준
	recoveryTimeout  time.Duration // Open→Half-Open 대기 시간
	halfOpenMax      int           // Half-Open에서 Closed 전환 기준 (성공 횟수)
	lastFailureTime  time.Time     // 마지막 실패 시각
	providerName     string
}

// NewCircuitBreaker - Circuit Breaker 생성
func NewCircuitBreaker(providerName string, failureThreshold int, recoveryTimeoutSec int, halfOpenMax int) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		recoveryTimeout:  time.Duration(recoveryTimeoutSec) * time.Second,
		halfOpenMax:      halfOpenMax,
		providerName:     providerName,
	}
}

// Allow - 요청 허용 여부 판정
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// 복구 타임아웃 경과 시 Half-Open으로 전환
		if time.Since(cb.lastFailureTime) >= cb.recoveryTimeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// RecordSuccess - 성공 기록
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failureCount = 0
	case StateHalfOpen:
		cb.successCount++
		// Half-Open에서 충분한 성공 시 Closed로 복구
		if cb.successCount >= cb.halfOpenMax {
			cb.state = StateClosed
			cb.failureCount = 0
			cb.successCount = 0
		}
	}
}

// RecordFailure - 실패 기록
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
		}
	case StateHalfOpen:
		// Half-Open에서 실패하면 다시 Open
		cb.state = StateOpen
		cb.successCount = 0
	}
}

// State - 현재 상태 조회
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats - 디버깅용 상태 정보
func (cb *CircuitBreaker) Stats() map[string]any {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return map[string]any{
		"provider":      cb.providerName,
		"state":         cb.state.String(),
		"failure_count": cb.failureCount,
		"success_count": cb.successCount,
	}
}
