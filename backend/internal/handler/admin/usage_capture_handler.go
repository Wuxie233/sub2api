package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type createCaptureShareRequest struct {
	RequestID     string  `json:"request_id"`
	APIKeyID      *int64  `json:"api_key_id"`
	ExpiresInDays *int    `json:"expires_in_days"`
	Label         *string `json:"label"`
}

type captureShareCreateResponse struct {
	ShareID   string     `json:"share_id"`
	Path      string     `json:"path"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

type captureShareListItem struct {
	ID           int64      `json:"id"`
	ShareID      string     `json:"share_id"`
	Path         string     `json:"path"`
	RequestID    string     `json:"request_id"`
	APIKeyID     *int64     `json:"api_key_id"`
	Label        *string    `json:"label"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	CreatedAt    time.Time  `json:"created_at"`
	ViewCount    int        `json:"view_count"`
	LastViewedAt *time.Time `json:"last_viewed_at"`
}

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

func (h *UsageHandler) CreateCaptureShare(c *gin.Context) {
	if h == nil || h.shareService == nil {
		response.Error(c, http.StatusServiceUnavailable, "usage capture share not available")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}

	var req createCaptureShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		response.BadRequest(c, "request_id is required")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresInDays != nil && *req.ExpiresInDays > 0 {
		value := time.Now().Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &value
	}
	createdBy := subject.UserID
	executeAdminIdempotentJSON(c, "admin.usage.capture_shares.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		share, err := h.shareService.CreateShare(ctx, service.CreateShareInput{
			RequestID: req.RequestID,
			APIKeyID:  req.APIKeyID,
			CreatedBy: &createdBy,
			Label:     req.Label,
			ExpiresAt: expiresAt,
		})
		if err != nil {
			return nil, err
		}
		return captureShareCreateResponse{
			ShareID:   share.ShareID,
			Path:      usageCaptureSharePath(share.ShareID),
			ExpiresAt: share.ExpiresAt,
			CreatedAt: share.CreatedAt,
		}, nil
	})
}

func (h *UsageHandler) ListCaptureShares(c *gin.Context) {
	if h == nil || h.shareService == nil {
		response.Error(c, http.StatusServiceUnavailable, "usage capture share not available")
		return
	}
	page, pageSize := response.ParsePagination(c)
	shares, total, err := h.shareService.ListShares(c.Request.Context(), service.ShareListFilter{RequestID: strings.TrimSpace(c.Query("request_id"))}, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	now := time.Now()
	items := make([]captureShareListItem, 0, len(shares))
	for _, share := range shares {
		items = append(items, captureShareListItemFromService(share, now))
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *UsageHandler) RevokeCaptureShare(c *gin.Context) {
	if h == nil || h.shareService == nil {
		response.Error(c, http.StatusServiceUnavailable, "usage capture share not available")
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid share id")
		return
	}
	if err := h.shareService.RevokeShare(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "status": "revoked"})
}

func captureShareListItemFromService(share *service.UsageRequestCaptureShare, now time.Time) captureShareListItem {
	return captureShareListItem{
		ID:           share.ID,
		ShareID:      share.ShareID,
		Path:         usageCaptureSharePath(share.ShareID),
		RequestID:    share.RequestID,
		APIKeyID:     share.APIKeyID,
		Label:        share.Label,
		Status:       service.ShareStatus(share, now),
		ExpiresAt:    share.ExpiresAt,
		RevokedAt:    share.RevokedAt,
		CreatedAt:    share.CreatedAt,
		ViewCount:    share.ViewCount,
		LastViewedAt: share.LastViewedAt,
	}
}

func usageCaptureSharePath(shareID string) string {
	return "/s/" + shareID
}
