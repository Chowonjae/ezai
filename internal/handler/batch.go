package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/middleware"
	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
	"github.com/Chowonjae/ezai/internal/queue"
)

// BatchHandler - 배치 요청 핸들러
type BatchHandler struct {
	producer *queue.Producer
	jobStore *queue.JobStore
	registry *provider.Registry
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

// SetRegistry - 프로바이더 레지스트리 설정 (배치 요청 검증용)
func (h *BatchHandler) SetRegistry(r *provider.Registry) {
	h.registry = r
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

	if err := req.ValidateOptions(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 프로바이더 존재 여부 검증 (큐에 넣기 전에 빠른 실패)
	if h.registry != nil {
		if _, err := h.registry.Get(req.Provider); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	traceID := middleware.GetTraceID(c)
	clientID := middleware.GetClientID(c)

	jobID, err := h.producer.Enqueue(c.Request.Context(), &req, clientID)
	if err != nil {
		h.logger.Error("배치 등록 실패", zap.String("trace_id", traceID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "배치 등록 실패",
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
// 배치 작업 상태 및 결과 조회 (소유자 또는 신뢰 네트워크만 접근 가능)
func (h *BatchHandler) GetJob(c *gin.Context) {
	jobID := c.Param("job_id")

	job, err := h.jobStore.Get(c.Request.Context(), jobID)
	if err != nil {
		if errors.Is(err, queue.ErrJobNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Job 조회 실패"})
		}
		return
	}

	// 소유자 검증: 신뢰 네트워크가 아니면 본인 job만 조회 가능
	clientID := middleware.GetClientID(c)
	if !middleware.IsTrustedNet(c) && job.ClientID != clientID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job을 찾을 수 없습니다"})
		return
	}

	c.JSON(http.StatusOK, job)
}
