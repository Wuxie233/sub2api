package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterPulseRoutes(r *gin.Engine, h *handler.Handlers) {
	r.GET("/pulse", h.Pulse.ServePage)
	r.GET("/pulse/api/usage", h.Pulse.ServeUsage)
}
