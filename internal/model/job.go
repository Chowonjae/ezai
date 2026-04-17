package model

import "time"

// JobStatus - 배치 작업 상태
type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobProcessing JobStatus = "processing"
	JobCompleted  JobStatus = "completed"
	JobFailed     JobStatus = "failed"
)

// BatchJob - 배치 작업
type BatchJob struct {
	JobID     string        `json:"job_id"`
	Status    JobStatus     `json:"status"`
	Request   *ChatRequest  `json:"request,omitempty"`
	Response  *ChatResponse `json:"response,omitempty"`
	Error     *string       `json:"error,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}
