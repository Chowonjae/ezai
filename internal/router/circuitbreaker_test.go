package router

import (
	"testing"
	"time"
)

func TestCircuitBreakerClosed(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, 1, 2)

	// 초기 상태: Closed
	if !cb.Allow() {
		t.Error("Closed 상태에서 Allow()=false")
	}

	// 성공 기록 → 여전히 Closed
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Error("성공 후 Closed가 아님")
	}
}

func TestCircuitBreakerOpenOnFailure(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, 1, 2)

	// 3회 연속 실패 → Open
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("3회 실패 후 상태: got %v, want Open", cb.State())
	}

	if cb.Allow() {
		t.Error("Open 상태에서 Allow()=true")
	}
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 1, 2) // recoveryTimeout=1초

	// Open으로 전환
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatal("Open이 아님")
	}

	// 1초 대기 → Half-Open
	time.Sleep(1100 * time.Millisecond)
	if !cb.Allow() {
		t.Error("recoveryTimeout 후 Allow()=false")
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("상태: got %v, want Half-Open", cb.State())
	}
}

func TestCircuitBreakerRecovery(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 1, 2) // halfOpenMax=2

	// Open
	cb.RecordFailure()

	// Half-Open
	time.Sleep(1100 * time.Millisecond)
	cb.Allow()

	// 2회 성공 → Closed
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Errorf("복구 후 상태: got %v, want Closed", cb.State())
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 1, 2)

	// Open
	cb.RecordFailure()

	// Half-Open
	time.Sleep(1100 * time.Millisecond)
	cb.Allow()

	// Half-Open에서 실패 → 다시 Open
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Errorf("Half-Open 실패 후 상태: got %v, want Open", cb.State())
	}
}
