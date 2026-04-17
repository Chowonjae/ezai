package handler

import (
	"net/http"

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
	if err != nil || existing == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "키 조회 실패"})
		return
	}

	// 새 시크릿 생성
	secret, err := crypto.GenerateClientSecret()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "시크릿 생성 실패"})
		return
	}

	secretHash := crypto.HashSecret(secret)
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

	_ = h.auditLog.Record(existing.ID, "client_key_rotated", clientID, "셀프 로테이션")

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
