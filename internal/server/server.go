package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/cache"
	"github.com/Chowonjae/ezai/internal/config"
	"github.com/Chowonjae/ezai/internal/handler"
	"github.com/Chowonjae/ezai/internal/middleware"
	"github.com/Chowonjae/ezai/internal/provider"
	"github.com/Chowonjae/ezai/internal/queue"
	"github.com/Chowonjae/ezai/internal/router"
	"github.com/Chowonjae/ezai/internal/service"
	"github.com/Chowonjae/ezai/internal/store"
)

// Server - HTTP 서버
type Server struct {
	httpServer *http.Server
	engine     *gin.Engine
	logger     *zap.Logger
	cfg        *config.Config
	logWriter  *store.RequestLogWriter
	consumer   *queue.Consumer
}

// Deps - 서버 의존성 (New 파라미터 정리용)
type Deps struct {
	Config         *config.Config
	Registry       *provider.Registry
	Router         *router.Router
	Logger         *zap.Logger
	KeyStore       *store.KeyStore
	AuditLog       *store.AuditLog
	LogWriter      *store.RequestLogWriter
	PromptManager  *config.PromptManager
	PricingManager *config.PricingManager
	UsageReader    *store.UsageReader
	Cache          *cache.Cache
	Redis          *redis.Client
	Producer       *queue.Producer
	JobStore       *queue.JobStore
	Consumer        *queue.Consumer
	UsageAdmin      *store.UsageAdmin
	RetentionConfig *config.RetentionConfig
	ClientKeyStore  *store.ClientKeyStore
}

// New - 서버 생성
func New(d Deps) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	// ClientKeyValidator 구성
	var clientKeyValidator *service.ClientKeyService
	authCfg := middleware.AuthConfig{
		TrustedCIDRs: d.Config.Auth.TrustedCIDRs,
	}
	if d.ClientKeyStore != nil {
		clientKeyValidator = service.NewClientKeyService(d.ClientKeyStore, d.Redis)
		authCfg.Validator = clientKeyValidator
	}

	// 미들웨어 체인: Recovery → RequestID → Logger → Auth
	engine.Use(
		middleware.Recovery(d.Logger),
		middleware.RequestID(),
		middleware.Logger(d.Logger),
		middleware.Auth(authCfg, d.Logger),
	)

	// Redis Rate Limiting (Redis가 설정된 경우)
	if d.Redis != nil {
		engine.Use(middleware.RateLimit(d.Redis, middleware.RateLimitConfig{
			RequestsPerMinute: 120,
		}, d.Logger))
	}

	// 핸들러 생성
	chatHandler := handler.NewChatHandler(d.Registry, d.Logger)
	if d.Router != nil {
		chatHandler.SetRouter(d.Router)
	}
	if d.LogWriter != nil {
		chatHandler.SetLogWriter(d.LogWriter)
	}
	if d.PromptManager != nil {
		chatHandler.SetPromptManager(d.PromptManager)
	}
	if d.PricingManager != nil {
		chatHandler.SetPricingManager(d.PricingManager)
	}
	if d.Cache != nil {
		chatHandler.SetCache(d.Cache)
	}

	healthHandler := handler.NewHealthHandler()
	modelsHandler := handler.NewModelsHandler(d.Registry)
	streamHandler := handler.NewStreamHandler(d.Registry, d.Logger)

	// Usage 핸들러
	var usageHandler *handler.UsageHandler
	if d.UsageReader != nil {
		usageHandler = handler.NewUsageHandler(d.UsageReader, d.PricingManager)
	}

	// 배치 핸들러 (Redis가 설정된 경우)
	var batchHandler *handler.BatchHandler
	if d.Producer != nil && d.JobStore != nil {
		batchHandler = handler.NewBatchHandler(d.Producer, d.JobStore, d.Logger)
	}

	// Admin 핸들러
	var adminHandler *handler.AdminHandler
	if d.KeyStore != nil && d.AuditLog != nil {
		var logReader *store.RequestLogReader
		if d.UsageReader != nil {
			// UsageReader와 같은 DB를 사용
			logReader = store.NewRequestLogReader(d.UsageReader.DB())
		}
		adminHandler = handler.NewAdminHandler(d.KeyStore, d.AuditLog, logReader, d.Logger)
	}

	// 클라이언트 키 관리 핸들러
	var clientKeyAdminHandler *handler.ClientKeyAdminHandler
	var clientKeyRotateHandler *handler.ClientKeyRotateHandler
	if d.ClientKeyStore != nil && d.AuditLog != nil {
		clientKeyAdminHandler = handler.NewClientKeyAdminHandler(d.ClientKeyStore, d.AuditLog, clientKeyValidator, d.Logger)
		clientKeyRotateHandler = handler.NewClientKeyRotateHandler(d.ClientKeyStore, d.AuditLog, clientKeyValidator, d.Logger)
	}

	// Usage Admin 핸들러 (아카이브, 리셋, 삭제)
	var usageAdminHandler *handler.UsageAdminHandler
	if d.UsageAdmin != nil && d.RetentionConfig != nil {
		usageAdminHandler = handler.NewUsageAdminHandler(d.UsageAdmin, d.RetentionConfig, d.Logger)
	}

	// 라우트 등록
	registerRoutes(engine, routesDeps{
		chatHandler:            chatHandler,
		healthHandler:          healthHandler,
		modelsHandler:          modelsHandler,
		streamHandler:          streamHandler,
		batchHandler:           batchHandler,
		usageHandler:           usageHandler,
		adminHandler:           adminHandler,
		usageAdminHandler:      usageAdminHandler,
		clientKeyAdminHandler:  clientKeyAdminHandler,
		clientKeyRotateHandler: clientKeyRotateHandler,
		trustedCIDRs:           d.Config.Auth.TrustedCIDRs,
		logger:                 d.Logger,
	})

	httpServer := &http.Server{
		Addr:         d.Config.Server.Addr(),
		Handler:      engine,
		ReadTimeout:  time.Duration(d.Config.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(d.Config.Server.WriteTimeoutSec) * time.Second,
	}

	return &Server{
		httpServer: httpServer,
		engine:     engine,
		logger:     d.Logger,
		cfg:        d.Config,
		logWriter:  d.LogWriter,
		consumer:   d.Consumer,
	}
}

// Start - 서버 시작
func (s *Server) Start() error {
	s.logger.Info("서버 시작", zap.String("addr", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("서버 시작 실패: %w", err)
	}
	return nil
}

// Shutdown - 서버 종료 (graceful)
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(s.cfg.Server.ShutdownTimeoutSec)*time.Second,
	)
	defer cancel()

	s.logger.Info("서버 종료 중...")

	if s.consumer != nil {
		s.consumer.Stop()
	}
	if s.logWriter != nil {
		s.logWriter.Close()
	}

	return s.httpServer.Shutdown(ctx)
}
