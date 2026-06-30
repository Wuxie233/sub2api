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

const usageCaptureViewTestSecret = "test-jwt-secret-32-byte-minimum-value"

type usageCaptureViewRepoFake struct {
	captures []*service.UsageRequestCapture
}

func (r *usageCaptureViewRepoFake) CreateBestEffort(_ context.Context, capture *service.UsageRequestCapture) error {
	cloned := *capture
	cloned.PayloadGzip = append([]byte(nil), capture.PayloadGzip...)
	r.captures = append(r.captures, &cloned)
	return nil
}

func (r *usageCaptureViewRepoFake) GetByRequestID(_ context.Context, requestID string, apiKeyID *int64) (*service.UsageRequestCapture, error) {
	for _, capture := range r.captures {
		if capture.RequestID == requestID && usageCaptureViewAPIKeyIDEqual(capture.APIKeyID, apiKeyID) {
			cloned := *capture
			cloned.PayloadGzip = append([]byte(nil), capture.PayloadGzip...)
			return &cloned, nil
		}
	}
	return nil, service.ErrUsageRequestCaptureNotFound
}

func (r *usageCaptureViewRepoFake) ExistsByRequestID(_ context.Context, requestID string, apiKeyID *int64) (bool, error) {
	for _, capture := range r.captures {
		if capture.RequestID == requestID && usageCaptureViewAPIKeyIDEqual(capture.APIKeyID, apiKeyID) {
			return true, nil
		}
	}
	return false, nil
}

func (r *usageCaptureViewRepoFake) DeleteExpired(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, nil
}

func usageCaptureViewAPIKeyIDEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func newUsageCaptureViewTestService(t *testing.T, requestID string, apiKeyID *int64) *service.UsageCaptureService {
	t.Helper()
	repo := &usageCaptureViewRepoFake{}
	cfg := &config.Config{UsageCapture: config.UsageCaptureConfig{
		Enabled:        true,
		RetentionDays:  7,
		MaxRecordBytes: 1_000_000,
	}}
	captureSvc := service.NewUsageCaptureService(repo, cfg)
	require.NoError(t, captureSvc.Capture(context.Background(), service.CaptureInput{
		RequestID:    requestID,
		APIKeyID:     apiKeyID,
		Method:       "POST",
		Path:         "/v1/messages",
		StatusCode:   200,
		RequestBody:  []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`),
		ResponseBody: []byte(`{"content":[{"type":"text","text":"hello"}]}`),
	}))
	return captureSvc
}

func TestUsageCaptureViewHandlerServe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const requestID = "req_capture_view_1"
	apiKeyID := int64(12)
	captureSvc := newUsageCaptureViewTestService(t, requestID, &apiKeyID)
	signer := service.NewUsageCapturePreviewSigner(usageCaptureViewTestSecret)
	validToken := signer.Sign(service.UsageCapturePreviewClaims{
		RequestID: requestID,
		APIKeyID:  &apiKeyID,
		ExpiresAt: time.Now().Add(service.UsageCapturePreviewTokenTTL),
	})
	expiredToken := signer.Sign(service.UsageCapturePreviewClaims{
		RequestID: requestID,
		APIKeyID:  &apiKeyID,
		ExpiresAt: time.Now().Add(-time.Minute),
	})

	handler := NewUsageCaptureViewHandler(&config.Config{JWT: config.JWTConfig{Secret: usageCaptureViewTestSecret}}, captureSvc)
	router := gin.New()
	router.GET("/usage-capture-view", handler.Serve)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid token renders html",
			query:      "request_id=" + requestID + "&api_key_id=12&token=" + validToken,
			wantStatus: http.StatusOK,
			wantBody:   "EMBEDDED_TRACE_DATA",
		},
		{
			name:       "bad token is forbidden",
			query:      "request_id=" + requestID + "&api_key_id=12&token=bad",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "expired token is forbidden",
			query:      "request_id=" + requestID + "&api_key_id=12&token=" + expiredToken,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing capture is not found",
			query:      "request_id=missing&api_key_id=12&token=" + signer.Sign(service.UsageCapturePreviewClaims{RequestID: "missing", APIKeyID: &apiKeyID, ExpiresAt: time.Now().Add(service.UsageCapturePreviewTokenTTL)}),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/usage-capture-view?"+tt.query, nil)
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

func TestUsageCaptureViewHandlerServeRejectsTokenForDifferentBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const requestID = "req_capture_view_binding"
	apiKeyID := int64(12)
	otherAPIKeyID := int64(13)
	captureSvc := newUsageCaptureViewTestService(t, requestID, &apiKeyID)
	signer := service.NewUsageCapturePreviewSigner(usageCaptureViewTestSecret)
	token := signer.Sign(service.UsageCapturePreviewClaims{
		RequestID: requestID,
		APIKeyID:  &otherAPIKeyID,
		ExpiresAt: time.Now().Add(service.UsageCapturePreviewTokenTTL),
	})

	handler := NewUsageCaptureViewHandler(&config.Config{JWT: config.JWTConfig{Secret: usageCaptureViewTestSecret}}, captureSvc)
	router := gin.New()
	router.GET("/usage-capture-view", handler.Serve)

	req := httptest.NewRequest(http.MethodGet, "/usage-capture-view?request_id="+requestID+"&api_key_id=12&token="+token, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}
