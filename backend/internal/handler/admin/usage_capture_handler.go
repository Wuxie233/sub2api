package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// PreviewCapture renders the self-contained HTML viewer for one captured request's
// full conversation. The admin route group already enforces admin auth, so no
// per-handler auth check is needed here. No token is ever placed in the URL.
// GET /api/v1/admin/usage/captures/preview?request_id=...&api_key_id=...
func (h *UsageHandler) PreviewCapture(c *gin.Context) {
	requestID := strings.TrimSpace(c.Query("request_id"))
	if requestID == "" {
		response.BadRequest(c, "request_id is required")
		return
	}

	var apiKeyID *int64
	if raw := strings.TrimSpace(c.Query("api_key_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid api_key_id")
			return
		}
		apiKeyID = &id
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
