package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/crypto"
	"github.com/Chowonjae/ezai/internal/middleware"
	"github.com/Chowonjae/ezai/internal/service"
	"github.com/Chowonjae/ezai/internal/store"
)

// ClientKeyRotateHandler - 클라이언트 셀프 로테이션 핸들러
type ClientKeyRotateHandler struct {
	store     *store.ClientKeyStore
	auditLog  *store.AuditLog
	validator *service.ClientKeyService
	logger    *zap.Logger
}

// NewClientKeyRotateHandler - 셀프 로테이션 핸들러 생성
func NewClientKeyRotateHandler(store *store.ClientKeyStore, auditLog *store.AuditLog, validator *service.ClientKeyService, logger *zap.Logger) *ClientKeyRotateHandler {
	return &ClientKeyRotateHandler{
		store:     store,
		auditLog:  auditLog,
		validator: validator,
		logger:    logger,
	}
}

// Rotate - POST /v1/keys/rotate
// 인증된 클라이언트가 자기 시크릿만 교체한다.
// client_id는 auth 미들웨어가 검증 후 context에 설정한 값을 사용한다.
func (h *ClientKeyRotateHandler) Rotate(c *gin.Context) {
	clientID := middleware.GetClientID(c)
	if clientID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "인증 정보가 없습니다"})
		return
	}

	// 현재 키 정보 조회 (만료일 확인용)
	existing, err := h.store.GetByClientID(clientID)
	if err != nil {
		h.logger.Error("키 조회 실패", zap.String("client_id", clientID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "키 조회 실패"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "키를 찾을 수 없습니다"})
		return
	}

	if !existing.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "비활성화된 키는 로테이션할 수 없습니다"})
		return
	}

	if expiresAt, err := time.Parse(time.RFC3339, existing.ExpiresAt); err == nil {
		if time.Now().UTC().After(expiresAt) {
			c.JSON(http.StatusForbidden, gin.H{"error": "만료된 키는 로테이션할 수 없습니다. 관리자에게 재발급을 요청하세요"})
			return
		}
	}

	// 새 시크릿 생성
	secret, err := crypto.GenerateClientSecret()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "시크릿 생성 실패"})
		return
	}

	secretHash, err := crypto.HashSecret(secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "시크릿 해싱 실패"})
		return
	}
	secretPrefix := secret[:12]

	// 시크릿만 교체 (만료일은 유지)
	if err := h.store.RotateSecret(clientID, secretHash, secretPrefix); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 캐시 무효화
	if h.validator != nil {
		h.validator.InvalidateCache(clientID)
	}

	if err := h.auditLog.Record(existing.ID, "client_key_rotated", clientID, "셀프 로테이션"); err != nil {
		h.logger.Warn("감사 로그 기록 실패", zap.Error(err))
	}

	h.logger.Info("클라이언트 키 셀프 로테이션",
		zap.String("client_id", clientID),
	)

	c.JSON(http.StatusOK, gin.H{
		"client_id":  clientID,
		"secret":     secret,
		"expires_at": existing.ExpiresAt,
		"warning":    "이 시크릿은 다시 확인할 수 없습니다. 안전한 곳에 보관하세요.",
	})
}
