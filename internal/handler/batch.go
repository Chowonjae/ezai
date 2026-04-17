package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/middleware"
	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/queue"
)

// BatchHandler - 배치 요청 핸들러
type BatchHandler struct {
	producer *queue.Producer
	jobStore *queue.JobStore
	logger   *zap.Logger
}

// NewBatchHandler - 배치 핸들러 생성
func NewBatchHandler(producer *queue.Producer, jobStore *queue.JobStore, logger *zap.Logger) *BatchHandler {
	return &BatchHandler{
		producer: producer,
		jobStore: jobStore,
		logger:   logger,
	}
}

// Submit - POST /batch
// 배치 요청을 큐에 등록하고 202 Accepted + Job ID 반환
func (h *BatchHandler) Submit(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "잘못된 요청 형식: " + err.Error(),
		})
		return
	}

	traceID := middleware.GetTraceID(c)

	jobID, err := h.producer.Enqueue(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "배치 등록 실패: " + err.Error(),
		})
		return
	}

	h.logger.Info("배치 요청 등록",
		zap.String("trace_id", traceID),
		zap.String("job_id", jobID),
		zap.String("provider", req.Provider),
	)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":  jobID,
		"status":  "pending",
		"message": "배치 요청이 등록되었습니다",
	})
}

// GetJob - GET /batch/:job_id
// 배치 작업 상태 및 결과 조회
func (h *BatchHandler) GetJob(c *gin.Context) {
	jobID := c.Param("job_id")

	job, err := h.jobStore.Get(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, job)
}
