package admin

import (
	"errors"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// PreviewCapture renders the self-contained HTML viewer for one captured request's
// full conversation. The admin route group already enforces admin auth, so no
// per-handler auth check is needed here. No token is ever placed in the URL.
// GET /api/v1/admin/usage/captures/preview?request_id=...&api_key_id=...
func (h *UsageHandler) PreviewCapture(c *gin.Context) {
	requestID, apiKeyID, err := service.ParseUsageCapturePreviewParams(c.Query("request_id"), c.Query("api_key_id"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if h.usageCaptureService == nil {
		response.Error(c, http.StatusServiceUnavailable, "usage capture not available")
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

	// Defense-in-depth headers. The viewer HTML already carries an inline meta CSP
	// that governs rendering when opened as a blob via authenticated XHR; these are
	// belt-and-suspenders for direct fetches.
	c.Header("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'")
	c.Header("X-Frame-Options", "SAMEORIGIN")
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

func (h *UsageHandler) PreviewCaptureLink(c *gin.Context) {
	requestID, apiKeyID, err := service.ParseUsageCapturePreviewParams(c.Query("request_id"), c.Query("api_key_id"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if h == nil || h.usageCaptureService == nil || h.previewSigner == nil {
		response.Error(c, http.StatusServiceUnavailable, "usage capture not available")
		return
	}
	if _, _, err := h.usageCaptureService.RenderHTML(c.Request.Context(), requestID, apiKeyID); err != nil {
		if errors.Is(err, service.ErrUsageRequestCaptureNotFound) {
			response.NotFound(c, "capture not found or expired")
			return
		}
		response.InternalError(c, "failed to render usage capture")
		return
	}
	previewURL, err := h.usageCapturePreviewURL(requestID, apiKeyID, time.Now())
	if err != nil {
		response.InternalError(c, "failed to create usage capture preview link")
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": previewURL})
}
