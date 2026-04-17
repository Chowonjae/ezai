package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
)

// ModelsHandler - 모델 목록 핸들러
type ModelsHandler struct {
	registry *provider.Registry
}

// NewModelsHandler - 모델 목록 핸들러 생성
func NewModelsHandler(registry *provider.Registry) *ModelsHandler {
	return &ModelsHandler{registry: registry}
}

// Models - GET /models
// 등록된 모든 프로바이더의 사용 가능한 모델 목록을 반환한다.
func (h *ModelsHandler) Models(c *gin.Context) {
	models := h.registry.AllModels()

	// 프로바이더 필터
	providerFilter := c.Query("provider")
	if providerFilter != "" {
		var filtered []model.ModelInfo
		for _, m := range models {
			if m.Provider == providerFilter {
				filtered = append(filtered, m)
			}
		}
		models = filtered
	}

	c.JSON(http.StatusOK, gin.H{
		"models": models,
	})
}
