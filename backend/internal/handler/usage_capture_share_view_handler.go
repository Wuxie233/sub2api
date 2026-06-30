package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type UsageCaptureShareViewHandler struct {
	shareService *service.UsageCaptureShareService
}

func NewUsageCaptureShareViewHandler(shareService *service.UsageCaptureShareService) *UsageCaptureShareViewHandler {
	return &UsageCaptureShareViewHandler{shareService: shareService}
}

func (h *UsageCaptureShareViewHandler) Serve(c *gin.Context) {
	shareID := strings.TrimSpace(c.Param("shareId"))
	if shareID == "" {
		response.BadRequest(c, "share id is required")
		return
	}
	if h == nil || h.shareService == nil {
		response.Error(c, http.StatusServiceUnavailable, "usage capture share not available")
		return
	}

	html, err := h.shareService.ResolvePublic(c.Request.Context(), shareID)
	if err != nil {
		if errors.Is(err, service.ErrUsageRequestCaptureShareNotFound) {
			response.NotFound(c, "share not found, expired, or revoked")
			return
		}
		response.InternalError(c, "failed to render usage capture share")
		return
	}

	c.Header("Content-Security-Policy", usageCaptureViewCSP)
	c.Header("X-Frame-Options", "SAMEORIGIN")
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}
