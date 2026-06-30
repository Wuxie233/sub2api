package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUsageCaptureShareViewHandlerServe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	captureRepo := &usageCaptureViewRepoFake{}
	captureSvc := service.NewUsageCaptureService(captureRepo, &config.Config{UsageCapture: config.UsageCaptureConfig{Enabled: true, RetentionDays: 7, MaxRecordBytes: 1_000_000}})
	require.NoError(t, captureSvc.Capture(context.Background(), service.CaptureInput{
		RequestID:    "req_public_share",
		Method:       "POST",
		Path:         "/v1/messages",
		StatusCode:   200,
		RequestBody:  []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseBody: []byte(`{"content":[{"type":"text","text":"hello"}]}`),
	}))
	shareRepo := &usageCaptureShareViewRepoFake{}
	shareSvc := service.NewUsageCaptureShareService(shareRepo, captureSvc)
	active, err := shareSvc.CreateShare(context.Background(), service.CreateShareInput{RequestID: "req_public_share"})
	require.NoError(t, err)
	revoked, err := shareSvc.CreateShare(context.Background(), service.CreateShareInput{RequestID: "req_public_share"})
	require.NoError(t, err)
	require.NoError(t, shareSvc.RevokeShare(context.Background(), revoked.ID))
	expiredAt := time.Now().Add(-time.Minute)
	expired, err := shareSvc.CreateShare(context.Background(), service.CreateShareInput{RequestID: "req_public_share", ExpiresAt: &expiredAt})
	require.NoError(t, err)

	handler := NewUsageCaptureShareViewHandler(shareSvc)
	router := gin.New()
	router.GET("/s/:shareId", handler.Serve)

	tests := []struct {
		name       string
		shareID    string
		wantStatus int
		wantBody   string
	}{
		{name: "active share renders html", shareID: active.ShareID, wantStatus: http.StatusOK, wantBody: "EMBEDDED_TRACE_DATA"},
		{name: "unknown share is not found", shareID: "unknown", wantStatus: http.StatusNotFound},
		{name: "revoked share is not found", shareID: revoked.ShareID, wantStatus: http.StatusNotFound},
		{name: "expired share is not found", shareID: expired.ShareID, wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/s/"+tt.shareID, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				require.Contains(t, rec.Body.String(), tt.wantBody)
				require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
				require.Equal(t, usageCaptureViewCSP, rec.Header().Get("Content-Security-Policy"))
				require.Equal(t, "SAMEORIGIN", rec.Header().Get("X-Frame-Options"))
			}
		})
	}
}

type usageCaptureShareViewRepoFake struct {
	shares []*service.UsageRequestCaptureShare
}

func (r *usageCaptureShareViewRepoFake) Create(_ context.Context, share *service.UsageRequestCaptureShare) error {
	if share.ID == 0 {
		share.ID = int64(len(r.shares) + 1)
	}
	if share.CreatedAt.IsZero() {
		share.CreatedAt = time.Now()
	}
	cloned := *share
	r.shares = append(r.shares, &cloned)
	return nil
}

func (r *usageCaptureShareViewRepoFake) GetByShareID(_ context.Context, shareID string) (*service.UsageRequestCaptureShare, error) {
	for _, share := range r.shares {
		if share.ShareID == shareID {
			cloned := *share
			return &cloned, nil
		}
	}
	return nil, service.ErrUsageRequestCaptureShareNotFound
}

func (r *usageCaptureShareViewRepoFake) List(_ context.Context, _ service.ShareListFilter, _, _ int) ([]*service.UsageRequestCaptureShare, int64, error) {
	return nil, 0, nil
}

func (r *usageCaptureShareViewRepoFake) Revoke(_ context.Context, id int64, revokedAt time.Time) error {
	for _, share := range r.shares {
		if share.ID == id {
			share.RevokedAt = &revokedAt
			return nil
		}
	}
	return service.ErrUsageRequestCaptureShareNotFound
}

func (r *usageCaptureShareViewRepoFake) IncrementView(_ context.Context, shareID string, viewedAt time.Time) error {
	for _, share := range r.shares {
		if share.ShareID == shareID {
			share.ViewCount++
			share.LastViewedAt = &viewedAt
			return nil
		}
	}
	return service.ErrUsageRequestCaptureShareNotFound
}
