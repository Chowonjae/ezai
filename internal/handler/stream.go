package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/config"
	"github.com/Chowonjae/ezai/internal/middleware"
	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
	"github.com/Chowonjae/ezai/internal/router"
	"github.com/Chowonjae/ezai/internal/store"
)

// StreamHandler - SSE 스트리밍 핸들러
type StreamHandler struct {
	registry            *provider.Registry
	router              *router.Router
	promptManager       *config.PromptManager
	pricingManager      *config.PricingManager
	loggingConfig       *config.LoggingConfig
	logger              *zap.Logger
	logWriter           *store.RequestLogWriter
	configDir           string
	streamWriteTimeout  time.Duration // SSE 스트리밍 전용 WriteDeadline (기본 10분)
}

// NewStreamHandler - 스트리밍 핸들러 생성
func NewStreamHandler(registry *provider.Registry, logger *zap.Logger) *StreamHandler {
	return &StreamHandler{
		registry: registry,
		logger:   logger,
	}
}

// SetRouter - 라우터 설정 (fallback/circuit breaker 활성화)
func (h *StreamHandler) SetRouter(r *router.Router) {
	h.router = r
}

// SetPromptManager - 프롬프트 매니저 설정
func (h *StreamHandler) SetPromptManager(pm *config.PromptManager) {
	h.promptManager = pm
}

// SetPricingManager - 가격 매니저 설정
func (h *StreamHandler) SetPricingManager(pm *config.PricingManager) {
	h.pricingManager = pm
}

// SetLoggingConfig - 로깅 설정 (프라이버시/미리보기 길이 등)
func (h *StreamHandler) SetLoggingConfig(lc *config.LoggingConfig) {
	h.loggingConfig = lc
}

// SetLogWriter - 비동기 로그 기록기 설정
func (h *StreamHandler) SetLogWriter(w *store.RequestLogWriter) {
	h.logWriter = w
}

// SetConfigDir - 설정 디렉토리 설정 (프로젝트별 fallback용)
func (h *StreamHandler) SetConfigDir(dir string) {
	h.configDir = dir
}

// SetStreamWriteTimeout - SSE 스트리밍 전용 WriteDeadline 설정
func (h *StreamHandler) SetStreamWriteTimeout(d time.Duration) {
	h.streamWriteTimeout = d
}

// Stream - POST /chat/stream
// SSE(Server-Sent Events)로 스트리밍 응답을 반환한다.
// Router가 설정된 경우 Circuit Breaker/세마포어/Fallback을 적용한다.
func (h *StreamHandler) Stream(c *gin.Context) {
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

	traceID := middleware.GetTraceID(c)
	clientID := middleware.GetClientID(c)
	start := time.Now()

	// 프롬프트 조합 (project/task가 지정된 경우)
	if h.promptManager != nil && (req.Project != "" || req.Task != "") {
		systemPrompt, err := h.promptManager.Build(req.Provider, req.Project, req.Task, req.PromptVars)
		if err != nil {
			h.logger.Warn("프롬프트 빌드 실패",
				zap.String("project", req.Project), zap.String("task", req.Task), zap.Error(err))
		} else if systemPrompt != "" {
			systemMsg := model.Message{Role: "system", Content: systemPrompt}
			req.Messages = append([]model.Message{systemMsg}, req.Messages...)
		}
	}

	// 프로젝트별 기본 fallback 적용 (요청에 fallback이 없고 project가 지정된 경우)
	if req.Project != "" && len(req.Fallback) == 0 && h.configDir != "" {
		projCfg, err := config.LoadProjectFallback(h.configDir, req.Project)
		if err != nil {
			h.logger.Warn("프로젝트 fallback 설정 로드 실패",
				zap.String("project", req.Project), zap.Error(err))
		} else if projCfg != nil {
			for _, entry := range projCfg.Fallback.DefaultChain {
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

	h.logger.Info("스트리밍 요청 수신",
		zap.String("trace_id", traceID),
		zap.String("provider", req.Provider),
		zap.String("model", req.Model),
	)

	// 스트리밍 대상 결정 + Fallback 시도
	var ch <-chan model.StreamChunk
	var actualProvider, actualModel string
	var attempts []router.FallbackAttempt
	var fallbackUsed bool
	var fallbackReason string

	if h.router != nil {
		targets := h.router.BuildTargets(&req)
		if len(targets) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "사용 가능한 프로바이더가 없습니다",
			})
			return
		}
		var lastErr error

		for i, target := range targets {
			attempt := router.FallbackAttempt{
				Order:    i + 1,
				Provider: target.Provider,
				Model:    target.Model,
			}

			// Circuit Breaker 확인
			if err := h.router.CheckCircuitBreaker(target.Provider); err != nil {
				attempt.Status = "circuit_open"
				attempt.Error = err.Error()
				attempts = append(attempts, attempt)
				lastErr = err
				continue
			}

			// 세마포어 획득
			if err := h.router.AcquireSemaphore(c.Request.Context(), target.Provider); err != nil {
				attempt.Status = "timeout"
				attempt.Error = "세마포어 획득 타임아웃"
				attempts = append(attempts, attempt)
				lastErr = err
				continue
			}

			// 프로바이더 조회
			p, err := h.registry.Get(target.Provider)
			if err != nil {
				h.router.ReleaseSemaphore(target.Provider)
				attempt.Status = "provider_not_found"
				attempt.Error = err.Error()
				attempts = append(attempts, attempt)
				lastErr = err
				continue
			}

			// 스트리밍은 장시간 실행되므로 프로바이더 타임아웃을 적용하지 않는다
			streamCtx := c.Request.Context()

			// 스트리밍 시작 시도
			provReq := req
			provReq.Provider = target.Provider
			provReq.Model = target.Model

			streamStart := time.Now()
			streamCh, err := p.ChatStream(streamCtx, &provReq)
			attempt.LatencyMs = time.Since(streamStart).Milliseconds()

			if err != nil {
				h.router.ReleaseSemaphore(target.Provider)
				h.router.RecordFailure(target.Provider)
				attempt.Status = "error"
				attempt.Error = err.Error()
				attempts = append(attempts, attempt)
				lastErr = err
				continue
			}

			// 스트리밍 연결 성공 (CB 결과는 스트림 완료 후 기록)
			attempt.Status = "success"
			attempts = append(attempts, attempt)

			ch = streamCh
			actualProvider = target.Provider
			actualModel = target.Model

			if i > 0 && lastErr != nil {
				fallbackUsed = true
				fallbackReason = fmt.Sprintf("fallback:stream - %s", lastErr.Error())
			}

			// 세마포어는 스트리밍 완료 후 해제
			defer h.router.ReleaseSemaphore(target.Provider)
			break
		}

		if ch == nil {
			// fallback 대상이 있었지만 전부 실패한 경우 기록
			if len(attempts) > 1 {
				fallbackUsed = true
				fallbackReason = "fallback:stream:all_failed"
			}
			h.writeStreamLog(traceID, clientID, c.ClientIP(), &req, "", "", nil, "", attempts, fallbackUsed, fallbackReason, time.Since(start).Milliseconds(), lastErr)
			c.JSON(http.StatusBadGateway, gin.H{
				"error":    "모든 프로바이더 스트리밍 실패",
				"trace_id": traceID,
			})
			return
		}
	} else {
		// Router 미설정: 직접 프로바이더 호출
		p, err := h.registry.Get(req.Provider)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		streamCh, err := p.ChatStream(c.Request.Context(), &req)
		if err != nil {
			h.logger.Error("스트리밍 시작 실패", zap.String("trace_id", traceID), zap.Error(err))
			c.JSON(http.StatusBadGateway, gin.H{
				"error":    "스트리밍 시작 실패",
				"trace_id": traceID,
			})
			return
		}

		ch = streamCh
		actualProvider = req.Provider
		actualModel = req.Model
	}

	// SSE 스트리밍 전용 WriteDeadline 연장
	// 글로벌 WriteTimeout(120s)은 동기 응답용이므로, 스트리밍은 별도 연장한다.
	if h.streamWriteTimeout > 0 {
		rc := http.NewResponseController(c.Writer)
		if err := rc.SetWriteDeadline(time.Now().Add(h.streamWriteTimeout)); err != nil {
			h.logger.Warn("스트리밍 WriteDeadline 연장 실패 (기존 타임아웃으로 동작)",
				zap.Error(err), zap.Duration("timeout", h.streamWriteTimeout))
		}
	}

	// SSE 헤더 설정
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Trace-ID", traceID)

	// 사용량 추적
	var finalUsage *model.UsageInfo
	var fullContent strings.Builder
	var streamErrorMsg string // 스트림 도중 에러 메시지 (빈 문자열 = 에러 없음)

	// SSE 이벤트 전송
	c.Stream(func(w io.Writer) bool {
		chunk, ok := <-ch
		if !ok {
			// 채널 닫힘
			fmt.Fprintf(w, "data: [DONE]\n\n")
			c.Writer.Flush()
			return false
		}

		if chunk.Error != nil {
			// 에러 이벤트
			streamErrorMsg = *chunk.Error
			errData, _ := json.Marshal(gin.H{"error": *chunk.Error})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errData)
			c.Writer.Flush()
			return false
		}

		if chunk.Done {
			// 최종 이벤트 (사용량 포함)
			if chunk.Usage != nil {
				finalUsage = chunk.Usage
				doneData, _ := json.Marshal(gin.H{"content": "", "done": true, "usage": chunk.Usage})
				fmt.Fprintf(w, "data: %s\n\n", doneData)
			} else {
				doneData, _ := json.Marshal(gin.H{"content": "", "done": true})
				fmt.Fprintf(w, "data: %s\n\n", doneData)
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			c.Writer.Flush()
			return false
		}

		// 텍스트 청크 이벤트 (done:false 포함)
		fullContent.WriteString(chunk.Content)
		chunkData, _ := json.Marshal(gin.H{"content": chunk.Content, "done": false})
		fmt.Fprintf(w, "data: %s\n\n", chunkData)
		c.Writer.Flush()
		return true
	})

	totalLatency := time.Since(start).Milliseconds()

	// 스트림 완료 후 Circuit Breaker 결과 기록
	if h.router != nil && actualProvider != "" {
		if streamErrorMsg != "" {
			h.router.RecordFailure(actualProvider)
		} else {
			h.router.RecordSuccess(actualProvider)
		}
	}

	// 비용 자동 계산
	if h.pricingManager != nil && finalUsage != nil {
		finalUsage.EstimatedCost = h.pricingManager.Calculate(
			actualModel, finalUsage.InputTokens, finalUsage.OutputTokens,
		)
	}

	h.logger.Info("스트리밍 완료",
		zap.String("trace_id", traceID),
		zap.String("provider", actualProvider),
		zap.String("model", actualModel),
		zap.Int64("latency_ms", totalLatency),
		zap.Bool("fallback_used", fallbackUsed),
	)

	// 비동기 로그 기록
	var logErr error
	if streamErrorMsg != "" {
		logErr = fmt.Errorf("스트림 에러: %s", streamErrorMsg)
	}
	h.writeStreamLog(traceID, clientID, c.ClientIP(), &req, actualProvider, actualModel, finalUsage, fullContent.String(), attempts, fallbackUsed, fallbackReason, totalLatency, logErr)
}

// writeStreamLog - 스트리밍 요청 로그를 비동기로 기록
func (h *StreamHandler) writeStreamLog(traceID, clientID, clientIP string, req *model.ChatRequest, actualProvider, actualModel string, usage *model.UsageInfo, outputContent string, attempts []router.FallbackAttempt, fallbackUsed bool, fallbackReason string, totalLatencyMs int64, reqErr error) {
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
		ActualProvider:    actualProvider,
		ActualModel:       actualModel,
		FallbackUsed:      fallbackUsed,
	}

	if fallbackReason != "" {
		log.RoutingReason = fallbackReason
	}

	// api_key_id 채우기
	if actualProvider != "" && h.registry != nil {
		if keyID, ok := h.registry.GetKeyID(actualProvider); ok && keyID > 0 {
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
	} else {
		log.Status = "success"
		if usage != nil {
			log.InputTokens = usage.InputTokens
			log.OutputTokens = usage.OutputTokens
			log.CostUSD = usage.EstimatedCost
		}
		log.OutputPreview = truncate(outputContent, outputLen)
		log.ProviderLatencyMs = totalLatencyMs
	}

	h.logWriter.Write(log)
}
