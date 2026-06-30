package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterUsageCaptureShareViewRoutes(r *gin.Engine, h *handler.Handlers) {
	r.GET("/s/:shareId", h.UsageCaptureShareView.Serve)
}
