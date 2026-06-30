package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const usageCaptureViewCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

type UsageCaptureViewHandler struct {
	usageCaptureService *service.UsageCaptureService
	signer              *service.UsageCapturePreviewSigner
}

func NewUsageCaptureViewHandler(cfg *config.Config, usageCaptureService *service.UsageCaptureService) *UsageCaptureViewHandler {
	secret := ""
	if cfg != nil {
		secret = cfg.JWT.Secret
	}
	return &UsageCaptureViewHandler{
		usageCaptureService: usageCaptureService,
		signer:              service.NewUsageCapturePreviewSigner(secret),
	}
}

func (h *UsageCaptureViewHandler) Serve(c *gin.Context) {
	requestID, apiKeyID, err := service.ParseUsageCapturePreviewParams(c.Query("request_id"), c.Query("api_key_id"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if h == nil || h.usageCaptureService == nil || h.signer == nil {
		response.Error(c, http.StatusServiceUnavailable, "usage capture not available")
		return
	}
	if !h.signer.Verify(c.Query("token"), requestID, apiKeyID, time.Now()) {
		c.Status(http.StatusForbidden)
		return
	}

	html, _, err := h.usageCaptureService.RenderHTML(c.Request.Context(), requestID, apiKeyID)
	if err != nil {
		if errors.Is(err, service.ErrUsageRequestCaptureNotFound) {
			response.NotFound(c, "capture not found or expired")
			return
		}
		response.InternalError(c, "failed to render usage capture")
		return
	}

	c.Header("Content-Security-Policy", usageCaptureViewCSP)
	c.Header("X-Frame-Options", "SAMEORIGIN")
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}
