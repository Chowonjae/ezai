package server

import (
	"github.com/gin-gonic/gin"

	"github.com/Chowonjae/ezai/internal/handler"
)

// registerRoutes - 라우트 등록
func registerRoutes(r *gin.Engine, chatHandler *handler.ChatHandler, healthHandler *handler.HealthHandler, modelsHandler *handler.ModelsHandler, streamHandler *handler.StreamHandler, batchHandler *handler.BatchHandler, usageHandler *handler.UsageHandler, adminHandler *handler.AdminHandler, usageAdminHandler *handler.UsageAdminHandler) {
	// 헬스체크
	r.GET("/health", healthHandler.Health)

	// 채팅 API
	r.POST("/chat", chatHandler.Chat)

	// 스트리밍 API
	r.POST("/chat/stream", streamHandler.Stream)

	// 모델 목록
	r.GET("/models", modelsHandler.Models)

	// 배치 API
	if batchHandler != nil {
		r.POST("/batch", batchHandler.Submit)
		r.GET("/batch/:job_id", batchHandler.GetJob)
	}

	// 사용량/비용 조회
	if usageHandler != nil {
		r.GET("/usage", usageHandler.Usage)
		r.GET("/usage/pricing", usageHandler.Pricing)
	}

	// Admin API
	if adminHandler != nil {
		admin := r.Group("/admin")
		{
			admin.GET("/keys", adminHandler.ListKeys)
			admin.POST("/keys", adminHandler.CreateKey)
			admin.PUT("/keys/:id", adminHandler.UpdateKey)
			admin.POST("/keys/:id/rotate", adminHandler.RotateKey)
			admin.DELETE("/keys/:id", adminHandler.DeleteKey)
			admin.GET("/keys/audit", adminHandler.ListAuditLogs)
			admin.GET("/logs", adminHandler.ListLogs)
		}
	}

	// Admin Usage API (아카이브, 리셋, 삭제)
	if usageAdminHandler != nil {
		admin := r.Group("/admin")
		{
			admin.POST("/usage/archive", usageAdminHandler.Archive)
			admin.POST("/usage/reset", usageAdminHandler.SoftReset)
			admin.DELETE("/usage", usageAdminHandler.HardDelete)
			admin.GET("/usage/retention", usageAdminHandler.RetentionPolicy)
		}
	}
}
