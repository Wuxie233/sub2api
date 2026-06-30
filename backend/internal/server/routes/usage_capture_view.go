package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterUsageCaptureViewRoutes(r *gin.Engine, h *handler.Handlers) {
	r.GET("/usage-capture-view", h.UsageCaptureView.Serve)
}
