package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *GatewayHandler) writeModelsList(c *gin.Context, platform string, groupID *int64, modelIDs []string) {
	if platform != service.PlatformCodeBuddy {
		writeModelsList(c, platform, modelIDs)
		return
	}
	capabilities := h.gatewayService.GetCodeBuddyModelCapabilities(c.Request.Context(), groupID, modelIDs)
	type modelWithCapabilities struct {
		claude.Model
		service.CodeBuddyModelCapabilities
	}
	models := make([]modelWithCapabilities, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, modelWithCapabilities{
			Model: claude.Model{
				ID: modelID, Type: "model", DisplayName: modelID, CreatedAt: "2024-01-01T00:00:00Z",
			},
			CodeBuddyModelCapabilities: capabilities[modelID],
		})
	}
	writeModelsListResponse(c, models)
}
