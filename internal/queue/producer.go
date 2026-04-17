package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Chowonjae/ezai/internal/model"
)

const (
	batchQueue = "ezai:batch:queue" // Redis List 큐
)

// Producer - 배치 요청 큐 등록
type Producer struct {
	rdb      *redis.Client
	jobStore *JobStore
}

// NewProducer - Producer 생성
func NewProducer(rdb *redis.Client, jobStore *JobStore) *Producer {
	return &Producer{rdb: rdb, jobStore: jobStore}
}

// Enqueue - 배치 요청을 큐에 등록하고 Job ID 반환
func (p *Producer) Enqueue(ctx context.Context, req *model.ChatRequest) (string, error) {
	jobID := fmt.Sprintf("job_%s_%s", time.Now().Format("20060102_150405"), uuid.New().String()[:8])

	// Job 상태 저장
	job := &model.BatchJob{
		JobID:     jobID,
		Status:    model.JobPending,
		Request:   req,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := p.jobStore.Save(ctx, job); err != nil {
		return "", fmt.Errorf("Job 저장 실패: %w", err)
	}

	// 큐에 Job ID 추가 (LPUSH → Consumer가 BRPOP)
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("요청 직렬화 실패: %w", err)
	}

	queueItem := map[string]string{
		"job_id":  jobID,
		"request": string(reqJSON),
	}
	itemJSON, _ := json.Marshal(queueItem)

	if err := p.rdb.LPush(ctx, batchQueue, string(itemJSON)).Err(); err != nil {
		return "", fmt.Errorf("큐 등록 실패: %w", err)
	}

	return jobID, nil
}
