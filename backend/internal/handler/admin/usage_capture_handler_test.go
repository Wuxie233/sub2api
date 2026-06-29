package admin

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

// fakeUsageCaptureRepo is an in-memory service.UsageRequestCaptureRepository used
// to back a real *service.UsageCaptureService in handler tests.
type fakeUsageCaptureRepo struct {
	captures []*service.UsageRequestCapture
}

func (r *fakeUsageCaptureRepo) CreateBestEffort(_ context.Context, capture *service.UsageRequestCapture) error {
	cloned := *capture
	cloned.PayloadGzip = append([]byte(nil), capture.PayloadGzip...)
	r.captures = append(r.captures, &cloned)
	return nil
}

func (r *fakeUsageCaptureRepo) GetByRequestID(_ context.Context, requestID string, apiKeyID *int64) (*service.UsageRequestCapture, error) {
	for _, capture := range r.captures {
		if capture.RequestID == requestID && usageCaptureAPIKeyEqual(capture.APIKeyID, apiKeyID) {
			cloned := *capture
			cloned.PayloadGzip = append([]byte(nil), capture.PayloadGzip...)
			return &cloned, nil
		}
	}
	return nil, service.ErrUsageRequestCaptureNotFound
}

func (r *fakeUsageCaptureRepo) ExistsByRequestID(_ context.Context, requestID string, apiKeyID *int64) (bool, error) {
	for _, capture := range r.captures {
		if capture.RequestID == requestID && usageCaptureAPIKeyEqual(capture.APIKeyID, apiKeyID) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeUsageCaptureRepo) DeleteExpired(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, nil
}

func usageCaptureAPIKeyEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TestUsageHandlerPreviewCapture(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const requestID = "req_preview_1"
	apiKeyID := int64(7)

	repo := &fakeUsageCaptureRepo{}
	cfg := &config.Config{UsageCapture: config.UsageCaptureConfig{
		Enabled:        true,
		RetentionDays:  7,
		MaxRecordBytes: 1_000_000,
	}}
	captureSvc := service.NewUsageCaptureService(repo, cfg)
	require.NoError(t, captureSvc.Capture(context.Background(), service.CaptureInput{
		RequestID:    requestID,
		APIKeyID:     &apiKeyID,
		Method:       "POST",
		Path:         "/v1/messages",
		StatusCode:   200,
		RequestBody:  []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`),
		ResponseBody: []byte(`{"content":[{"type":"text","text":"hello"}]}`),
	}))

	handler := NewUsageHandler(nil, nil, nil, nil, captureSvc)
	router := gin.New()
	router.GET("/admin/usage/captures/preview", handler.PreviewCapture)

	tests := []struct {
		name        string
		query       string
		wantStatus  int
		wantContent string
		wantBody    string
	}{
		{
			name:        "existing capture renders viewer",
			query:       "request_id=" + requestID + "&api_key_id=7",
			wantStatus:  http.StatusOK,
			wantContent: "text/html",
			wantBody:    "EMBEDDED_TRACE_DATA",
		},
		{
			name:       "unknown request_id is not found",
			query:      "request_id=does_not_exist",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "empty request_id is bad request",
			query:      "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid api_key_id is bad request",
			query:      "request_id=" + requestID + "&api_key_id=notanumber",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/usage/captures/preview?"+tc.query, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantContent != "" {
				require.Contains(t, rec.Header().Get("Content-Type"), tc.wantContent)
			}
			if tc.wantBody != "" {
				require.Contains(t, rec.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestUsageHandlerPreviewCaptureServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewUsageHandler(nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/admin/usage/captures/preview", handler.PreviewCapture)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/captures/preview?request_id=req_x", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
