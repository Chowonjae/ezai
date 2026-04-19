package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
	"github.com/Chowonjae/ezai/internal/router"
)

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
		go c.worker(ctx, i)
	}
	c.logger.Info("배치 Consumer 시작", zap.Int("workers", c.workers))
}

// Stop - Worker 종료
func (c *Consumer) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
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
		return
	}

	c.logger.Info("배치 처리 시작", zap.String("job_id", item.JobID), zap.String("provider", req.Provider))

	// 원래 생성 시각 보존 (상태 변경 전에 조회)
	createdAt := time.Now().UTC()
	if existing, err := c.jobStore.Get(ctx, item.JobID); err == nil {
		createdAt = existing.CreatedAt
	}

	// 상태: processing
	if err := c.jobStore.UpdateStatus(ctx, item.JobID, model.JobProcessing); err != nil {
		c.logger.Warn("Job 상태 업데이트 실패", zap.String("job_id", item.JobID), zap.Error(err))
	}

	// Router를 통한 실행 (fallback/circuit breaker/세마포어 적용)
	var resp *model.ChatResponse
	var err error

	if c.router != nil {
		resp, _, err = c.router.Execute(ctx, &req)
	} else {
		// Router 미설정: 직접 프로바이더 호출
		var p provider.Provider
		p, err = c.registry.Get(req.Provider)
		if err != nil {
			c.failJob(ctx, item.JobID, createdAt, err)
			return
		}
		resp, err = p.Chat(ctx, &req)
	}

	if err != nil {
		c.failJob(ctx, item.JobID, createdAt, err)
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
