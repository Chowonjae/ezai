package queue

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
	"github.com/Chowonjae/ezai/internal/router"
)

const maxRetries = 3 // 배치 작업 최대 재시도 횟수

// Consumer - 배치 큐 소비자
// Worker goroutine 풀로 큐에서 요청을 꺼내 처리한다.
type Consumer struct {
	rdb      *redis.Client
	registry *provider.Registry
	router   *router.Router
	jobStore *JobStore
	logger   *zap.Logger
	workers  int
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewConsumer - Consumer 생성
func NewConsumer(rdb *redis.Client, registry *provider.Registry, jobStore *JobStore, logger *zap.Logger, workers int) *Consumer {
	if workers <= 0 {
		workers = 3
	}
	return &Consumer{
		rdb:      rdb,
		registry: registry,
		jobStore: jobStore,
		logger:   logger,
		workers:  workers,
	}
}

// SetRouter - 라우터 설정 (fallback/circuit breaker 적용)
func (c *Consumer) SetRouter(r *router.Router) {
	c.router = r
}

// Start - Worker goroutine 풀 시작
func (c *Consumer) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	for i := 0; i < c.workers; i++ {
		c.wg.Add(1)
		go func(id int) {
			defer c.wg.Done()
			c.worker(ctx, id)
		}(i)
	}
	c.logger.Info("배치 Consumer 시작", zap.Int("workers", c.workers))
}

// Stop - Worker 종료 (진행 중 작업 완료 대기)
func (c *Consumer) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	c.logger.Info("배치 Consumer 종료 완료")
}

// worker - 단일 Worker: BRPOP으로 큐에서 대기하며 요청 처리
func (c *Consumer) worker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("배치 Worker 종료", zap.Int("worker_id", id))
			return
		default:
		}

		// BRPOP: 큐에 항목이 올 때까지 최대 5초 대기
		result, err := c.rdb.BRPop(ctx, 5*time.Second, batchQueue).Result()
		if err != nil {
			if err == redis.Nil {
				continue // 타임아웃, 다시 대기
			}
			if ctx.Err() != nil {
				return // 컨텍스트 취소
			}
			c.logger.Error("큐 읽기 실패", zap.Error(err))
			time.Sleep(time.Second)
			continue
		}

		// result[0] = 큐 이름, result[1] = 데이터
		if len(result) < 2 {
			continue
		}

		c.processItem(ctx, result[1])
	}
}

// processItem - 큐 항목 처리
func (c *Consumer) processItem(ctx context.Context, itemJSON string) {
	var item struct {
		JobID   string `json:"job_id"`
		Request string `json:"request"`
	}
	if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
		c.logger.Error("큐 항목 파싱 실패", zap.Error(err))
		return
	}

	var req model.ChatRequest
	if err := json.Unmarshal([]byte(item.Request), &req); err != nil {
		c.logger.Error("요청 파싱 실패", zap.String("job_id", item.JobID), zap.Error(err))
		// 파싱 실패한 Job도 failed 상태로 갱신하여 영원히 pending에 빠지지 않도록 함
		c.failJob(ctx, item.JobID, time.Now().UTC(), err)
		return
	}

	c.logger.Info("배치 처리 시작", zap.String("job_id", item.JobID), zap.String("provider", req.Provider))

	// 원래 생성 시각 보존 + 중복 처리 방지
	existing, err := c.jobStore.Get(ctx, item.JobID)
	createdAt := time.Now().UTC()
	if err == nil && existing != nil {
		// 이미 완료/실패한 Job은 중복 처리 방지
		if existing.Status == model.JobCompleted || existing.Status == model.JobFailed {
			c.logger.Info("이미 처리된 Job, 스킵",
				zap.String("job_id", item.JobID), zap.String("status", string(existing.Status)))
			return
		}
		createdAt = existing.CreatedAt
	}

	// 상태: processing
	if err := c.jobStore.UpdateStatus(ctx, item.JobID, model.JobProcessing); err != nil {
		c.logger.Warn("Job 상태 업데이트 실패", zap.String("job_id", item.JobID), zap.Error(err))
	}

	// Router를 통한 실행 (fallback/circuit breaker/세마포어 적용)
	// 일시적 에러 시 지수 백오프로 재시도
	var resp *model.ChatResponse
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 지수 백오프: 1s, 2s, 4s
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			c.logger.Info("배치 재시도 대기",
				zap.String("job_id", item.JobID),
				zap.Int("attempt", attempt+1),
				zap.Duration("backoff", backoff),
			)
			select {
			case <-ctx.Done():
				c.failJob(ctx, item.JobID, createdAt, lastErr)
				return
			case <-time.After(backoff):
			}
		}

		var err error
		if c.router != nil {
			resp, _, err = c.router.Execute(ctx, &req)
		} else {
			var p provider.Provider
			p, err = c.registry.Get(req.Provider)
			if err != nil {
				c.failJob(ctx, item.JobID, createdAt, err)
				return
			}
			resp, err = p.Chat(ctx, &req)
		}

		if err == nil {
			break // 성공
		}

		lastErr = err

		// 재시도 불가능한 에러 (잘못된 요청 등)는 즉시 실패
		if !isRetryableError(err) {
			c.failJob(ctx, item.JobID, createdAt, err)
			return
		}
	}

	if resp == nil {
		c.failJob(ctx, item.JobID, createdAt, lastErr)
		return
	}

	// 성공: Job 업데이트
	now := time.Now().UTC()
	job := &model.BatchJob{
		JobID:     item.JobID,
		Status:    model.JobCompleted,
		Request:   &req,
		Response:  resp,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	if err := c.jobStore.Save(ctx, job); err != nil {
		c.logger.Error("Job 저장 실패", zap.String("job_id", item.JobID), zap.Error(err))
		return
	}

	c.logger.Info("배치 처리 완료", zap.String("job_id", item.JobID), zap.String("provider", resp.Provider))
}

// failJob - Job 실패 처리
func (c *Consumer) failJob(ctx context.Context, jobID string, createdAt time.Time, err error) {
	errStr := err.Error()
	now := time.Now().UTC()
	job := &model.BatchJob{
		JobID:     jobID,
		Status:    model.JobFailed,
		Error:     &errStr,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	if saveErr := c.jobStore.Save(ctx, job); saveErr != nil {
		c.logger.Warn("실패 Job 저장 실패", zap.String("job_id", jobID), zap.Error(saveErr))
	}
	c.logger.Error("배치 처리 실패", zap.String("job_id", jobID), zap.Error(err))
}

// isRetryableError - 재시도 가능한 일시적 에러 여부
// 네트워크 에러, 타임아웃, 서버 에러(5xx)는 재시도 가능.
// 잘못된 요청(4xx), 프로바이더 미발견 등은 재시도 불가.
func isRetryableError(err error) bool {
	msg := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"timeout", "deadline exceeded",
		"connection refused", "connection reset",
		"network is unreachable", "eof",
		"internal server error", "bad gateway",
		"service unavailable", "gateway timeout",
		"rate limit", "too many requests",
	}
	for _, p := range retryablePatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	// "429"는 status code 패턴으로 매칭하여 오탐 방지
	raw := err.Error()
	statusPatterns := []string{"status: 429", "status:429", "status code: 429", "429 "}
	for _, p := range statusPatterns {
		if strings.Contains(raw, p) {
			return true
		}
	}
	return false
}
