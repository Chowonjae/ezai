package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler - 헬스체크 핸들러
type HealthHandler struct{}

// NewHealthHandler - 헬스체크 핸들러 생성
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Health - GET /health
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}
