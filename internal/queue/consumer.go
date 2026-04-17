package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
)

// Consumer - 배치 큐 소비자
// Worker goroutine 풀로 큐에서 요청을 꺼내 처리한다.
type Consumer struct {
	rdb      *redis.Client
	registry *provider.Registry
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

	// 상태: processing
	_ = c.jobStore.UpdateStatus(ctx, item.JobID, model.JobProcessing)

	// 프로바이더 조회 및 실행
	p, err := c.registry.Get(req.Provider)
	if err != nil {
		c.failJob(ctx, item.JobID, err)
		return
	}

	resp, err := p.Chat(ctx, &req)
	if err != nil {
		c.failJob(ctx, item.JobID, err)
		return
	}

	// 성공: Job 업데이트
	job := &model.BatchJob{
		JobID:     item.JobID,
		Status:    model.JobCompleted,
		Request:   &req,
		Response:  resp,
		CreatedAt: time.Now().UTC(), // 원래 생성 시각을 유지하려면 별도 관리 필요
		UpdatedAt: time.Now().UTC(),
	}
	if err := c.jobStore.Save(ctx, job); err != nil {
		c.logger.Error("Job 저장 실패", zap.String("job_id", item.JobID), zap.Error(err))
		return
	}

	c.logger.Info("배치 처리 완료", zap.String("job_id", item.JobID), zap.String("provider", resp.Provider))
}

// failJob - Job 실패 처리
func (c *Consumer) failJob(ctx context.Context, jobID string, err error) {
	errStr := err.Error()
	job := &model.BatchJob{
		JobID:     jobID,
		Status:    model.JobFailed,
		Error:     &errStr,
		UpdatedAt: time.Now().UTC(),
	}
	_ = c.jobStore.Save(ctx, job)
	c.logger.Error("배치 처리 실패", zap.String("job_id", jobID), zap.Error(err))
}
