package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/cache"
	"github.com/Chowonjae/ezai/internal/config"
	"github.com/Chowonjae/ezai/internal/middleware"
	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
	"github.com/Chowonjae/ezai/internal/router"
	"github.com/Chowonjae/ezai/internal/store"
)

// ChatHandler - 채팅 요청 핸들러
type ChatHandler struct {
	registry       *provider.Registry
	router         *router.Router
	promptManager  *config.PromptManager
	pricingManager *config.PricingManager
	loggingConfig  *config.LoggingConfig
	cache          *cache.Cache
	logger         *zap.Logger
	logWriter      *store.RequestLogWriter
	configDir      string
}

// SetRegistry - 레지스트리 설정 (api_key_id 조회용)
func (h *ChatHandler) SetRegistry(r *provider.Registry) {
	h.registry = r
}

// NewChatHandler - 채팅 핸들러 생성
func NewChatHandler(registry *provider.Registry, logger *zap.Logger) *ChatHandler {
	return &ChatHandler{
		registry: registry,
		logger:   logger,
	}
}

// SetRouter - 라우터 설정 (fallback/circuit breaker 활성화)
func (h *ChatHandler) SetRouter(r *router.Router) {
	h.router = r
}

// SetPromptManager - 프롬프트 매니저 설정
func (h *ChatHandler) SetPromptManager(pm *config.PromptManager) {
	h.promptManager = pm
}

// SetPricingManager - 가격 매니저 설정 (비용 자동 계산)
func (h *ChatHandler) SetPricingManager(pm *config.PricingManager) {
	h.pricingManager = pm
}

// SetCache - Redis 캐시 설정
func (h *ChatHandler) SetCache(c *cache.Cache) {
	h.cache = c
}

// SetLogWriter - 비동기 로그 기록기 설정
func (h *ChatHandler) SetLogWriter(w *store.RequestLogWriter) {
	h.logWriter = w
}

// SetLoggingConfig - 로깅 설정 (프라이버시/미리보기 길이 등)
func (h *ChatHandler) SetLoggingConfig(lc *config.LoggingConfig) {
	h.loggingConfig = lc
}

// SetConfigDir - 설정 디렉토리 경로 설정 (프로젝트별 fallback 로드용)
func (h *ChatHandler) SetConfigDir(dir string) {
	h.configDir = dir
}

// Chat - POST /chat
func (h *ChatHandler) Chat(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "잘못된 요청 형식: " + err.Error(),
		})
		return
	}

	traceID := middleware.GetTraceID(c)
	clientID := middleware.GetClientID(c)

	// 프롬프트 조합 (project/task가 지정된 경우)
	if h.promptManager != nil && (req.Project != "" || req.Task != "") {
		systemPrompt, err := h.promptManager.Build(req.Provider, req.Project, req.Task, req.PromptVars)
		if err == nil && systemPrompt != "" {
			// 기존 system 메시지 앞에 조합된 프롬프트 추가
			systemMsg := model.Message{Role: "system", Content: systemPrompt}
			req.Messages = append([]model.Message{systemMsg}, req.Messages...)
		}
	}

	// 프로젝트별 기본 fallback 적용 (요청에 fallback이 없고 project가 지정된 경우)
	if req.Project != "" && len(req.Fallback) == 0 && h.configDir != "" {
		if projCfg, err := config.LoadProjectFallback(h.configDir, req.Project); err == nil {
			for _, entry := range projCfg.Fallback.DefaultChain {
				// primary와 같은 프로바이더/모델은 건너뜀
				if entry.Provider == req.Provider && entry.Model == req.Model {
					continue
				}
				req.Fallback = append(req.Fallback, model.FallbackTarget{
					Provider: entry.Provider,
					Model:    entry.Model,
				})
			}
			if req.FallbackPolicy == "" && projCfg.Fallback.Policy != "" {
				req.FallbackPolicy = projCfg.Fallback.Policy
			}
		}
	}

	h.logger.Info("채팅 요청 수신",
		zap.String("trace_id", traceID),
		zap.String("provider", req.Provider),
		zap.String("model", req.Model),
	)

	// 캐시 조회 (스트리밍, Search Grounding은 캐시하지 않음)
	canCache := h.cache != nil && !req.Options.Stream && !req.Options.SearchGrounding
	if canCache {
		if cached, err := h.cache.Get(c.Request.Context(), &req); err == nil && cached != nil {
			cached.ID = traceID
			cached.Metadata.LatencyMs = 0
			h.logger.Info("캐시 히트",
				zap.String("trace_id", traceID),
				zap.String("provider", cached.Provider),
			)
			c.JSON(http.StatusOK, cached)
			return
		}
	}

	start := time.Now()
	var resp *model.ChatResponse
	var attempts []router.FallbackAttempt
	var err error

	if h.router != nil {
		// Router를 통한 실행 (fallback/circuit breaker/세마포어 적용)
		resp, attempts, err = h.router.Execute(c.Request.Context(), &req)
	} else {
		// Router 미설정: 직접 프로바이더 호출
		var p provider.Provider
		p, err = h.registry.Get(req.Provider)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		resp, err = p.Chat(c.Request.Context(), &req)
	}

	totalLatency := time.Since(start).Milliseconds()

	if err != nil {
		h.logger.Error("요청 실패",
			zap.String("trace_id", traceID),
			zap.Error(err),
		)
		h.writeLog(traceID, clientID, c.ClientIP(), &req, nil, attempts, totalLatency, err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "프로바이더 요청 실패: " + err.Error(),
		})
		return
	}

	resp.ID = traceID
	resp.Metadata.LatencyMs = totalLatency

	// 비용 자동 계산
	if h.pricingManager != nil {
		resp.Usage.EstimatedCost = h.pricingManager.Calculate(
			resp.Model, resp.Usage.InputTokens, resp.Usage.OutputTokens,
		)
	}

	h.logger.Info("채팅 응답 완료",
		zap.String("trace_id", traceID),
		zap.String("provider", resp.Provider),
		zap.String("model", resp.Model),
		zap.Int("input_tokens", resp.Usage.InputTokens),
		zap.Int("output_tokens", resp.Usage.OutputTokens),
		zap.Int64("latency_ms", totalLatency),
		zap.Bool("fallback_used", resp.Metadata.FallbackUsed),
	)

	h.writeLog(traceID, clientID, c.ClientIP(), &req, resp, attempts, totalLatency, nil)

	// 캐시 저장
	if canCache {
		_ = h.cache.Set(c.Request.Context(), &req, resp)
	}

	c.JSON(http.StatusOK, resp)
}

// writeLog - 요청 로그를 비동기로 기록
func (h *ChatHandler) writeLog(traceID, clientID, clientIP string, req *model.ChatRequest, resp *model.ChatResponse, attempts []router.FallbackAttempt, totalLatencyMs int64, reqErr error) {
	if h.logWriter == nil {
		return
	}

	inputLen, outputLen := loggingPreviewLengths(h.loggingConfig)

	log := &store.RequestLog{
		TraceID:           traceID,
		ClientID:          clientID,
		ClientIP:          clientIP,
		Project:           req.Project,
		Task:              req.Task,
		RequestedProvider: req.Provider,
		RequestedModel:    req.Model,
		PromptHash:        loggingHashPrompt(h.loggingConfig, req.Messages),
		InputPreview:      previewText(req.Messages, inputLen),
		LatencyMs:         totalLatencyMs,
		SearchGrounding:   req.Options.SearchGrounding,
	}

	// api_key_id 채우기
	if resp != nil && h.registry != nil {
		if keyID, ok := h.registry.GetKeyID(resp.Provider); ok && keyID > 0 {
			log.APIKeyID = keyID
		}
	}

	// 옵션 JSON
	if optJSON, err := json.Marshal(req.Options); err == nil {
		log.OptionsJSON = string(optJSON)
	}

	// fallback 시도 이력
	if len(attempts) > 0 {
		var chain []store.FallbackAttemptLog
		for _, a := range attempts {
			chain = append(chain, store.FallbackAttemptLog{
				Order: a.Order, Provider: a.Provider, Model: a.Model,
				Status: a.Status, Error: a.Error, LatencyMs: a.LatencyMs,
			})
		}
		log.FallbackChain = chain
	}

	if reqErr != nil {
		log.Status = "error"
		log.ErrorMessage = reqErr.Error()
	} else if resp != nil {
		log.Status = "success"
		log.ActualProvider = resp.Provider
		log.ActualModel = resp.Model
		log.InputTokens = resp.Usage.InputTokens
		log.OutputTokens = resp.Usage.OutputTokens
		log.CostUSD = resp.Usage.EstimatedCost
		log.FallbackUsed = resp.Metadata.FallbackUsed
		log.ProviderLatencyMs = totalLatencyMs
		log.OutputPreview = truncate(resp.Content, outputLen)
		if resp.Metadata.FallbackReason != nil {
			log.RoutingReason = *resp.Metadata.FallbackReason
		}
	}

	h.logWriter.Write(log)
}

// loggingPreviewLengths - 로깅 설정에서 미리보기 길이를 반환 (기본값 200)
func loggingPreviewLengths(lc *config.LoggingConfig) (inputLen, outputLen int) {
	if lc != nil {
		return lc.Logging.Record.InputPreviewLength, lc.Logging.Record.OutputPreviewLength
	}
	return 200, 200
}

// loggingHashPrompt - 설정에 따라 프롬프트 해시를 반환 (비활성화 시 빈 문자열)
func loggingHashPrompt(lc *config.LoggingConfig, messages []model.Message) string {
	if lc != nil && !lc.Logging.Privacy.HashPrompts {
		return ""
	}
	h := sha256.New()
	for _, m := range messages {
		h.Write([]byte(m.Role + ":" + m.Content))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// previewText - 마지막 사용자 메시지의 앞 maxLen자
func previewText(messages []model.Message, maxLen int) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return truncate(messages[i].Content, maxLen)
		}
	}
	return ""
}

// truncate - 문자열을 maxLen 길이로 자르기
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
