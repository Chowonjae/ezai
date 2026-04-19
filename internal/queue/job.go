package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Chowonjae/ezai/internal/model"
)

// ErrJobNotFound - Job을 찾을 수 없을 때 반환하는 sentinel error
var ErrJobNotFound = errors.New("Job을 찾을 수 없습니다")

const (
	jobKeyPrefix     = "ezai:batch:job:"        // Redis Hash 키 접두사
	processingSetKey = "ezai:batch:processing"  // processing 상태 Job 추적용 Sorted Set (score=시작 시각)
	jobTTL           = 24 * time.Hour           // Job 데이터 보존 기간
)

// JobStore - Redis 기반 Job 상태 관리
type JobStore struct {
	rdb *redis.Client
}

// NewJobStore - Job 저장소 생성
func NewJobStore(rdb *redis.Client) *JobStore {
	return &JobStore{rdb: rdb}
}

// Save - Job 상태 저장 (Redis Hash)
func (js *JobStore) Save(ctx context.Context, job *model.BatchJob) error {
	key := jobKeyPrefix + job.JobID

	reqJSON, _ := json.Marshal(job.Request)
	respJSON, _ := json.Marshal(job.Response)
	errStr := ""
	if job.Error != nil {
		errStr = *job.Error
	}

	fields := map[string]interface{}{
		"job_id":     job.JobID,
		"client_id":  job.ClientID,
		"status":     string(job.Status),
		"request":    string(reqJSON),
		"response":   string(respJSON),
		"error":      errStr,
		"created_at": job.CreatedAt.Format(time.RFC3339),
		"updated_at": job.UpdatedAt.Format(time.RFC3339),
	}

	pipe := js.rdb.Pipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, jobTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// Get - Job 상태 조회
func (js *JobStore) Get(ctx context.Context, jobID string) (*model.BatchJob, error) {
	key := jobKeyPrefix + jobID

	vals, err := js.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("Job 조회 실패: %w", err)
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}

	job := &model.BatchJob{
		JobID:    vals["job_id"],
		ClientID: vals["client_id"],
		Status:   model.JobStatus(vals["status"]),
	}

	if vals["request"] != "" && vals["request"] != "null" {
		var req model.ChatRequest
		if err := json.Unmarshal([]byte(vals["request"]), &req); err == nil {
			job.Request = &req
		}
	}

	if vals["response"] != "" && vals["response"] != "null" {
		var resp model.ChatResponse
		if err := json.Unmarshal([]byte(vals["response"]), &resp); err == nil {
			job.Response = &resp
		}
	}

	if vals["error"] != "" {
		errStr := vals["error"]
		job.Error = &errStr
	}

	if t, err := time.Parse(time.RFC3339, vals["created_at"]); err == nil {
		job.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, vals["updated_at"]); err == nil {
		job.UpdatedAt = t
	}

	return job, nil
}

// UpdateStatus - Job 상태만 업데이트
func (js *JobStore) UpdateStatus(ctx context.Context, jobID string, status model.JobStatus) error {
	key := jobKeyPrefix + jobID
	return js.rdb.HSet(ctx, key,
		"status", string(status),
		"updated_at", time.Now().UTC().Format(time.RFC3339),
	).Err()
}

// MarkProcessing - Job을 processing 상태로 전환하고 추적 Set에 등록
func (js *JobStore) MarkProcessing(ctx context.Context, jobID string) error {
	if err := js.UpdateStatus(ctx, jobID, model.JobProcessing); err != nil {
		return err
	}
	return js.rdb.ZAdd(ctx, processingSetKey, redis.Z{
		Score:  float64(time.Now().UTC().Unix()),
		Member: jobID,
	}).Err()
}

// ClearProcessing - 완료/실패 시 추적 Set에서 제거
func (js *JobStore) ClearProcessing(ctx context.Context, jobID string) {
	js.rdb.ZRem(ctx, processingSetKey, jobID)
}

// StaleProcessingJobs - 지정 시간 이상 processing 상태인 Job ID 목록 반환
func (js *JobStore) StaleProcessingJobs(ctx context.Context, threshold time.Duration) ([]string, error) {
	cutoff := float64(time.Now().UTC().Add(-threshold).Unix())
	return js.rdb.ZRangeByScore(ctx, processingSetKey, &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%f", cutoff),
	}).Result()
}
