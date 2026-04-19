package server

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/handler"
	"github.com/Chowonjae/ezai/internal/middleware"
)

// routesDeps - 라우트 등록에 필요한 의존성
type routesDeps struct {
	chatHandler   *handler.ChatHandler
	modelsHandler *handler.ModelsHandler
	streamHandler          *handler.StreamHandler
	batchHandler           *handler.BatchHandler
	usageHandler           *handler.UsageHandler
	adminHandler           *handler.AdminHandler
	usageAdminHandler      *handler.UsageAdminHandler
	clientKeyAdminHandler  *handler.ClientKeyAdminHandler
	clientKeyRotateHandler *handler.ClientKeyRotateHandler
	trustedCIDRs           []string
	logger                 *zap.Logger
}

// registerRoutes - 라우트 등록
// 주의: /health는 server.go에서 인증 미들웨어 이전에 별도 등록됨
func registerRoutes(r *gin.Engine, d routesDeps) {
	// 채팅 API
	r.POST("/chat", d.chatHandler.Chat)

	// 스트리밍 API
	r.POST("/chat/stream", d.streamHandler.Stream)

	// 모델 목록
	r.GET("/models", d.modelsHandler.Models)

	// 배치 API
	if d.batchHandler != nil {
		r.POST("/batch", d.batchHandler.Submit)
		r.GET("/batch/:job_id", d.batchHandler.GetJob)
	}

	// 사용량/비용 조회
	if d.usageHandler != nil {
		r.GET("/usage", d.usageHandler.Usage)
		r.GET("/usage/pricing", d.usageHandler.Pricing)
	}

	// 셀프 로테이션 (인증된 외부 클라이언트용)
	if d.clientKeyRotateHandler != nil {
		r.POST("/v1/keys/rotate", d.clientKeyRotateHandler.Rotate)
	}

	// Admin API (신뢰 네트워크에서만 접근 가능)
	trustedOnly := middleware.TrustedNetworkOnly(d.trustedCIDRs, d.logger)

	if d.adminHandler != nil {
		admin := r.Group("/admin")
		admin.Use(trustedOnly)
		{
			admin.GET("/keys", d.adminHandler.ListKeys)
			admin.POST("/keys", d.adminHandler.CreateKey)
			admin.PUT("/keys/:id", d.adminHandler.UpdateKey)
			admin.POST("/keys/:id/rotate", d.adminHandler.RotateKey)
			admin.DELETE("/keys/:id", d.adminHandler.DeleteKey)
			admin.GET("/keys/audit", d.adminHandler.ListAuditLogs)
			admin.GET("/logs", d.adminHandler.ListLogs)
			admin.GET("/logs/stats", d.adminHandler.LogStats)
		}
	}

	// 클라이언트 키 관리 Admin API
	if d.clientKeyAdminHandler != nil {
		ckAdmin := r.Group("/admin/client-keys")
		ckAdmin.Use(trustedOnly)
		{
			ckAdmin.POST("", d.clientKeyAdminHandler.Create)
			ckAdmin.GET("", d.clientKeyAdminHandler.List)
			ckAdmin.DELETE("/:client_id", d.clientKeyAdminHandler.Revoke)
			ckAdmin.POST("/:client_id/reissue", d.clientKeyAdminHandler.Reissue)
		}
	}

	// Admin Usage API (아카이브, 리셋, 삭제)
	if d.usageAdminHandler != nil {
		usageAdmin := r.Group("/admin")
		usageAdmin.Use(trustedOnly)
		{
			usageAdmin.POST("/usage/archive", d.usageAdminHandler.Archive)
			usageAdmin.POST("/usage/reset", d.usageAdminHandler.SoftReset)
			usageAdmin.DELETE("/usage", d.usageAdminHandler.HardDelete)
			usageAdmin.GET("/usage/retention", d.usageAdminHandler.RetentionPolicy)
		}
	}
}
