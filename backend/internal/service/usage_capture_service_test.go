package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/web/claudetap"
	"github.com/stretchr/testify/require"
)

type usageCaptureFakeRepo struct {
	mu               sync.Mutex
	captures         []*UsageRequestCapture
	createCalled     int
	createErr        error
	deleteResults    []int
	deleteErr        error
	deleteCalls      int
	deleteBatchSizes []int
}

func (r *usageCaptureFakeRepo) CreateBestEffort(ctx context.Context, capture *UsageRequestCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalled++
	if r.createErr != nil {
		return r.createErr
	}
	cloned := *capture
	cloned.PayloadGzip = append([]byte(nil), capture.PayloadGzip...)
	r.captures = append(r.captures, &cloned)
	return nil
}

func (r *usageCaptureFakeRepo) GetByRequestID(ctx context.Context, requestID string, apiKeyID *int64) (*UsageRequestCapture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, capture := range r.captures {
		if capture.RequestID == requestID && usageCaptureTestAPIKeyMatches(capture.APIKeyID, apiKeyID) {
			cloned := *capture
			cloned.PayloadGzip = append([]byte(nil), capture.PayloadGzip...)
			return &cloned, nil
		}
	}
	return nil, ErrUsageRequestCaptureNotFound
}

func (r *usageCaptureFakeRepo) ExistsByRequestID(ctx context.Context, requestID string, apiKeyID *int64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, capture := range r.captures {
		if capture.RequestID == requestID && usageCaptureTestAPIKeyMatches(capture.APIKeyID, apiKeyID) {
			return true, nil
		}
	}
	return false, nil
}

func (r *usageCaptureFakeRepo) DeleteExpired(ctx context.Context, before time.Time, batchSize int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	r.deleteBatchSizes = append(r.deleteBatchSizes, batchSize)
	if r.deleteErr != nil {
		return 0, r.deleteErr
	}
	if len(r.deleteResults) == 0 {
		return 0, nil
	}
	deleted := r.deleteResults[0]
	r.deleteResults = r.deleteResults[1:]
	return deleted, nil
}

func TestUsageCaptureServiceRoundTrip(t *testing.T) {
	repo := &usageCaptureFakeRepo{}
	svc := NewUsageCaptureService(repo, usageCaptureTestConfig(true, 7, 1_000_000))
	timestamp := time.Date(2026, 6, 30, 12, 10, 5, 0, time.FixedZone("UTC+8", 8*60*60))
	apiKeyID := int64(42)
	usageLogID := int64(100)
	userID := int64(7)
	accountID := int64(9)

	input := CaptureInput{
		RequestID:  "req_roundtrip",
		APIKeyID:   &apiKeyID,
		UsageLogID: &usageLogID,
		UserID:     &userID,
		AccountID:  &accountID,
		Provider:   "anthropic",
		Model:      "claude-sonnet-4-5",
		Endpoint:   "/v1/messages",
		Stream:     true,
		StatusCode: 200,
		DurationMs: 1234,
		Method:     "POST",
		Path:       "/v1/messages",
		RequestHeaders: map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer secret",
		},
		ResponseHeaders: map[string]string{"Content-Type": "application/json"},
		RequestBody:     []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`),
		ResponseBody:    []byte(`{"content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":2,"output_tokens":1}}`),
		Timestamp:       timestamp,
	}

	require.NoError(t, svc.Capture(context.Background(), input))
	require.Len(t, repo.captures, 1)

	record, capture, err := svc.Get(context.Background(), input.RequestID, &apiKeyID)
	require.NoError(t, err)
	require.Equal(t, input.RequestID, record.RequestID)
	require.Equal(t, 1, record.Turn)
	require.Equal(t, input.DurationMs, record.DurationMs)
	require.Equal(t, timestamp.UTC().Format(time.RFC3339), record.Timestamp)
	require.Equal(t, "POST", record.Request.Method)
	require.Equal(t, "/v1/messages", record.Request.Path)
	require.JSONEq(t, string(input.RequestBody), string(record.Request.Body))
	require.JSONEq(t, string(input.ResponseBody), string(record.Response.Body))
	require.Equal(t, int64(len(input.RequestBody)), capture.RequestBytes)
	require.Equal(t, int64(len(input.ResponseBody)), capture.ResponseBytes)
	require.False(t, capture.Truncated)
	require.Nil(t, capture.TruncateReason)
	require.Equal(t, usageCaptureSchemaVersion, capture.CaptureSchemaVersion)
	require.Equal(t, usageLogID, *capture.UsageLogID)
	require.Equal(t, userID, *capture.UserID)
	require.Equal(t, accountID, *capture.AccountID)
}

func TestUsageCaptureServiceGzipValidAndCompresses(t *testing.T) {
	repo := &usageCaptureFakeRepo{}
	svc := NewUsageCaptureService(repo, usageCaptureTestConfig(true, 7, 1_000_000))
	body := []byte(`{"text":"` + strings.Repeat("compressible", 2000) + `"}`)

	require.NoError(t, svc.Capture(context.Background(), CaptureInput{
		RequestID:    "req_gzip",
		Method:       "POST",
		Path:         "/v1/messages",
		StatusCode:   200,
		RequestBody:  body,
		ResponseBody: body,
	}))
	require.Len(t, repo.captures, 1)
	capture := repo.captures[0]
	decoded := usageCaptureTestGunzip(t, capture.PayloadGzip)
	require.JSONEq(t, string(decoded), string(decoded))
	require.Less(t, capture.CompressedBytes, int64(len(decoded)))
}

func TestUsageCaptureServiceByteCapTruncatesAndStaysNearCap(t *testing.T) {
	repo := &usageCaptureFakeRepo{}
	svc := NewUsageCaptureService(repo, usageCaptureTestConfig(true, 7, 650))

	require.NoError(t, svc.Capture(context.Background(), CaptureInput{
		RequestID:    "req_truncated",
		Method:       "POST",
		Path:         "/v1/messages",
		StatusCode:   200,
		RequestBody:  []byte(`{"prompt":"` + strings.Repeat("a", 2000) + `"}`),
		ResponseBody: []byte(`{"answer":"` + strings.Repeat("b", 2000) + `"}`),
	}))
	require.Len(t, repo.captures, 1)
	capture := repo.captures[0]
	require.True(t, capture.Truncated)
	require.NotNil(t, capture.TruncateReason)
	require.Contains(t, *capture.TruncateReason, "usage_capture.max_record_bytes")
	decoded := usageCaptureTestGunzip(t, capture.PayloadGzip)
	require.LessOrEqual(t, len(decoded), 650)

	var record claudetap.TraceRecord
	require.NoError(t, json.Unmarshal(decoded, &record))
	var requestMarker usageCaptureTruncatedPayload
	require.NoError(t, json.Unmarshal(record.Request.Body, &requestMarker))
	require.True(t, requestMarker.Truncated)
	var responseMarker usageCaptureTruncatedPayload
	require.NoError(t, json.Unmarshal(record.Response.Body, &responseMarker))
	require.True(t, responseMarker.Truncated)
}

func TestUsageCaptureServiceRedactsSensitiveHeaders(t *testing.T) {
	repo := &usageCaptureFakeRepo{}
	svc := NewUsageCaptureService(repo, usageCaptureTestConfig(true, 7, 1_000_000))

	require.NoError(t, svc.Capture(context.Background(), CaptureInput{
		RequestID: "req_redact",
		RequestHeaders: map[string]string{
			"Authorization": "Bearer secret",
			"x-api-key":     "sk-secret",
			"X-Trace-ID":    "trace-1",
		},
		ResponseHeaders: map[string]string{"Set-Cookie": "session=secret"},
		RequestBody:     []byte(`{}`),
		ResponseBody:    []byte(`{}`),
	}))
	record, _, err := svc.Get(context.Background(), "req_redact", nil)
	require.NoError(t, err)
	require.Equal(t, usageCaptureRedactedHeaderValue, record.Request.Headers["Authorization"])
	require.Equal(t, usageCaptureRedactedHeaderValue, record.Request.Headers["x-api-key"])
	require.Equal(t, "trace-1", record.Request.Headers["X-Trace-ID"])
	require.Equal(t, usageCaptureRedactedHeaderValue, record.Response.Headers["Set-Cookie"])
}

func TestUsageCaptureServiceDisabledNoOp(t *testing.T) {
	repo := &usageCaptureFakeRepo{}
	svc := NewUsageCaptureService(repo, usageCaptureTestConfig(false, 7, 1_000_000))

	require.NoError(t, svc.Capture(context.Background(), CaptureInput{RequestID: "req_disabled"}))
	require.Equal(t, 0, repo.createCalled)
	require.Empty(t, repo.captures)
}

func TestUsageCaptureServiceExpiresAt(t *testing.T) {
	repo := &usageCaptureFakeRepo{}
	svc := NewUsageCaptureService(repo, usageCaptureTestConfig(true, 3, 1_000_000))
	before := time.Now()
	require.NoError(t, svc.Capture(context.Background(), CaptureInput{RequestID: "req_expires", RequestBody: []byte(`{}`), ResponseBody: []byte(`{}`)}))
	after := time.Now()
	require.Len(t, repo.captures, 1)
	require.NotNil(t, repo.captures[0].ExpiresAt)
	require.True(t, repo.captures[0].ExpiresAt.After(before.Add(3*24*time.Hour)) || repo.captures[0].ExpiresAt.Equal(before.Add(3*24*time.Hour)))
	require.True(t, repo.captures[0].ExpiresAt.Before(after.Add(3*24*time.Hour)) || repo.captures[0].ExpiresAt.Equal(after.Add(3*24*time.Hour)))

	repo = &usageCaptureFakeRepo{}
	svc = NewUsageCaptureService(repo, usageCaptureTestConfig(false, 0, 1_000_000))
	svc.cfg.UsageCapture.Enabled = true
	require.NoError(t, svc.Capture(context.Background(), CaptureInput{RequestID: "req_no_expiry", RequestBody: []byte(`{}`), ResponseBody: []byte(`{}`)}))
	require.Len(t, repo.captures, 1)
	require.Nil(t, repo.captures[0].ExpiresAt)
}

func TestUsageCaptureServiceCaptureBestEffortSwallowsErrors(t *testing.T) {
	repo := &usageCaptureFakeRepo{createErr: errors.New("insert failed")}
	svc := NewUsageCaptureService(repo, usageCaptureTestConfig(true, 7, 1_000_000))
	require.NotPanics(t, func() {
		svc.CaptureBestEffort(context.Background(), CaptureInput{RequestID: "req_best_effort", RequestBody: []byte(`{}`), ResponseBody: []byte(`{}`)})
	})
	require.Equal(t, 1, repo.createCalled)
}

func TestUsageCaptureServiceAvailable(t *testing.T) {
	repo := &usageCaptureFakeRepo{}
	svc := NewUsageCaptureService(repo, usageCaptureTestConfig(true, 7, 1_000_000))
	apiKeyID := int64(1)
	require.NoError(t, svc.Capture(context.Background(), CaptureInput{RequestID: "req_exists", APIKeyID: &apiKeyID, RequestBody: []byte(`{}`), ResponseBody: []byte(`{}`)}))

	available, err := svc.Available(context.Background(), "req_exists", &apiKeyID)
	require.NoError(t, err)
	require.True(t, available)
	available, err = svc.Available(context.Background(), "req_missing", &apiKeyID)
	require.NoError(t, err)
	require.False(t, available)
}

func TestUsageCaptureRetentionServiceRunOnceDeletesUntilPartial(t *testing.T) {
	repo := &usageCaptureFakeRepo{deleteResults: []int{2, 2, 1}}
	cfg := usageCaptureTestConfig(true, 30, 1_000_000)
	cfg.UsageCapture.RetentionBatchSize = 2
	svc := NewUsageCaptureRetentionService(repo, cfg)

	svc.runOnce()

	require.Equal(t, 3, repo.deleteCalls)
	require.Equal(t, []int{2, 2, 2}, repo.deleteBatchSizes)
}

func TestUsageCaptureRetentionServiceRunOnceDisabled(t *testing.T) {
	repo := &usageCaptureFakeRepo{deleteResults: []int{1}}
	svc := NewUsageCaptureRetentionService(repo, usageCaptureTestConfig(false, 30, 1_000_000))

	svc.runOnce()

	require.Equal(t, 0, repo.deleteCalls)
}

func TestUsageCaptureRetentionServiceStartStop(t *testing.T) {
	repo := &usageCaptureFakeRepo{}
	cfg := usageCaptureTestConfig(true, 30, 1_000_000)
	cfg.UsageCapture.RetentionIntervalSeconds = 1
	svc := NewUsageCaptureRetentionService(repo, cfg)

	svc.Start()
	svc.Stop()
}

func usageCaptureTestConfig(enabled bool, retentionDays int, maxRecordBytes int) *config.Config {
	return &config.Config{UsageCapture: config.UsageCaptureConfig{
		Enabled:                  enabled,
		RetentionDays:            retentionDays,
		MaxRecordBytes:           maxRecordBytes,
		RetentionIntervalSeconds: 300,
		RetentionBatchSize:       2000,
	}}
}

func usageCaptureTestGunzip(t *testing.T, payload []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return decoded
}

func usageCaptureTestAPIKeyMatches(left *int64, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
