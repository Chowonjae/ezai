package concurrency

import (
	"context"
	"fmt"
)

// Semaphore - 프로바이더별 동시 요청 제한
// 채널 기반으로 구현하여 goroutine 안전하다.
type Semaphore struct {
	ch   chan struct{}
	name string
}

// NewSemaphore - maxConcurrent 개수만큼의 세마포어 생성
func NewSemaphore(name string, maxConcurrent int) *Semaphore {
	return &Semaphore{
		ch:   make(chan struct{}, maxConcurrent),
		name: name,
	}
}

// Acquire - 세마포어 획득 (context 취소 시 에러 반환)
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("세마포어 획득 타임아웃 (%s): %w", s.name, ctx.Err())
	}
}

// Release - 세마포어 반환
func (s *Semaphore) Release() {
	<-s.ch
}

// Available - 현재 사용 가능한 슬롯 수
func (s *Semaphore) Available() int {
	return cap(s.ch) - len(s.ch)
}
