package router

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/concurrency"
	"github.com/Chowonjae/ezai/internal/config"
	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
)

// Router - 요청 라우팅 + fallback 실행 엔진
type Router struct {
	registry   *provider.Registry
	fbCfg      *config.FallbackGlobalConfig
	breakers   map[string]*CircuitBreaker
	semaphores map[string]*concurrency.Semaphore
	logger     *zap.Logger
}

// NewRouter - 라우터 생성
func NewRouter(registry *provider.Registry, fbCfg *config.FallbackGlobalConfig, logger *zap.Logger) *Router {
	breakers := make(map[string]*CircuitBreaker)
	semaphores := make(map[string]*concurrency.Semaphore)

	if fbCfg != nil {
		for name, pCfg := range fbCfg.Providers {
			breakers[name] = NewCircuitBreaker(
				name,
				fbCfg.CircuitBreaker.FailureThreshold,
				fbCfg.CircuitBreaker.RecoveryTimeoutSec,
				fbCfg.CircuitBreaker.HalfOpenRequests,
			)
			semaphores[name] = concurrency.NewSemaphore(name, pCfg.MaxConcurrent)
		}
	}

	return &Router{
		registry:   registry,
		fbCfg:      fbCfg,
		breakers:   breakers,
		semaphores: semaphores,
		logger:     logger,
	}
}

// Execute - 요청 실행 (fallback 포함)
// 반환: 응답, fallback 시도 이력, 에러
func (r *Router) Execute(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, []FallbackAttempt, error) {
	// fallback 체인 구성: primary + fallback 대상들
	targets := r.buildTargets(req)
	policy := req.FallbackPolicy
	if policy == "" {
		policy = PolicyOnError
	}

	// always_fastest: 모든 대상에 동시 요청
	if policy == PolicyFastest {
		return r.executeFastest(ctx, req, targets)
	}

	// 순차 fallback
	return r.executeSequential(ctx, req, targets, policy)
}

// executeSequential - 순차 fallback 실행
func (r *Router) executeSequential(ctx context.Context, req *model.ChatRequest, targets []model.FallbackTarget, policy string) (*model.ChatResponse, []FallbackAttempt, error) {
	var attempts []FallbackAttempt
	var lastErr error

	for i, target := range targets {
		resp, attempt, err := r.tryProvider(ctx, req, target, i+1)
		attempts = append(attempts, attempt)

		if err == nil {
			// 성공
			if i > 0 && lastErr != nil {
				resp.Metadata.FallbackUsed = true
				reason := fmt.Sprintf("fallback:%s - %s", policy, lastErr.Error())
				resp.Metadata.FallbackReason = &reason
			}
			return resp, attempts, nil
		}

		lastErr = err

		// fallback 여부 판정 (circuit_open, provider_not_found는 항상 다음으로)
		if attempt.Status == "circuit_open" || attempt.Status == "provider_not_found" {
			continue
		}
		if shouldFallback(policy, err) {
			continue
		}
		// fallback 대상이 아닌 에러 → 즉시 반환
		return nil, attempts, err
	}

	return nil, attempts, fmt.Errorf("모든 프로바이더 요청 실패: %w", lastErr)
}

// tryProvider - 단일 프로바이더에 요청 시도
// 세마포어/타임아웃/Circuit Breaker를 이 함수 스코프 안에서 처리한다.
func (r *Router) tryProvider(ctx context.Context, req *model.ChatRequest, target model.FallbackTarget, order int) (*model.ChatResponse, FallbackAttempt, error) {
	attempt := FallbackAttempt{
		Order:    order,
		Provider: target.Provider,
		Model:    target.Model,
	}

	// Circuit Breaker 확인
	if cb, ok := r.breakers[target.Provider]; ok {
		if !cb.Allow() {
			attempt.Status = "circuit_open"
			attempt.Error = fmt.Sprintf("프로바이더 '%s' Circuit Breaker 열림", target.Provider)
			r.logger.Warn("Circuit Breaker 열림, 건너뜀", zap.String("provider", target.Provider))
			return nil, attempt, fmt.Errorf("%s", attempt.Error)
		}
	}

	// 프로바이더 조회
	p, err := r.registry.Get(target.Provider)
	if err != nil {
		attempt.Status = "provider_not_found"
		attempt.Error = err.Error()
		return nil, attempt, err
	}

	// 세마포어 획득 (함수 스코프에서 defer로 안전하게 해제)
	if sem, ok := r.semaphores[target.Provider]; ok {
		if err := sem.Acquire(ctx); err != nil {
			attempt.Status = "timeout"
			attempt.Error = "세마포어 획득 타임아웃"
			return nil, attempt, err
		}
		defer sem.Release()
	}

	// 프로바이더별 타임아웃 적용
	provCtx := ctx
	if timeoutMs, ok := r.getProviderTimeout(target.Provider); ok {
		var cancel context.CancelFunc
		provCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()
	}

	// 요청 실행
	provReq := *req
	provReq.Provider = target.Provider
	provReq.Model = target.Model

	start := time.Now()
	resp, err := p.Chat(provCtx, &provReq)
	attempt.LatencyMs = time.Since(start).Milliseconds()

	if err != nil {
		attempt.Status = "error"
		attempt.Error = err.Error()

		if cb, ok := r.breakers[target.Provider]; ok {
			cb.RecordFailure()
		}
		r.logger.Warn("프로바이더 요청 실패",
			zap.String("provider", target.Provider),
			zap.String("model", target.Model),
			zap.Error(err),
			zap.Int64("latency_ms", attempt.LatencyMs),
		)
		return nil, attempt, err
	}

	// 성공
	attempt.Status = "success"
	if cb, ok := r.breakers[target.Provider]; ok {
		cb.RecordSuccess()
	}

	return resp, attempt, nil
}

// executeFastest - 모든 대상에 동시 요청, 첫 성공 반환
func (r *Router) executeFastest(ctx context.Context, req *model.ChatRequest, targets []model.FallbackTarget) (*model.ChatResponse, []FallbackAttempt, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		resp    *model.ChatResponse
		attempt FallbackAttempt
		err     error
	}

	ch := make(chan result, len(targets))

	for i, target := range targets {
		go func(order int, t model.FallbackTarget) {
			attempt := FallbackAttempt{
				Order:    order,
				Provider: t.Provider,
				Model:    t.Model,
			}

			p, err := r.registry.Get(t.Provider)
			if err != nil {
				attempt.Status = "error"
				attempt.Error = err.Error()
				ch <- result{attempt: attempt, err: err}
				return
			}

			provReq := *req
			provReq.Provider = t.Provider
			provReq.Model = t.Model

			start := time.Now()
			resp, err := p.Chat(ctx, &provReq)
			attempt.LatencyMs = time.Since(start).Milliseconds()

			if err != nil {
				attempt.Status = "error"
				attempt.Error = err.Error()
				ch <- result{attempt: attempt, err: err}
				return
			}

			attempt.Status = "success"
			ch <- result{resp: resp, attempt: attempt}
		}(i+1, target)
	}

	// 결과 수집
	var attempts []FallbackAttempt
	var mu sync.Mutex
	var lastErr error

	for range targets {
		res := <-ch
		mu.Lock()
		attempts = append(attempts, res.attempt)
		mu.Unlock()

		if res.err == nil && res.resp != nil {
			cancel() // 나머지 요청 취소
			if len(targets) > 1 {
				res.resp.Metadata.FallbackUsed = true
				reason := "always_fastest - 가장 빠른 응답 사용"
				res.resp.Metadata.FallbackReason = &reason
			}
			return res.resp, attempts, nil
		}
		lastErr = res.err
	}

	return nil, attempts, fmt.Errorf("모든 프로바이더 요청 실패 (always_fastest): %w", lastErr)
}

// buildTargets - primary + fallback 대상 목록 구성
func (r *Router) buildTargets(req *model.ChatRequest) []model.FallbackTarget {
	targets := []model.FallbackTarget{
		{Provider: req.Provider, Model: req.Model},
	}
	targets = append(targets, req.Fallback...)
	return targets
}

// getProviderTimeout - 프로바이더별 타임아웃(ms) 조회
func (r *Router) getProviderTimeout(providerName string) (int, bool) {
	if r.fbCfg != nil {
		if pCfg, ok := r.fbCfg.Providers[providerName]; ok && pCfg.TimeoutMs > 0 {
			return pCfg.TimeoutMs, true
		}
	}
	return 0, false
}

// BuildTargets - primary + fallback 대상 목록 구성 (외부 사용용)
func (r *Router) BuildTargets(req *model.ChatRequest) []model.FallbackTarget {
	return r.buildTargets(req)
}

// CheckCircuitBreaker - Circuit Breaker 상태 확인 (스트리밍 등 외부 사용용)
func (r *Router) CheckCircuitBreaker(providerName string) error {
	if cb, ok := r.breakers[providerName]; ok {
		if !cb.Allow() {
			return fmt.Errorf("프로바이더 '%s' Circuit Breaker 열림", providerName)
		}
	}
	return nil
}

// AcquireSemaphore - 세마포어 획득 (스트리밍 등 외부 사용용)
func (r *Router) AcquireSemaphore(ctx context.Context, providerName string) error {
	if sem, ok := r.semaphores[providerName]; ok {
		return sem.Acquire(ctx)
	}
	return nil
}

// ReleaseSemaphore - 세마포어 해제
func (r *Router) ReleaseSemaphore(providerName string) {
	if sem, ok := r.semaphores[providerName]; ok {
		sem.Release()
	}
}

// RecordSuccess - 성공 기록 (스트리밍 등 외부 사용용)
func (r *Router) RecordSuccess(providerName string) {
	if cb, ok := r.breakers[providerName]; ok {
		cb.RecordSuccess()
	}
}

// RecordFailure - 실패 기록 (스트리밍 등 외부 사용용)
func (r *Router) RecordFailure(providerName string) {
	if cb, ok := r.breakers[providerName]; ok {
		cb.RecordFailure()
	}
}

// ProviderTimeout - 프로바이더별 타임아웃(ms) 조회 (외부 사용용)
func (r *Router) ProviderTimeout(providerName string) (int, bool) {
	return r.getProviderTimeout(providerName)
}

// CircuitBreakerStats - 모든 프로바이더의 Circuit Breaker 상태 조회
func (r *Router) CircuitBreakerStats() []map[string]any {
	var stats []map[string]any
	for _, cb := range r.breakers {
		stats = append(stats, cb.Stats())
	}
	return stats
}
