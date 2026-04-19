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
	redis      *redis.Client
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
	LoggingConfig   *config.LoggingConfig
	ConfigDir       string
}

// New - 서버 생성
func New(d Deps) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	// 프록시 신뢰 비활성화: c.ClientIP()가 X-Forwarded-For를 무시하고 실제 원격 IP를 반환하도록 한다.
	// 리버스 프록시 뒤에서 운영할 경우, 프록시 IP 목록을 명시적으로 설정해야 한다.
	engine.SetTrustedProxies(nil)

	// ClientKeyValidator 구성
	var clientKeyValidator *service.ClientKeyService
	authCfg := middleware.AuthConfig{
		TrustedCIDRs: d.Config.Auth.TrustedCIDRs,
	}
	if d.ClientKeyStore != nil {
		clientKeyValidator = service.NewClientKeyService(d.ClientKeyStore, d.Redis)
		authCfg.Validator = clientKeyValidator
	}

	// 공통 미들웨어: Recovery → RequestID → BodyLimit → Logger
	engine.Use(
		middleware.Recovery(d.Logger),
		middleware.RequestID(),
		middleware.BodyLimit(10<<20), // 요청 body 최대 10MB
		middleware.Logger(d.Logger),
	)

	// Health 엔드포인트 - 인증 없이 접근 가능 (LB/K8s liveness probe용)
	healthHandler := handler.NewHealthHandler()
	engine.GET("/health", healthHandler.Health)

	// Rate Limiting → Auth 순서: 인증 실패 요청도 rate limit 적용 (DDoS 방어)
	if d.Redis != nil {
		engine.Use(middleware.RateLimit(d.Redis, middleware.RateLimitConfig{
			RequestsPerMinute: d.Config.Auth.RateLimitPerMinute,
		}, d.Logger))
	}

	// 인증 미들웨어 (이후 등록되는 라우트에만 적용)
	engine.Use(middleware.Auth(authCfg, d.Logger))

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
	if d.ConfigDir != "" {
		chatHandler.SetConfigDir(d.ConfigDir)
	}
	if d.LoggingConfig != nil {
		chatHandler.SetLoggingConfig(d.LoggingConfig)
	}

	modelsHandler := handler.NewModelsHandler(d.Registry)
	streamHandler := handler.NewStreamHandler(d.Registry, d.Logger)
	if d.Router != nil {
		streamHandler.SetRouter(d.Router)
	}
	if d.LogWriter != nil {
		streamHandler.SetLogWriter(d.LogWriter)
	}
	if d.PromptManager != nil {
		streamHandler.SetPromptManager(d.PromptManager)
	}
	if d.PricingManager != nil {
		streamHandler.SetPricingManager(d.PricingManager)
	}
	if d.LoggingConfig != nil {
		streamHandler.SetLoggingConfig(d.LoggingConfig)
	}
	if d.ConfigDir != "" {
		streamHandler.SetConfigDir(d.ConfigDir)
	}
	if d.Config.Server.StreamWriteTimeoutSec > 0 {
		streamHandler.SetStreamWriteTimeout(
			time.Duration(d.Config.Server.StreamWriteTimeoutSec) * time.Second,
		)
	}

	// Usage 핸들러
	var usageHandler *handler.UsageHandler
	if d.UsageReader != nil {
		usageHandler = handler.NewUsageHandler(d.UsageReader, d.PricingManager)
	}

	// 배치 핸들러 (Redis가 설정된 경우)
	var batchHandler *handler.BatchHandler
	if d.Producer != nil && d.JobStore != nil {
		batchHandler = handler.NewBatchHandler(d.Producer, d.JobStore, d.Logger)
		batchHandler.SetRegistry(d.Registry)
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

	// 요청 body 크기 제한 (기본 10MB)
	engine.MaxMultipartMemory = 10 << 20

	httpServer := &http.Server{
		Addr:           d.Config.Server.Addr(),
		Handler:        engine,
		ReadTimeout:    time.Duration(d.Config.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout:   time.Duration(d.Config.Server.WriteTimeoutSec) * time.Second,
		MaxHeaderBytes: 1 << 20, // 요청 헤더 최대 1MB
	}

	return &Server{
		httpServer: httpServer,
		engine:     engine,
		logger:     d.Logger,
		cfg:        d.Config,
		logWriter:  d.LogWriter,
		consumer:   d.Consumer,
		redis:      d.Redis,
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
// 순서: HTTP 서버 → consumer → logWriter → Redis
// HTTP 서버를 먼저 종료해야 진행 중 요청이 Redis/DB를 사용할 수 있다.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(s.cfg.Server.ShutdownTimeoutSec)*time.Second,
	)
	defer cancel()

	s.logger.Info("서버 종료 중...")

	var shutdownErr error

	// 1. HTTP 서버 종료 (새 요청 거부, 진행 중 요청 완료 대기)
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.Error("HTTP 서버 종료 실패", zap.Error(err))
		shutdownErr = err
	}

	// 2. 배치 consumer 종료 (진행 중 작업 완료 대기)
	if s.consumer != nil {
		s.consumer.Stop()
	}

	// 3. 비동기 로그 기록기 종료 (버퍼 flush)
	if s.logWriter != nil {
		s.logWriter.Close()
	}

	// 4. Redis 연결 종료 (모든 사용자가 종료된 후)
	if s.redis != nil {
		s.redis.Close()
	}

	return shutdownErr
}
