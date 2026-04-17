package concurrency

import (
	"context"
	"testing"
	"time"
)

func TestSemaphoreAcquireRelease(t *testing.T) {
	sem := NewSemaphore("test", 2)

	if sem.Available() != 2 {
		t.Errorf("초기 가용: got %d, want 2", sem.Available())
	}

	ctx := context.Background()
	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("1차 획득 실패: %v", err)
	}
	if sem.Available() != 1 {
		t.Errorf("1차 후 가용: got %d, want 1", sem.Available())
	}

	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("2차 획득 실패: %v", err)
	}
	if sem.Available() != 0 {
		t.Errorf("2차 후 가용: got %d, want 0", sem.Available())
	}

	sem.Release()
	if sem.Available() != 1 {
		t.Errorf("해제 후 가용: got %d, want 1", sem.Available())
	}
}

func TestSemaphoreTimeout(t *testing.T) {
	sem := NewSemaphore("test", 1)
	ctx := context.Background()

	// 슬롯 1개 점유
	sem.Acquire(ctx)

	// 타임아웃 컨텍스트로 획득 시도
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	err := sem.Acquire(timeoutCtx)
	if err == nil {
		t.Error("가득 찬 세마포어에서 타임아웃 에러가 발생하지 않음")
	}
}
