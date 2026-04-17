package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/config"
	"github.com/Chowonjae/ezai/internal/store"
)

// UsageAdminHandler - 비용/로그 관리 API 핸들러 (아카이브, 리셋, 삭제)
type UsageAdminHandler struct {
	usageAdmin      *store.UsageAdmin
	retentionConfig *config.RetentionConfig
	logger          *zap.Logger
}

// NewUsageAdminHandler - 비용 관리 핸들러 생성
func NewUsageAdminHandler(usageAdmin *store.UsageAdmin, retentionConfig *config.RetentionConfig, logger *zap.Logger) *UsageAdminHandler {
	return &UsageAdminHandler{
		usageAdmin:      usageAdmin,
		retentionConfig: retentionConfig,
		logger:          logger,
	}
}

// archiveRequest - 아카이브 요청 구조체
type archiveRequest struct {
	Before       string `json:"before" binding:"required"`       // 이 날짜 이전 데이터 아카이브 (YYYY-MM-DD)
	Confirmation string `json:"confirmation" binding:"required"` // 확인 문자열
	Reason       string `json:"reason"`                          // 사유
}

// Archive - POST /admin/usage/archive
// 지정 기간 데이터를 일별 요약으로 집계 후 원본 삭제
func (h *UsageAdminHandler) Archive(c *gin.Context) {
	// 작업 허용 여부 확인
	if !h.retentionConfig.Retention.Reset.IsOperationAllowed("archive") {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "archive 작업이 허용되지 않았습니다. usage_retention.yaml의 allowed_operations를 확인하세요",
		})
		return
	}

	var req archiveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청: " + err.Error()})
		return
	}

	// 확인 문자열 검증
	if h.retentionConfig.Retention.Reset.RequireConfirmation {
		expected := fmt.Sprintf("CONFIRM-ARCHIVE-%s", req.Before)
		if req.Confirmation != expected {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":    "확인 문자열이 일치하지 않습니다",
				"expected": expected,
			})
			return
		}
	}

	result, err := h.usageAdmin.Archive(req.Before)
	if err != nil {
		h.logger.Error("아카이브 실패", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "아카이브 실패: " + err.Error()})
		return
	}

	h.logger.Info("아카이브 완료",
		zap.String("before", req.Before),
		zap.Int("archived_rows", result.ArchivedRows),
		zap.Int("summary_rows", result.SummaryRows),
		zap.String("reason", req.Reason),
	)

	c.JSON(http.StatusOK, result)
}

// resetRequest - 소프트 리셋 요청 구조체
type resetRequest struct {
	Before       string `json:"before" binding:"required"`
	Confirmation string `json:"confirmation" binding:"required"`
	Reason       string `json:"reason"`
}

// SoftReset - POST /admin/usage/reset
// 지정 기간 로그의 비용 필드를 0으로 리셋 (로그 자체는 보존)
func (h *UsageAdminHandler) SoftReset(c *gin.Context) {
	if !h.retentionConfig.Retention.Reset.IsOperationAllowed("soft_reset") {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "soft_reset 작업이 허용되지 않았습니다. usage_retention.yaml의 allowed_operations를 확인하세요",
		})
		return
	}

	var req resetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청: " + err.Error()})
		return
	}

	if h.retentionConfig.Retention.Reset.RequireConfirmation {
		expected := fmt.Sprintf("CONFIRM-RESET-%s", req.Before)
		if req.Confirmation != expected {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":    "확인 문자열이 일치하지 않습니다",
				"expected": expected,
			})
			return
		}
	}

	result, err := h.usageAdmin.SoftReset(req.Before)
	if err != nil {
		h.logger.Error("소프트 리셋 실패", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "소프트 리셋 실패: " + err.Error()})
		return
	}

	h.logger.Info("소프트 리셋 완료",
		zap.String("before", req.Before),
		zap.String("reason", req.Reason),
	)

	c.JSON(http.StatusOK, result)
}

// hardDeleteRequest - 하드 삭제 요청 구조체
type hardDeleteRequest struct {
	Before       string `json:"before" binding:"required"`
	Confirmation string `json:"confirmation" binding:"required"`
	Reason       string `json:"reason"`
}

// HardDelete - DELETE /admin/usage
// 지정 기간 로그를 완전 삭제 (기본 비활성화, usage_retention.yaml에서 활성화 필요)
func (h *UsageAdminHandler) HardDelete(c *gin.Context) {
	if !h.retentionConfig.Retention.Reset.IsOperationAllowed("hard_delete") {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "hard_delete 작업이 허용되지 않았습니다. usage_retention.yaml의 allowed_operations에 hard_delete를 추가하세요",
		})
		return
	}

	var req hardDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청: " + err.Error()})
		return
	}

	if h.retentionConfig.Retention.Reset.RequireConfirmation {
		expected := fmt.Sprintf("CONFIRM-DELETE-%s", req.Before)
		if req.Confirmation != expected {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":    "확인 문자열이 일치하지 않습니다",
				"expected": expected,
			})
			return
		}
	}

	result, err := h.usageAdmin.HardDelete(req.Before)
	if err != nil {
		h.logger.Error("하드 삭제 실패", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "하드 삭제 실패: " + err.Error()})
		return
	}

	h.logger.Warn("하드 삭제 완료",
		zap.String("before", req.Before),
		zap.Int("deleted_rows", result.DeletedRows),
		zap.String("reason", req.Reason),
	)

	c.JSON(http.StatusOK, result)
}

// RetentionPolicy - GET /admin/usage/retention
// 현재 보존 정책 설정을 반환한다.
func (h *UsageAdminHandler) RetentionPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, h.retentionConfig)
}
